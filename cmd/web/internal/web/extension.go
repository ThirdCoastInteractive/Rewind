package web

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/pkg/encryption"
)

type extensionArchiveRequest struct {
	URL string `json:"url"`
}

type extensionArchiveResponse struct {
	JobID    string `json:"job_id"`
	Redirect string `json:"redirect"`
}

func (s *Webserver) requireExtensionBearerToken(c echo.Context) (*db.User, string, error) {
	authHeader := c.Request().Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		return nil, "", echo.NewHTTPError(http.StatusUnauthorized, "missing bearer token")
	}
	tokenStr := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
	if tokenStr == "" {
		return nil, "", echo.NewHTTPError(http.StatusUnauthorized, "missing bearer token")
	}

	tokenRow, err := s.dbc.Queries(c.Request().Context()).GetExtensionTokenByToken(c.Request().Context(), tokenStr)
	if err != nil {
		return nil, "", echo.NewHTTPError(http.StatusUnauthorized, "invalid or expired token")
	}

	_ = s.dbc.Queries(c.Request().Context()).UpdateExtensionTokenLastUsed(c.Request().Context(), tokenStr)

	user, err := s.dbc.Queries(c.Request().Context()).SelectUserByID(c.Request().Context(), tokenRow.UserID)
	if err != nil || !user.Enabled {
		return nil, "", echo.NewHTTPError(http.StatusUnauthorized, "user not found or disabled")
	}

	return user, tokenStr, nil
}

func validateExtensionIdentityRedirectURI(clientID string, raw string) error {
	if clientID == "" {
		return errors.New("client_id required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if u.Scheme != "https" {
		return errors.New("redirect_uri must be https")
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return errors.New("redirect_uri host required")
	}

	// Chrome: https://<extension-id>.chromiumapp.org/...
	expectedChrome := strings.ToLower(clientID) + ".chromiumapp.org"
	if subtle.ConstantTimeCompare([]byte(host), []byte(expectedChrome)) == 1 {
		return nil
	}

	// Firefox: https://<something>.extensions.allizom.org/... (or extensions.mozilla.org)
	if isFirefoxIdentityRedirectHost(host) {
		return nil
	}

	return fmt.Errorf("redirect_uri host must be %s (chrome) or *.%s (firefox)", expectedChrome, "extensions.allizom.org")
}

func isFirefoxIdentityRedirectHost(host string) bool {
	// Common Firefox redirect hosts from browser.identity.getRedirectURL().
	// The left-most label is not directly derived from gecko.id in all cases,
	// so we validate by suffix rather than exact match.
	suffixes := []string{
		".extensions.allizom.org",
		".extensions.mozilla.org",
	}
	for _, s := range suffixes {
		if strings.HasSuffix(host, s) {
			// Require at least one label before the suffix.
			return len(host) > len(s)
		}
	}
	return false
}

// HandleAPIExtensionAuthStart starts an interactive login flow for MV3 extensions.
// If the user is already authenticated, it redirects directly to /api/extension/auth/finish.
// Otherwise it redirects to /login with the necessary query params preserved.
func (s *Webserver) HandleAPIExtensionAuthStart(c echo.Context) error {
	clientID := strings.TrimSpace(c.QueryParam("client_id"))
	redirectURI := strings.TrimSpace(c.QueryParam("redirect_uri"))
	state := strings.TrimSpace(c.QueryParam("state"))

	if len(s.allowedExtensionIDs) > 0 {
		if _, ok := s.allowedExtensionIDs[clientID]; !ok {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "client_id not allowed"})
		}
	}

	if err := validateExtensionIdentityRedirectURI(clientID, redirectURI); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	if state == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "state required"})
	}

	// If already logged in, jump straight to finish.
	accessLevel, _ := c.Get("accessLevel").(string)
	if accessLevel != "" && accessLevel != "unauthenticated" {
		return c.Redirect(302, "/api/extension/auth/finish?client_id="+url.QueryEscape(clientID)+"&redirect_uri="+url.QueryEscape(redirectURI)+"&state="+url.QueryEscape(state))
	}

	// Redirect to /login; the login form posts back to the same URL (query preserved)
	// so our login handler can bounce to /api/extension/auth/finish after success.
	return c.Redirect(302, "/login?extensionAuth=true&client_id="+url.QueryEscape(clientID)+"&redirect_uri="+url.QueryEscape(redirectURI)+"&state="+url.QueryEscape(state))
}

