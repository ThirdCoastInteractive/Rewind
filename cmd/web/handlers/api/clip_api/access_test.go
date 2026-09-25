package clip_api

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/cmd/web/handlers/common"
	"thirdcoast.systems/rewind/pkg/plugin"
)

func TestClipMutateAllowsLiveWorkspace(t *testing.T) {
	plugin.Reset()
	t.Cleanup(plugin.Reset)
	var a, b pgtype.UUID
	_ = a.Scan("11111111-1111-1111-1111-111111111111")
	_ = b.Scan("22222222-2222-2222-2222-222222222222")
	if a == b {
		t.Fatal("fixture ids")
	}
	if common.LiveWorkspaceWrite() {
		t.Fatal("OSS")
	}
	plugin.Use(plugin.Set{Authz: liveAuthz{}})
	if !common.LiveWorkspaceWrite() {
		t.Fatal("live")
	}
}

type liveAuthz struct{}

func (liveAuthz) Allow(context.Context, *plugin.Actor, string, string) bool { return true }
