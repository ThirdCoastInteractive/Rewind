package encode

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"thirdcoast.systems/rewind/internal/stitch"
)

// CompileCanonicalOverlays emits an ASS composition layer for non-image
// overlays. Coordinates are canvas pixels. Image assets are intentionally rejected
// until the asset composition leaf supplies their immutable files.
func CompileCanonicalOverlays(d stitch.Document) (string, error) {
	ordered := append([]stitch.Overlay(nil), d.Overlays...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Z < ordered[j].Z })
	var b strings.Builder
	fmt.Fprintf(&b, "[Script Info]\nScriptType: v4.00+\nPlayResX: %d\nPlayResY: %d\n\n[V4+ Styles]\nFormat: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding\nStyle: Default,Arial,48,&H00FFFFFF,&H00FFFFFF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,2,0,7,0,0,0,1\n\n[Events]\nFormat: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\n", d.Width, d.Height)
	for _, o := range ordered {
		if !o.Visible || o.EndUS <= o.StartUS {
			continue
		}
		if o.Kind == "image" || o.AssetID != "" {
			return "", fmt.Errorf("image overlay %q requires asset composition", o.ID)
		}
		if o.Kind != "text" && o.Kind != "callout" && o.Kind != "arrow" && o.Kind != "rectangle" && o.Kind != "rect" && o.Kind != "ellipse" && o.Kind != "freehand" {
			return "", fmt.Errorf("unsupported overlay kind %q", o.Kind)
		}
		bodies, err := overlayASSBodies(o, d.Width, d.Height)
		if err != nil {
			return "", fmt.Errorf("overlay %q: %w", o.ID, err)
		}
		for _, body := range bodies {
			fmt.Fprintf(&b, "Dialogue: %d,%s,%s,Default,,0,0,0,,%s\n", o.Z, assTimeUS(o.StartUS), assTimeUS(o.EndUS), body)
		}
	}
	return b.String(), nil
}

