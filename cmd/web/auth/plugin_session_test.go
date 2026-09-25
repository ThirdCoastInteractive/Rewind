package auth

import (
	"net/http/httptest"
	"os"
	"testing"

	"thirdcoast.systems/rewind/pkg/plugin"
)

func TestPluginAuthRoundTripEmptySecret(t *testing.T) {
	t.Setenv("SESSION_SECRET", "")
	plugin.Reset()
	t.Cleanup(plugin.Reset)

	sm := NewSessionManager(os.Getenv("SESSION_SECRET"))
	plugin.Use(plugin.Set{Authn: NewLocalAuth(sm)})

	req := httptest.NewRequest("GET", "http://example.com/login", nil)
	rr := httptest.NewRecorder()
	if err := sm.SaveSession(rr, req, "user-1", "alice", AccessUser); err != nil {
		t.Fatal(err)
	}
	var cookie = rr.Result().Cookies()
	if len(cookie) == 0 {
		t.Fatal("no session cookie")
	}

	req2 := httptest.NewRequest("GET", "http://example.com/", nil)
	req2.AddCookie(cookie[0])

	uid, name, err := sm.GetSession(req2)
	if err != nil {
		t.Fatal(err)
	}
	if uid != "user-1" || name != "alice" {
		t.Fatalf("GetSession %s %s", uid, name)
	}

	actor, err := plugin.Auth().Current(req2)
	if err != nil {
		t.Fatal(err)
	}
	if actor == nil || actor.UserID != "user-1" {
		t.Fatalf("Auth.Current %+v", actor)
	}

	local := NewLocalAuth(sm)
	rr3 := httptest.NewRecorder()
	if err := local.Logout(rr3, req2); err != nil {
		t.Fatal(err)
	}
	cleared := rr3.Result().Cookies()
	if len(cleared) == 0 || cleared[0].MaxAge >= 0 {
		t.Fatalf("expected cleared session cookie, got %+v", cleared)
	}
	// Browser drops MaxAge<0 cookies; request without session must fail.
	req3 := httptest.NewRequest("GET", "http://example.com/", nil)
	if _, _, err := sm.GetSession(req3); err == nil {
		t.Fatal("expected GetSession to fail after Logout")
	}
}
