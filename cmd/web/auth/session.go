package auth

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gorilla/sessions"
)

const (
	SessionName       = "rewind_session"
	UserIDKey         = "user_id"
	UsernameKey       = "username"
	AccessLevelKey    = "access_level"
	SessionCreatedKey = "created_at"
)

var (
	ErrNotAuthenticated = errors.New("not authenticated")
)

type SessionManager struct {
	store *sessions.CookieStore
}

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

func (sm *SessionManager) GetSession(r *http.Request) (userID, username string, err error) {
	session, err := sm.store.Get(r, SessionName)
	if err != nil {
		_, cookieErr := r.Cookie(SessionName)
		slog.Warn("failed to decode session", "error", err, "host", r.Host, "has_cookie", cookieErr == nil)
		return "", "", err
	}

	userIDVal, ok := session.Values[UserIDKey]
	if !ok {
		return "", "", ErrNotAuthenticated
	}

	usernameVal, ok := session.Values[UsernameKey]
	if !ok {
		return "", "", ErrNotAuthenticated
	}

	uid, ok := userIDVal.(string)
	if !ok {
		return "", "", ErrNotAuthenticated
	}

	uname, ok := usernameVal.(string)
	if !ok {
		return "", "", ErrNotAuthenticated
	}

	return uid, uname, nil
}

// GetAccessLevel reads the stored access level from the session cookie.
// Returns AccessUnauthenticated if the session is missing or invalid.
func (sm *SessionManager) GetAccessLevel(r *http.Request) AccessLevel {
	session, err := sm.store.Get(r, SessionName)
	if err != nil {
		return AccessUnauthenticated
	}

	val, ok := session.Values[AccessLevelKey]
	if !ok {
		return AccessUnauthenticated
	}

	str, ok := val.(string)
	if !ok {
		return AccessUnauthenticated
	}

	level := AccessLevel(str)
	switch level {
	case AccessUser, AccessAdmin:
		return level
	default:
		return AccessUnauthenticated
	}
}

func (sm *SessionManager) IsAuthenticated(r *http.Request) bool {
	_, _, err := sm.GetSession(r)
	return err == nil
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

func (sm *SessionManager) ClearSession(w http.ResponseWriter, r *http.Request) error {
	session, _ := sm.store.Get(r, SessionName)
	session.Options.MaxAge = -1
	return session.Save(r, w)
}

type AccessLevel string

const (
	AccessUnauthenticated AccessLevel = "unauthenticated"
	AccessUser            AccessLevel = "user"
	AccessAdmin           AccessLevel = "admin"
)