// HandleAPIExtensionAuthFinish mints a bearer token for an authenticated user
// and redirects back to the chrome.identity redirect URL with #token and #state.
func (s *Webserver) HandleAPIExtensionAuthFinish(c echo.Context) error {
	clientID := strings.TrimSpace(c.QueryParam("client_id"))
	redirectURI := strings.TrimSpace(c.QueryParam("redirect_uri"))
	state := strings.TrimSpace(c.QueryParam("state"))

	if len(s.allowedExtensionIDs) > 0 {
		if _, ok := s.allowedExtensionIDs[clientID]; !ok {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "client_id not allowed"})
		}
	}

	if err := validateExtensionIdentityRedirectURI(clientID, redirectURI); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	if state == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "state required"})
	}

	userID, _, err := s.sessionManager.GetSession(c.Request())
	if err != nil || userID == "" {
		return c.Redirect(302, "/login?extensionAuth=true&client_id="+url.QueryEscape(clientID)+"&redirect_uri="+url.QueryEscape(redirectURI)+"&state="+url.QueryEscape(state))
	}

	tokenStr, err := s.createExtensionToken(c.Request().Context(), userID)
	if err != nil {
		slog.Error("failed to create extension token", "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to create token"})
	}

	sep := "#"
	if strings.Contains(redirectURI, "#") {
		sep = "&"
	}
	redirect := redirectURI + sep + "token=" + url.QueryEscape(tokenStr) + "&state=" + url.QueryEscape(state)
	return c.Redirect(302, redirect)
}

// ExtensionStatusResponse is the JSON response for /api/extension/status
type ExtensionStatusResponse struct {
	OK            bool           `json:"ok"`
	Authenticated bool           `json:"authenticated"`
	User          *ExtensionUser `json:"user,omitempty"`
	RunningJobs   int            `json:"running_jobs"`
	CookieCount   int            `json:"cookie_count"`
	ServerTime    string         `json:"server_time"`
	AppVersion    string         `json:"app_version,omitempty"`
	Token         string         `json:"token,omitempty"` // Only sent on login
}

// ExtensionUser contains user info for the extension
type ExtensionUser struct {
	Username string      `json:"username"`
	Role     db.UserRole `json:"role"`
}

// ExtensionCookiesRequest is the JSON request for /api/extension/cookies
type ExtensionCookiesRequest struct {
	CookiesContent string `json:"cookies_content"`
}

// HandleAPIExtensionStatus returns status information for the extension
func (s *Webserver) HandleAPIExtensionStatus(c echo.Context) error {
	user, _, err := s.requireExtensionBearerToken(c)
	if err != nil {
		return err
	}

	response := ExtensionStatusResponse{
		OK:          true,
		ServerTime:  time.Now().UTC().Format(time.RFC3339),
		RunningJobs: 0,
		CookieCount: 0,
	}

	response.Authenticated = true
	response.User = &ExtensionUser{
		Username: user.UserName,
		Role:     user.Role,
	}

	// Get running jobs count for this user
	jobs, err := s.dbc.Queries(c.Request().Context()).ListDownloadJobsByUser(c.Request().Context(), &db.ListDownloadJobsByUserParams{
		ArchivedBy: user.ID,
		PageLimit:  6,
	})
	if err == nil {
		runningCount := 0
		for _, job := range jobs {
			if job.Status == db.JobStatusQueued || job.Status == db.JobStatusProcessing {
				runningCount++
			}
		}
		response.RunningJobs = runningCount
	}

	// Get cookie count for the current site (if provided)
	site := c.QueryParam("site")
	if site != "" {
		cookies, err := s.dbc.Queries(c.Request().Context()).GetUserCookies(c.Request().Context(), user.ID)
		if err == nil {
			cookieCount := 0
			for _, cookie := range cookies {
				// Match cookies for this site (domain ends with site or is exact match)
				if strings.HasSuffix(cookie.Domain, site) || strings.TrimPrefix(cookie.Domain, ".") == site {
					cookieCount++
				}
			}
			response.CookieCount = cookieCount
		}
	}

	return c.JSON(http.StatusOK, response)
}

