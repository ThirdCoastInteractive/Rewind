package shownote_api

import (
	"context"
	"testing"

	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/pkg/plugin"
)

type liveArchiveGuardStub struct{}

func (liveArchiveGuardStub) CreateInput(context.Context, *plugin.Actor, string) (*plugin.LiveInput, error) {
	return nil, nil
}
func (liveArchiveGuardStub) GetInput(context.Context, *plugin.Actor, string) (*plugin.LiveInput, error) {
	return nil, nil
}
func (liveArchiveGuardStub) ListInputs(context.Context, *plugin.Actor) ([]*plugin.LiveInput, error) {
	return nil, nil
}
func (liveArchiveGuardStub) DeleteInput(context.Context, *plugin.Actor, string) error { return nil }
func (liveArchiveGuardStub) AddOutput(context.Context, *plugin.Actor, string, string, string) (*plugin.LiveOutput, error) {
	return nil, nil
}
func (liveArchiveGuardStub) RemoveOutput(context.Context, *plugin.Actor, string, string) error {
	return nil
}
func (liveArchiveGuardStub) EnableOutput(context.Context, *plugin.Actor, string, string, bool) error {
	return nil
}
func (liveArchiveGuardStub) Mount(*echo.Echo) {}

func TestExternalArchiveBlockedOnlyWithLivePlugin(t *testing.T) {
	plugin.Reset()
	if externalArchiveBlocked() {
		t.Fatal("OSS must allow external archival")
	}
	plugin.Use(plugin.Set{Live: liveArchiveGuardStub{}})
	if !externalArchiveBlocked() {
		t.Fatal("Live must block external archival")
	}
	plugin.Reset()
}
