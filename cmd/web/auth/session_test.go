package auth

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSessionManager_SaveAndGetSession_RoundTrip(t *testing.T) {
	sm := NewSessionManager("test-secret")

	req := httptest.NewRequest("GET", "http://example.com/", nil)
	rr := httptest.NewRecorder()

	err := sm.SaveSession(rr, req, "user-1", "alice", AccessUser)
	require.NoError(t, err)

	res := rr.Result()
	require.NotNil(t, res)
	cookies := res.Cookies()
	require.NotEmpty(t, cookies)

	var sessionCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == SessionName {
			sessionCookie = c
			break
		}
	}
	require.NotNil(t, sessionCookie)
	require.NotEmpty(t, sessionCookie.Value)

	req2 := httptest.NewRequest("GET", "http://example.com/", nil)
	req2.AddCookie(sessionCookie)

	uid, uname, err := sm.GetSession(req2)
	require.NoError(t, err)
	require.Equal(t, "user-1", uid)
	require.Equal(t, "alice", uname)
	require.True(t, sm.IsAuthenticated(req2))

	// Verify access level is stored and retrievable
	require.Equal(t, AccessUser, sm.GetAccessLevel(req2))

	// Verify session created_at is stored and retrievable
	createdAt := sm.GetSessionCreatedAt(req2)
	require.False(t, createdAt.IsZero())
	require.WithinDuration(t, time.Now(), createdAt, 5*time.Second)
}

func TestSessionManager_SaveSession_SecureDetection(t *testing.T) {
	sm := NewSessionManager("test-secret")

	t.Run("tls implies secure", func(t *testing.T) {
		req := httptest.NewRequest("GET", "https://example.com/", nil)
		req.TLS = &tls.ConnectionState{}
		rr := httptest.NewRecorder()

		err := sm.SaveSession(rr, req, "user-1", "alice", AccessUser)
		require.NoError(t, err)

		var found bool
		for _, c := range rr.Result().Cookies() {
			if c.Name == SessionName {
				found = true
				require.True(t, c.Secure)
				break
			}
		}
		require.True(t, found)
	})

	t.Run("x-forwarded-proto implies secure", func(t *testing.T) {
		req := httptest.NewRequest("GET", "http://example.com/", nil)
		req.Header.Set("X-Forwarded-Proto", "https")
		rr := httptest.NewRecorder()

		err := sm.SaveSession(rr, req, "user-1", "alice", AccessUser)
		require.NoError(t, err)

		var found bool
		for _, c := range rr.Result().Cookies() {
			if c.Name == SessionName {
				found = true
				require.True(t, c.Secure)
				break
			}
		}
		require.True(t, found)
	})
}

func TestSessionManager_GetSession_NotAuthenticated(t *testing.T) {
	sm := NewSessionManager("test-secret")

	req := httptest.NewRequest("GET", "http://example.com/", nil)
	uid, uname, err := sm.GetSession(req)
	require.ErrorIs(t, err, ErrNotAuthenticated)
	require.Equal(t, "", uid)
	require.Equal(t, "", uname)
	require.False(t, sm.IsAuthenticated(req))
}

func TestSessionManager_GetSession_BadCookie(t *testing.T) {
	sm := NewSessionManager("test-secret")

	req := httptest.NewRequest("GET", "http://example.com/", nil)
	req.AddCookie(&http.Cookie{Name: SessionName, Value: "this-is-not-a-valid-cookie"})

	uid, uname, err := sm.GetSession(req)
	require.Error(t, err)
	require.Equal(t, "", uid)
	require.Equal(t, "", uname)
}

func TestSessionManager_ClearSession(t *testing.T) {
	sm := NewSessionManager("test-secret")

	req := httptest.NewRequest("GET", "http://example.com/", nil)
	rr := httptest.NewRecorder()

	err := sm.ClearSession(rr, req)
	require.NoError(t, err)

	// Gorilla sessions writes a Set-Cookie header for deletion.
	setCookies := rr.Result().Header.Values("Set-Cookie")
	require.NotEmpty(t, setCookies)

	var found bool
	for _, v := range setCookies {
		if strings.HasPrefix(v, SessionName+"=") {
			found = true
			// Be flexible across implementations: deletion usually sets Max-Age=0 and/or Expires in past.
			require.True(t, strings.Contains(v, "Max-Age=0") || strings.Contains(v, "Max-Age=-1") || strings.Contains(v, "Expires="))
			break
		}
	}
	require.True(t, found)
}
