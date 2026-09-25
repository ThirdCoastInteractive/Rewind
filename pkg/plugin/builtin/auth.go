package builtin

import (
	"context"

	"thirdcoast.systems/rewind/pkg/plugin"
)

// LocalAuthz: any logged-in user can read/write the library; admin is a role.
type LocalAuthz struct{}

func (LocalAuthz) Allow(_ context.Context, actor *plugin.Actor, action, _ string) bool {
	if actor == nil {
		return false
	}
	if action == plugin.ActionAdmin {
		return actor.HasRole("admin")
	}
	return true
}

// OSSGuards is true when Authz is unset or LocalAuthz (self-hosted).
func OSSGuards() bool {
	g := plugin.Guards()
	if g == nil {
		return true
	}
	_, ok := g.(LocalAuthz)
	return ok
}