// HandleAPIExtensionCookies handles cookie uploads from the extension
func (s *Webserver) HandleAPIExtensionCookies(c echo.Context) error {
	user, _, err := s.requireExtensionBearerToken(c)
	if err != nil {
		return err
	}

	// Parse request
	var req ExtensionCookiesRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "invalid request body",
		})
	}

	if req.CookiesContent == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "cookies_content is required",
		})
	}

	// Parse and store cookies
	validCount, invalidCount, err := s.parseCookiesAndStore(c.Request().Context(), user.ID, req.CookiesContent)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": fmt.Sprintf("failed to store cookies: %v", err),
		})
	}

	if validCount == 0 {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": fmt.Sprintf("no valid cookies found (%d invalid lines)", invalidCount),
		})
	}

	return c.JSON(http.StatusOK, map[string]any{
		"status":        "ok",
		"valid_count":   validCount,
		"invalid_count": invalidCount,
	})
}

// parseCookiesAndStore parses Netscape cookie format and stores in database
func (s *Webserver) parseCookiesAndStore(ctx context.Context, userUUID pgtype.UUID, cookiesContent string) (validCount, invalidCount int, err error) {
	// Normalize line endings (handle Windows CRLF, Unix LF, old Mac CR)
	normalizedContent := strings.ReplaceAll(cookiesContent, "\r\n", "\n")
	normalizedContent = strings.ReplaceAll(normalizedContent, "\r", "\n")
	lines := strings.Split(normalizedContent, "\n")

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Skip empty lines and comments
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		// Netscape format: domain	flag	path	secure	expiration	name	value
		parts := strings.Split(trimmed, "\t")
		if len(parts) >= 7 {
			// Parse expiration to int64
			expiration, err := strconv.ParseInt(parts[4], 10, 64)
			if err != nil {
				invalidCount++
				continue
			}

			// Encrypt the cookie value
			encryptedValue, err := encryption.Encrypt(s.encryptionManager, parts[6])
			if err != nil {
				slog.Error("failed to encrypt cookie value", "error", err)
				invalidCount++
				continue
			}

			// Insert cookie into database (will upsert if exists)
			if err := s.dbc.Queries(ctx).InsertCookie(ctx, &db.InsertCookieParams{
				UserID:     userUUID,
				Domain:     parts[0],
				Flag:       parts[1],
				Path:       parts[2],
				Secure:     parts[3],
				Expiration: expiration,
				Name:       parts[5],
				Value:      encryptedValue,
			}); err != nil {
				slog.Error("failed to insert cookie", "error", err)
				invalidCount++
				continue
			}

			validCount++
		} else {
			invalidCount++
		}
	}

	return validCount, invalidCount, nil
}

