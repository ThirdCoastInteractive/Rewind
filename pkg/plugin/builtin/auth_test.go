package builtin

import (
	"context"
	"testing"

	"thirdcoast.systems/rewind/pkg/plugin"
)

type liveAuthz struct{}

func (liveAuthz) Allow(context.Context, *plugin.Actor, string, string) bool { return true }

func TestOSSGuards(t *testing.T) {
	plugin.Reset()
	t.Cleanup(plugin.Reset)

	if !OSSGuards() {
		t.Fatal("nil Guards is OSS")
	}

	plugin.Use(plugin.Set{Authz: LocalAuthz{}})
	if !OSSGuards() {
		t.Fatal("LocalAuthz is OSS")
	}

	plugin.Reset()
	plugin.Use(plugin.Set{Authz: liveAuthz{}})
	if OSSGuards() {
		t.Fatal("non-LocalAuthz Guards is live")
	}
}
