package auth

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gorilla/sessions"
	"thirdcoast.systems/rewind/pkg/plugin"
)

func pluginauth() plugin.Authn {
	return plugin.Auth()
}

const (
	// SessionName is the cookie name used to store the user session.
	SessionName = "rewind_session"
	// UserIDKey is the session key for the authenticated user's UUID.
	UserIDKey = "user_id"
	// UsernameKey is the session key for the authenticated user's display name.
	UsernameKey = "username"
	// AccessLevelKey is the session key for the user's access level (user or admin).
	AccessLevelKey = "access_level"
	// SessionCreatedKey is the session key for the Unix timestamp when the session was created.
	SessionCreatedKey = "created_at"
)

var (
	// ErrNotAuthenticated is returned when a session is missing or does not contain valid credentials.
	ErrNotAuthenticated = errors.New("not authenticated")
)

// SessionManager handles user session creation, retrieval, and invalidation using encrypted cookies.
type SessionManager struct {
	store *sessions.CookieStore
}

// NewSessionManager creates a SessionManager with the given secret, generating a random one if empty.
func NewSessionManager(secret string) *SessionManager {
	if secret == "" {
		secret = generateSecret()
	}
	return &SessionManager{
		store: sessions.NewCookieStore([]byte(secret)),
	}
}

func generateSecret() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.StdEncoding.EncodeToString(b)
}

// SaveSession persists the user's identity and access level into a signed session cookie.
func (sm *SessionManager) SaveSession(w http.ResponseWriter, r *http.Request, userID, username string, accessLevel AccessLevel) error {
	session, _ := sm.store.Get(r, SessionName)
	session.Values[UserIDKey] = userID
	session.Values[UsernameKey] = username
	session.Values[AccessLevelKey] = string(accessLevel)
	session.Values[SessionCreatedKey] = time.Now().Unix()

	// Determine if we're on HTTPS
	isHTTPS := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"

	// Configure session options
	session.Options = &sessions.Options{
		Path:     "/",
		MaxAge:   86400 * 7, // 7 days
		HttpOnly: true,
	}

	// Extension API is bearer-token based; keep session cookies in a safer mode.
	session.Options.SameSite = http.SameSiteLaxMode
	session.Options.Secure = isHTTPS

	return session.Save(r, w)
}

// ReadCookie decodes the session cookie without going through the Auth plugin.
// LocalAuth.Current uses this so GetSession can route through plugin.Auth without recursing.
func (sm *SessionManager) ReadCookie(r *http.Request) (userID, username string, level AccessLevel, created time.Time, err error) {
	session, err := sm.store.Get(r, SessionName)
	if err != nil {
		_, cookieErr := r.Cookie(SessionName)
		slog.Warn("failed to decode session", "error", err, "host", r.Host, "has_cookie", cookieErr == nil)
		return "", "", AccessUnauthenticated, time.Time{}, err
	}

	userIDVal, ok := session.Values[UserIDKey]
	if !ok {
		return "", "", AccessUnauthenticated, time.Time{}, ErrNotAuthenticated
	}
	usernameVal, ok := session.Values[UsernameKey]
	if !ok {
		return "", "", AccessUnauthenticated, time.Time{}, ErrNotAuthenticated
	}
	uid, ok := userIDVal.(string)
	if !ok {
		return "", "", AccessUnauthenticated, time.Time{}, ErrNotAuthenticated
	}
	uname, ok := usernameVal.(string)
	if !ok {
		return "", "", AccessUnauthenticated, time.Time{}, ErrNotAuthenticated
	}

	level = AccessUnauthenticated
	if v, ok := session.Values[AccessLevelKey].(string); ok {
		switch AccessLevel(v) {
		case AccessUser, AccessAdmin:
			level = AccessLevel(v)
		}
	}
	if v, ok := session.Values[SessionCreatedKey].(int64); ok {
		created = time.Unix(v, 0)
	}
	return uid, uname, level, created, nil
}

// GetSession returns the current user through plugin.Auth when registered,
// otherwise the session cookie (tests that never call plugin.Use).
func (sm *SessionManager) GetSession(r *http.Request) (userID, username string, err error) {
	if a := pluginauth(); a != nil {
		actor, err := a.Current(r)
		if err != nil || actor == nil {
			return "", "", ErrNotAuthenticated
		}
		return actor.UserID, actor.Name, nil
	}
	uid, name, _, _, err := sm.ReadCookie(r)
	return uid, name, err
}

// GetAccessLevel reads authorization through plugin.Auth roles when registered.
func (sm *SessionManager) GetAccessLevel(r *http.Request) AccessLevel {
	if a := pluginauth(); a != nil {
		actor, err := a.Current(r)
		if err != nil || actor == nil {
			return AccessUnauthenticated
		}
		if actor.HasRole("admin") {
			return AccessAdmin
		}
		return AccessUser
	}
	_, _, level, _, err := sm.ReadCookie(r)
	if err != nil {
		return AccessUnauthenticated
	}
	return level
}

// GetSessionCreatedAt returns the time the session was created.
// Returns zero time if the session is missing or invalid.
func (sm *SessionManager) GetSessionCreatedAt(r *http.Request) time.Time {
	session, err := sm.store.Get(r, SessionName)
	if err != nil {
		return time.Time{}
	}

	val, ok := session.Values[SessionCreatedKey]
	if !ok {
		return time.Time{}
	}

	unix, ok := val.(int64)
	if !ok {
		return time.Time{}
	}

	return time.Unix(unix, 0)
}

// ClearSession expires the session cookie, effectively logging the user out.
func (sm *SessionManager) ClearSession(w http.ResponseWriter, r *http.Request) error {
	session, _ := sm.store.Get(r, SessionName)
	session.Options.MaxAge = -1
	return session.Save(r, w)
}

// AccessLevel represents a user's authorization tier.
type AccessLevel string

const (
	// AccessUnauthenticated indicates no valid session exists.
	AccessUnauthenticated AccessLevel = "unauthenticated"
	// AccessUser indicates a regular authenticated user.
	AccessUser AccessLevel = "user"
	// AccessAdmin indicates an administrator with full privileges.
	AccessAdmin AccessLevel = "admin"
)