// HandleAPIExtensionStatusStream returns an SSE stream of status updates for the extension
func (s *Webserver) HandleAPIExtensionStatusStream(c echo.Context) error {
	user, _, err := s.requireExtensionBearerToken(c)
	if err != nil {
		return err
	}

	w := c.Response().Writer
	flusher, ok := w.(http.Flusher)
	if !ok {
		return c.String(500, "streaming unsupported")
	}

	c.Response().Header().Set(echo.HeaderContentType, "text/event-stream")
	c.Response().Header().Set(echo.HeaderCacheControl, "no-cache")
	c.Response().Header().Set(echo.HeaderConnection, "keep-alive")

	// Send initial connection comment
	_, _ = fmt.Fprintf(w, ": connected\n\n")
	flusher.Flush()

	lastFingerprint := ""
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.Request().Context().Done():
			return nil
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(c.Request().Context(), 3*time.Second)

			// Build status response
			response := ExtensionStatusResponse{
				OK:          true,
				ServerTime:  time.Now().UTC().Format(time.RFC3339),
				RunningJobs: 0,
				CookieCount: 0,
			}

			// Get user details
			freshUser, err := s.dbc.Queries(ctx).SelectUserByID(ctx, user.ID)
			if err == nil && freshUser.Enabled {
				response.Authenticated = true
				response.User = &ExtensionUser{
					Username: freshUser.UserName,
					Role:     freshUser.Role,
				}

				// Get running jobs count
				jobs, err := s.dbc.Queries(ctx).ListDownloadJobsByUser(ctx, &db.ListDownloadJobsByUserParams{
					ArchivedBy: user.ID,
					PageLimit:  6,
				})
				if err == nil {
					runningCount := 0
					for _, job := range jobs {
						if job.Status == "queued" || job.Status == "processing" {
							runningCount++
						}
					}
					response.RunningJobs = runningCount
				}
			}

			cancel()

			// Create fingerprint to detect changes
			fingerprint := fmt.Sprintf("%t|%d|%s",
				response.Authenticated,
				response.RunningJobs,
				response.ServerTime[:19], // Truncate to second precision
			)

			// Only send if changed
			if fingerprint != lastFingerprint {
				lastFingerprint = fingerprint

				data, err := json.Marshal(response)
				if err != nil {
					continue
				}

				if _, err := fmt.Fprintf(w, "event: status\n"); err != nil {
					return err
				}
				if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
					return err
				}
				flusher.Flush()
			}
		}
	}
}

// HandleAPIExtensionArchive enqueues a download job and returns the job ID and a UI redirect.
func (s *Webserver) HandleAPIExtensionArchive(c echo.Context) error {
	user, _, err := s.requireExtensionBearerToken(c)
	if err != nil {
		return err
	}

	var req extensionArchiveRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}
	req.URL = strings.TrimSpace(req.URL)
	if req.URL == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "url is required"})
	}

	refresh := false
	if existing, err := s.dbc.Queries(c.Request().Context()).SelectVideoBySrc(c.Request().Context(), req.URL); err == nil && existing != nil {
		refresh = true
	}

	job, err := s.dbc.Queries(c.Request().Context()).EnqueueDownloadJob(c.Request().Context(), &db.EnqueueDownloadJobParams{
		URL:        req.URL,
		ArchivedBy: user.ID,
		Refresh:    refresh,
		ExtraArgs:  []string{},
	})
	if err != nil {
		slog.Error("failed to enqueue job from extension", "error", err, "url", req.URL)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to enqueue job"})
	}

	return c.JSON(http.StatusOK, extensionArchiveResponse{
		JobID:    job.ID.String(),
		Redirect: "/jobs/" + job.ID.String(),
	})
}

// HandleAPIExtensionLogout revokes the current bearer token.
func (s *Webserver) HandleAPIExtensionLogout(c echo.Context) error {
	_, tokenStr, err := s.requireExtensionBearerToken(c)
	if err != nil {
		return err
	}
	_ = s.dbc.Queries(c.Request().Context()).RevokeExtensionToken(c.Request().Context(), tokenStr)
	return c.JSON(http.StatusOK, map[string]any{"status": "ok"})
}

