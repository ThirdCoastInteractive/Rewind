package encode

import (
	"fmt"
	"thirdcoast.systems/rewind/internal/stitch"
)

type canonicalRenderConfig struct {
	Width, Height  int
	FPS            float64
	StartUS, EndUS int64
}

func canonicalRenderConfigFromSnapshot(s stitch.RenderSnapshot) (canonicalRenderConfig, error) {
	if s.Document.Width <= 0 || s.Document.Height <= 0 || s.Document.FPS <= 0 {
		return canonicalRenderConfig{}, fmt.Errorf("canonical snapshot has invalid canvas")
	}
	c := canonicalRenderConfig{Width: s.Document.Width, Height: s.Document.Height, FPS: s.Document.FPS}
	if s.Options.Scope == "range" {
		c.StartUS, c.EndUS = s.Options.StartUS, s.Options.EndUS
	}
	return c, nil
}
