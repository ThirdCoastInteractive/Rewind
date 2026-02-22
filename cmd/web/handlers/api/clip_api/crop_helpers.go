package clip_api

import (
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
}