// extensionCORSMiddleware adds CORS headers for chrome extension requests
func (s *Webserver) extensionCORSMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		origin := c.Request().Header.Get("Origin")
		allowedOrigin := ""
		localOrPrivate := isLocalOrPrivateRequestHost(c)

		// Set CORS headers for extension requests
		// Chrome/Firefox extensions send Origin header, but some contexts might not
		if origin != "" {
			if u, err := url.Parse(origin); err == nil {
				if u.Scheme == "chrome-extension" || u.Scheme == "moz-extension" {
					if localOrPrivate {
						allowedOrigin = origin
					} else {
						if _, ok := s.allowedExtensionIDs[u.Host]; ok {
							allowedOrigin = origin
						}
					}
				}
			}
		}

		if allowedOrigin != "" {
			c.Response().Header().Set("Vary", "Origin")
			c.Response().Header().Set("Access-Control-Allow-Origin", allowedOrigin)
			c.Response().Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			c.Response().Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Accept")
			c.Response().Header().Set("Access-Control-Expose-Headers", "Content-Type")
		}

		// Handle preflight OPTIONS request
		if c.Request().Method == "OPTIONS" {
			if origin != "" && allowedOrigin == "" {
				return c.NoContent(http.StatusForbidden)
			}
			return c.NoContent(http.StatusNoContent)
		}

		// Call next handler and ensure CORS headers are preserved on error
		err := next(c)

		// Re-apply CORS headers in case they were cleared by error handling
		if allowedOrigin != "" {
			if c.Response().Header().Get("Access-Control-Allow-Origin") == "" {
				c.Response().Header().Set("Vary", "Origin")
				c.Response().Header().Set("Access-Control-Allow-Origin", allowedOrigin)
			}
		}

		return err
	}
}

func isLocalOrPrivateRequestHost(c echo.Context) bool {
	hostHeader := strings.TrimSpace(c.Request().Header.Get("X-Forwarded-Host"))
	if hostHeader == "" {
		hostHeader = strings.TrimSpace(c.Request().Host)
	}
	if hostHeader == "" {
		return false
	}
	// If multiple forwarded hosts are provided, use the first.
	if idx := strings.Index(hostHeader, ","); idx >= 0 {
		hostHeader = strings.TrimSpace(hostHeader[:idx])
	}

	host := hostHeader
	if h, _, err := net.SplitHostPort(hostHeader); err == nil {
		host = h
	}
	host = strings.ToLower(strings.TrimSpace(host))

	if host == "localhost" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() {
			return true
		}
		if ip4 := ip.To4(); ip4 != nil {
			// RFC1918 + link-local
			switch {
			case ip4[0] == 10:
				return true
			case ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31:
				return true
			case ip4[0] == 192 && ip4[1] == 168:
				return true
			case ip4[0] == 169 && ip4[1] == 254:
				return true
			default:
				return false
			}
		}
		// IPv6: accept loopback already handled; treat ULA (fc00::/7) and link-local (fe80::/10) as local.
		if len(ip) == net.IPv6len {
			if ip[0]&0xfe == 0xfc { // fc00::/7
				return true
			}
			if ip[0] == 0xfe && (ip[1]&0xc0) == 0x80 { // fe80::/10
				return true
			}
		}
	}

	return false
}

// createExtensionToken generates a random token, persists it in the DB, and returns the token string.
func (s *Webserver) createExtensionToken(ctx context.Context, userID string) (string, error) {
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", err
	}
	tokenStr := base64.URLEncoding.EncodeToString(tokenBytes)

	var userUUID pgtype.UUID
	if err := userUUID.Scan(userID); err != nil {
		return "", err
	}

	_, err := s.dbc.Queries(ctx).CreateExtensionToken(ctx, &db.CreateExtensionTokenParams{
		UserID:    userUUID,
		Token:     tokenStr,
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(90 * 24 * time.Hour), Valid: true},
	})
	if err != nil {
		return "", err
	}

	return tokenStr, nil
}