func overlayASSBodies(o stitch.Overlay, width, height int) ([]string, error) {
	x, y := o.X, o.Y
	w, h := o.Width, o.Height
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("invalid canvas")
	}
	alpha := int((1 - clamp(o.Opacity, 0, 1)) * 255)
	common := fmt.Sprintf("{\\alpha&H%02X&\\frz%.2f\\pos(%.2f,%.2f)", alpha, -o.Rotation, x, y)
	common += fmt.Sprintf("\\org(%.2f,%.2f)", x+w/2, y+h/2)
	if o.Color != "" {
		common += "\\c" + assColor(o.Color)
	}
	if o.Font != "" {
		common += "\\fn" + assEscapeTag(o.Font)
	}
	if o.FontSize > 0 {
		common += "\\fs" + strconv.FormatFloat(o.FontSize, 'f', -1, 64)
	}
	common += "}"
	stroke := o.Color
	if stroke == "" {
		stroke = "#57d5b2"
	}
	shape := "\\3c" + assColor(stroke)
	if o.Background == "" {
		shape += "\\1a&HFF&\\bord1"
	} else {
		shape += "\\c" + assColor(o.Background) + "\\bord1"
	}
	switch o.Kind {
	case "text":
		if strings.TrimSpace(o.Text) == "" {
			return nil, fmt.Errorf("text overlay has empty text")
		}
		return []string{common + assEscape(o.Text)}, nil
	case "callout":
		if strings.TrimSpace(o.Text) == "" {
			return nil, fmt.Errorf("callout overlay has empty text")
		}
		if o.Background == "" {
			shape = "\\c" + assColor("#13241f") + "\\3c" + assColor(stroke) + "\\bord1"
		}
		r := math.Min(18, math.Min(w, h)/4)
		bubble := fmt.Sprintf("{\\p1}m %.2f 0 l %.2f 0 l %.2f %.2f l %.2f %.2f l %.2f %.2f l %.2f %.2f l %.2f %.2f l 0 %.2f l 0 %.2f l %.2f 0{\\p0}", r, w-r, w, r, w, h-r, w-r, h, r, h, 0.0, h-r, 0.0, r, r)
		tailX1 := math.Min(96, w-48)
		tailX2 := math.Min(144, w-16)
		tailX3 := math.Min(120, w-32)
		bubble += fmt.Sprintf("{\\p1}m %.2f %.2f l %.2f %.2f l %.2f %.2f{\\p0}", tailX1, h, tailX2, h, tailX3, h+42)
		textCommon := fmt.Sprintf("{\\alpha&H%02X&\\frz%.2f\\pos(%.2f,%.2f)\\org(%.2f,%.2f)\\fn%s\\c%s\\fs%s}", alpha, -o.Rotation, x+24, y+24, x+w/2, y+h/2, assEscapeTag(maxString(o.Font, "Arial")), assColor(stroke), strconv.FormatFloat(maxFloat(o.FontSize, 42), 'f', -1, 64))
		return []string{common + "{" + shape + "}" + bubble, textCommon + assEscape(o.Text)}, nil
	case "rectangle", "rect":
		return []string{common + "{" + shape + "}" + fmt.Sprintf("{\\p1}m 0 0 l %.2f 0 %.2f %.2f 0 %.2f{\\p0}", w, w, h, h)}, nil
	case "ellipse":
		// ASS cubic bezier commands preserve the browser ellipse silhouette.
		k := 0.5522847498
		var ellipse strings.Builder
		fmt.Fprintf(&ellipse, "{\\p1}m %.2f %.2f ", w, h/2)
		fmt.Fprintf(&ellipse, "b %.2f %.2f %.2f %.2f %.2f %.2f ", w, h/2-k*h/2, w/2+k*w/2, 0.0, w/2, 0.0)
		fmt.Fprintf(&ellipse, "b %.2f %.2f %.2f %.2f %.2f %.2f ", w/2-k*w/2, 0.0, 0.0, h/2-k*h/2, 0.0, h/2)
		fmt.Fprintf(&ellipse, "b %.2f %.2f %.2f %.2f %.2f %.2f ", 0.0, h/2+k*h/2, w/2-k*w/2, h, w/2, h)
		fmt.Fprintf(&ellipse, "b %.2f %.2f %.2f %.2f %.2f %.2f{\\p0}", w/2+k*w/2, h, w, h/2+k*h/2, w, h/2)
		p := ellipse.String()
		return []string{common + "{" + shape + "}" + p}, nil
	case "arrow":
		length := math.Hypot(w, h)
		nx, ny := -h/length*4, w/length*4
		shaft := fmt.Sprintf("m %.2f %.2f l %.2f %.2f %.2f %.2f %.2f %.2f", nx, ny, w+nx, h+ny, w-nx, h-ny, -nx, -ny)
		head := fmt.Sprintf("m %.2f %.2f l %.2f %.2f %.2f %.2f", w, h, w-34, h-10, w-18, h-34)
		arrowShape := "\\c" + assColor(stroke) + "\\bord0"
		return []string{common + "{" + arrowShape + "}{\\p1}" + shaft + "{\\p0}", common + "{" + arrowShape + "}{\\p1}" + head + "{\\p0}"}, nil
	case "freehand":
		if len(o.Points) < 2 {
			return nil, fmt.Errorf("freehand overlay needs at least two points")
		}
		var bodies []string
		for i := 1; i < len(o.Points); i++ {
			a, z := o.Points[i-1], o.Points[i]
			dx, dy := z.X-a.X, z.Y-a.Y
			length := math.Hypot(dx, dy)
			if length == 0 {
				continue
			}
			nx, ny := -dy/length*4, dx/length*4
			poly := fmt.Sprintf("{\\p1}m %.2f %.2f l %.2f %.2f %.2f %.2f %.2f %.2f{\\p0}", a.X+nx, a.Y+ny, z.X+nx, z.Y+ny, z.X-nx, z.Y-ny, a.X-nx, a.Y-ny)
			bodies = append(bodies, common+"{\\c"+assColor(stroke)+"\\bord0}"+poly)
		}
		if len(bodies) == 0 {
			return nil, fmt.Errorf("freehand overlay has no nonzero segments")
		}
		return bodies, nil
	}
	return nil, fmt.Errorf("unsupported overlay kind %q", o.Kind)
}

func maxFloat(v, fallback float64) float64 {
	if v > 0 {
		return v
	}
	return fallback
}

func maxString(v, fallback string) string {
	if v != "" {
		return v
	}
	return fallback
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
