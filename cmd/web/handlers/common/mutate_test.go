package common

import (
	"context"
	"testing"

	"thirdcoast.systems/rewind/pkg/plugin"
	"thirdcoast.systems/rewind/pkg/plugin/builtin"
)

func TestLiveWorkspaceWrite(t *testing.T) {
	plugin.Reset()
	t.Cleanup(plugin.Reset)
	if LiveWorkspaceWrite() {
		t.Fatal("OSS default must keep creator-or-admin deletes")
	}
	plugin.Use(plugin.Set{Authz: liveAuthz{}})
	if !LiveWorkspaceWrite() {
		t.Fatal("Live tenant authz must allow workspace deletes")
	}
	plugin.Use(plugin.Set{Authz: builtin.LocalAuthz{}})
	if LiveWorkspaceWrite() {
		t.Fatal("LocalAuthz is OSS")
	}
}

type liveAuthz struct{}

func (liveAuthz) Allow(context.Context, *plugin.Actor, string, string) bool { return true }
