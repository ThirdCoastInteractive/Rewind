package web

import (
	"context"
	"net/http"
	"path"
	"sync"

	"github.com/labstack/echo/v4"
)

// collaborationAccess tracks request lifetimes, including sockets still
// upgrading. Cancelling a ygo request closes its socket and forces fresh auth.
type collaborationAccess struct {
	mu    sync.Mutex
	rooms map[string]map[*http.Request]context.CancelFunc
}

func (a *collaborationAccess) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		r = r.WithContext(ctx)
		room := path.Base(r.URL.Path)
		a.mu.Lock()
		if a.rooms == nil {
			a.rooms = make(map[string]map[*http.Request]context.CancelFunc)
		}
		if a.rooms[room] == nil {
			a.rooms[room] = make(map[*http.Request]context.CancelFunc)
		}
		a.rooms[room][r] = cancel
		a.mu.Unlock()
		defer func() {
			a.mu.Lock()
			delete(a.rooms[room], r)
			if len(a.rooms[room]) == 0 {
				delete(a.rooms, room)
			}
			a.mu.Unlock()
		}()
		next.ServeHTTP(w, r)
	})
}

func (a *collaborationAccess) revoke(room string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, cancel := range a.rooms[room] {
		cancel()
	}
}

// changingHosts forces every existing peer to reauthenticate after a successful
// roster mutation. New requests read the committed roles; preexisting requests
// are registered before authorization, so pending upgrades are revoked too.
func (a *collaborationAccess) changingHosts(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		err := next(c)
		if err == nil && c.Response().Status < 400 {
			a.revoke(c.Param("id"))
		}
		return err
	}
}
