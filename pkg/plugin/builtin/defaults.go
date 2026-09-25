package builtin

import (
	"os"
	"strings"

	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/pkg/plugin"
)

// Defaults fills any unset Blob, Authz, and ML slots. Authn is registered by the web process.
func Defaults(dbc *db.DatabaseConnection) {
	downloads := strings.TrimSpace(os.Getenv("DOWNLOADS_DIR"))
	if downloads == "" {
		downloads = "/downloads"
	}
	cur := plugin.Current()
	s := plugin.Set{}
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
