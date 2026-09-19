package builtin

import (
	"os"
	"strings"

	"thirdcoast.systems/rewind/cmd/web/auth"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/pkg/plugin"
)

// Defaults fills any unset plugin slot with OSS builtins.
func Defaults(sessions *auth.SessionManager, dbc *db.DatabaseConnection) {
	downloads := strings.TrimSpace(os.Getenv("DOWNLOADS_DIR"))
	if downloads == "" {
		downloads = "/downloads"
	}
	cur := plugin.Current()
	s := plugin.Set{}
	if cur.Authn == nil {
		s.Authn = NewLocalAuth(sessions)
	}
	if cur.Authz == nil {
		s.Authz = LocalAuthz{}
	}
	if cur.Blob == nil {
		s.Blob = NewDisk(downloads)
	}
	if cur.ML == nil {
		s.ML = &LocalML{DB: dbc}
	}
	plugin.Use(s)
}
