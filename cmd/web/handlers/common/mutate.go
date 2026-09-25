package common

import "thirdcoast.systems/rewind/pkg/plugin/builtin"

// LiveWorkspaceWrite is true when Live tenant authz is installed.
// After RequireVideo(ActionVideoWrite), workspace members may delete recordings
// and clips they did not personally create. OSS stays creator-or-admin.
func LiveWorkspaceWrite() bool {
	return !builtin.OSSGuards()
}
