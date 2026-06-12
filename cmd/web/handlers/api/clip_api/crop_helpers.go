package clip_api

import (
	"fmt"
	"strings"

	"github.com/starfederation/datastar-go/datastar"
	"thirdcoast.systems/rewind/cmd/web/templates/components"
	"thirdcoast.systems/rewind/pkg/utils/crops"
)

// patchCropUI sends SSE patches for both the CropManager and CutExportPanel
// after any crop mutation (create, update, delete).
func patchCropUI(sse *datastar.ServerSentEventGenerator, clipIDStr string, clipCrops crops.CropArray) {
	_ = sse.PatchElementTempl(
		components.CropManager(clipIDStr, clipCrops),
		datastar.WithSelector("#crop-manager"),
		datastar.WithModeReplace(),
	)
	_ = sse.PatchElementTempl(
		components.CutExportPanel(clipCrops),
		datastar.WithSelectorID("cut-export-panel"),
	)
	// Refresh multicam camera buttons when crops change.
	// Pass nil shotList since the JS manages shots client-side
	// and will re-render from its own state.
	_ = sse.PatchElementTempl(
		components.MulticamPanel(clipIDStr, clipCrops, nil),
		datastar.WithSelectorID("multicam-panel"),
	)
}

// deduplicateCropName appends a number suffix if a crop with the same name already exists.
func deduplicateCropName(name string, existing crops.CropArray) string {
	names := make(map[string]bool, len(existing))
	for _, c := range existing {
		names[strings.ToLower(c.Name)] = true
	}
	if !names[strings.ToLower(name)] {
		return name
	}
	for n := 2; n <= 99; n++ {
		candidate := fmt.Sprintf("%s %d", name, n)
		if !names[strings.ToLower(candidate)] {
			return candidate
		}
	}
	return name
}
