# Proposal: Graphical Filter Controls

## Summary

Replace generic range-slider / number-input controls with purpose-built graphical widgets for every filter type. The current filter UI treats all parameters identically — a label, a slider or number box, and a readout. This is functional but terrible for anything beyond a single scalar. An equalizer shouldn't be a dropdown with hidden numbers. Color temperature shouldn't be a raw 1000–12000 slider. A compressor needs a gain reduction curve, not four mystery numbers.

This proposal designs a bespoke graphical control for every filter and filter group in the system.

---

## Problem Statement

Every filter parameter in the current UI renders through one of six generic controls:

| Control               | Used By                                                                                                                                                             |
| --------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `<input type=range>`  | brightness, contrast, saturation, gamma, exposure, speed, volume, vignette, rotate, sharpen, bass, treble, noise_gate, font_size, color_temp, tint, lift/gamma/gain |
| `<input type=number>` | width, height, frequency, fade duration/offset                                                                                                                      |
| `<select>`            | transpose, denoise, normalize, curves preset, LUT preset, crop, text position                                                                                       |
| `<input type=text>`   | text content                                                                                                                                                        |
| `<input type=color>`  | fade color, pad color, text color                                                                                                                                   |
| Preset `<select>`     | equalizer, compressor, color_balance                                                                                                                                |

Problems:
- **No spatial context.** Rotate is a slider from -180° to 180°. There's no rotation dial, no visual feedback of the angle.
- **No frequency context.** EQ, bass, treble, highpass, lowpass — all frequency-domain filters — are individual sliders with no frequency axis visualization. The equalizer filter is *preset-only* with no way to see or adjust the frequency/gain/width parameters.
- **No dynamics visualization.** The compressor shows preset names. There's no threshold line, no knee visualization, no gain reduction meter. Users can't see what the compressor is doing.
- **No curve visualization.** Curves presets are a dropdown. There's no curve graph showing the tonal mapping.
- **No color wheel.** Color temperature, tint, color balance, and lift/gamma/gain are all color grading tools that professionals expect to see as color wheels or at minimum color-annotated controls.
- **No before/after.** There's no split-view or toggle to see before vs after for any filter.

---

## Design: Per-Filter Graphical Controls

### New `FilterParamType` values

Extend the Go type system to support richer control types. The existing six types remain for backward compatibility; new types are added:

```go
const (
    // Existing
    FilterParamRange  FilterParamType = "range"
    FilterParamSelect FilterParamType = "select"
    FilterParamNumber FilterParamType = "number"
    FilterParamText   FilterParamType = "text"
    FilterParamPreset FilterParamType = "preset"
    FilterParamColor  FilterParamType = "color"

    // New graphical controls
    FilterParamDial        FilterParamType = "dial"          // Rotary knob / angle dial
    FilterParamXYPad       FilterParamType = "xy_pad"        // 2D coordinate pad
    FilterParamCurve       FilterParamType = "curve"         // Bezier curve editor
    FilterParamColorWheel  FilterParamType = "color_wheel"   // HSL color wheel
    FilterParamEQ          FilterParamType = "eq"            // Multi-band EQ graph
    FilterParamCompressor  FilterParamType = "compressor"    // Threshold/ratio/knee graph
    FilterParamGradient    FilterParamType = "gradient"      // Color temperature gradient
    FilterParamWaveform    FilterParamType = "waveform"      // Audio level meter
    FilterParamPosition    FilterParamType = "position"      // 9-point position grid
    FilterParamFadeCurve   FilterParamType = "fade_curve"    // Fade envelope preview
)
```

Each new type gets a dedicated templ component and a small JS behavior module (initialized via `sse.ExecuteScript()` after SSE patches, same pattern as `initScrubInputs`).

---

## Filter-by-Filter Graphical Controls

### SPATIAL FILTERS

#### 1. Crop — Visual Crop Selector

**Current:** Dropdown selecting a saved crop by name.

**Proposed:** Keep the dropdown for saved crops, but add a **thumbnail preview** of what the selected crop looks like. The thumbnail renders a miniature version of the source frame with the crop region highlighted (white border, darkened exterior). Click the thumbnail to open the cut page's crop editor in a modal.

```
┌──────────────────────────────────────┐
│  Crop   [Podcast Closeup     ▾]     │
│  ┌──────────────────────────┐        │
│  │░░░░░░░░░░░░░░░░░░░░░░░░░░│        │
│  │░░░┌────────────┐░░░░░░░░░│        │
│  │░░░│            │░░░░░░░░░│        │
│  │░░░│  selected  │░░░░░░░░░│        │
│  │░░░│   region   │░░░░░░░░░│        │
│  │░░░└────────────┘░░░░░░░░░│        │
│  │░░░░░░░░░░░░░░░░░░░░░░░░░░│        │
│  └──────────────────────────┘        │
└──────────────────────────────────────┘
```

**Implementation:** Server-side rendered `<canvas>` or `<svg>` with normalized crop rect coordinates. The crop data is already in the DB; the handler reads `crop.x, crop.y, crop.width, crop.height` and passes to a `CropThumbnail` templ component that renders an SVG with a highlighted rect.

#### 2. Scale — Aspect-Ratio-Linked Dimensions

**Current:** Single number input for width (height is implicit or separate).

**Proposed:** Two linked dimension inputs with a **chain-link toggle** for locked aspect ratio. An aspect ratio preset row provides common targets.

```
┌──────────────────────────────────────┐
│  Scale                               │
│  W [1920]  🔗  H [1080]             │
│                                      │
│  Presets:                            │
│  [1080p] [720p] [4K] [Square] [9:16] │
└──────────────────────────────────────┘
```

The 🔗 chain icon toggles aspect-lock. When locked, changing width auto-calculates height. Presets set both values at once. All rendered server-side; the lock toggle and preset buttons are DataStar actions.

#### 3. Rotate — Angular Dial

**Current:** Range slider -180° to 180°.

**Proposed:** A **circular dial** with degree tick marks. Drag around the circle to set angle. Snap to 0°/90°/180°/270° with shift held. Numeric readout in center.

```
┌──────────────────────────────────────┐
│  Rotate                              │
│         ╭─── 0° ───╮                 │
│       ╱               ╲              │
│    270°    ┌───────┐    90°          │
│      │     │ -15.5°│     │           │
│       ╲    └───────┘   ╱             │
│         ╰── 180° ──╯                 │
│                  ↻ ← drag indicator  │
│  Snap: [0°] [90°] [180°] [270°]     │
└──────────────────────────────────────┘
```

**Implementation:** SVG circle with a drag handle. `mousedown` → track angle from center via `Math.atan2()`. Snapping: when within 3° of a cardinal, lock. Update signal on drag. Templ renders the SVG structure; JS (via `ExecuteScript`) wires drag behavior.

#### 4. Transpose — Visual Rotation Buttons

**Current:** Dropdown with "CW 90°", "CCW 90°", etc.

**Proposed:** Four visual buttons showing the rotation direction with an arrow icon and a small preview indicating orientation.

```
┌──────────────────────────────────────┐
│  Rotate 90°                          │
│  ┌───┐ ┌───┐ ┌───┐ ┌───┐             │
│  │↱  │ │ ↰ │ │ ↱⇅│ │⇅↰ │             │
│  │CW │ │CCW│ │CW+│ │CCW│             │
│  │   │ │   │ │Flp│ │+Fl│             │
│  └───┘ └───┘ └───┘ └───┘             │
│              ▲ selected              │
└──────────────────────────────────────┘
```

Each button shows a mini rectangle with an orientation arrow. The active option is highlighted with the primary border. Pure templ — no JS needed.

#### 5–6. Flip (H/V) — Toggle Tiles

**Current:** No parameters (zero-param filters).

**Proposed:** No change needed — these are toggles. But enhance the filter card to show a **live preview thumbnail** of the flip effect (mirrored "F" letter or mirrored mini-frame icon).

#### 7. Pad / Letterbox — Visual Padding Editor

**Current:** Width + Height number inputs + color picker.

**Proposed:** A mini **frame-in-frame diagram** showing the video centered in the padded canvas. Drag the edges of the outer frame to adjust padding. Color swatch for the pad color.

```
┌──────────────────────────────────────┐
│  Pad / Letterbox                     │
│  ┌──────────────────────────┐        │
│  │ ██████████████████████████│        │
│  │ ██┌──────────────────┐██ │        │
│  │ ██│                  │██ │        │
│  │ ██│    video area    │██ │        │
│  │ ██│                  │██ │        │
│  │ ██└──────────────────┘██ │        │
│  │ ██████████████████████████│        │
│  └──────────────────────────┘        │
│  W [1920]  H [1080]  Color [■ #000] │
│  Presets: [16:9 Pad] [4:3 Pad]       │
│           [Square]   [Cinemascope]   │
└──────────────────────────────────────┘
```

The ██ region is a `<div>` colored with the pad color, with the inner "video area" as a dark contrasting rectangle. Proportions update as width/height change. Aspect ratio presets provided as buttons.

---

### COLOR & EFFECTS FILTERS

#### 8–11. Brightness / Contrast / Saturation / Gamma — Unified Color Strip

**Current:** Individual range sliders labeled "Value."

**Proposed:** Replace the generic "Value" label with a **gradient-backed slider** that visualizes the parameter's effect. The slider track itself shows what the parameter does:

```
┌──────────────────────────────────────┐
│  Brightness                          │
│  ◄▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓► 0.15  │
│   dark ←────────────────→ bright     │
│                                      │
│  Contrast                            │
│  ◄▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓► 1.20  │
│   flat ←────────────────→ punchy     │
│                                      │
│  Saturation                          │
│  ◄░░░░░░░░░▓▓▓▓▓▓▓▓▓▒▒▒▒▒▒▒▒▒► 1.40  │
│   gray ←────────────────→ vivid      │
│                                      │
│  Gamma                               │
│  ◄▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓► 1.00  │
│   shadows ←─────────────→ highlights │
└──────────────────────────────────────┘
```

**Implementation:**
- **Brightness slider track:** CSS `linear-gradient(to right, #000, #fff)` as the track background.
- **Contrast slider track:** Two-tone gradient that goes from flat gray → high-contrast black/white.
- **Saturation slider track:** `linear-gradient(to right, gray, red, orange, yellow, green, blue, purple)` — a rainbow strip that desaturates to gray on the left.
- **Gamma slider track:** Non-linear brightness gradient (shadows compressed left, highlights right).

The slider thumb position and numeric readout work exactly as today. The gradient is CSS-only — rendered by the templ component via inline `background` style. Zero JS overhead.

#### 12. Exposure — Stop-Based Scale with Zone Indicator

**Current:** Two range sliders (EV, black point).

**Proposed:** EV slider styled as a **photographic stop scale** with f-stop markings. Black point slider with a shadow zone indicator.

```
┌──────────────────────────────────────┐
│  Exposure                            │
│       -3  -2  -1   0  +1  +2  +3    │
│  EV   ──┼───┼───┼──●┼───┼───┼──  0.0│
│                                      │
│  Black Point                         │
│  ◄████████░░░░░░░░░░░░░░░░░░░░░► 0.000│
│  ▲ crushed blacks                    │
└──────────────────────────────────────┘
```

The EV slider has labeled stop marks at each integer value. The black point slider track fades from black (left) to transparent (right), visually showing what "crushing blacks" looks like.

#### 13. Color Temperature — Gradient Thermometer

**Current:** Two range sliders (temperature 1000–12000K, tint -1 to +1).

**Proposed:** A **horizontal color gradient** strip from deep orange (1000K) through white (6500K) to blue (12000K). Slider thumb moves along this strip. Tint slider goes green ↔ magenta.

```
┌──────────────────────────────────────┐
│  Color Temperature                   │
│                                      │
│  Temp ◄🟠🟡⬜🔵► 6500K              │
│         warm ─────── cool            │
│                                      │
│  Tint ◄🟢───⬜───🟣► 0.00           │
│         green ──── magenta           │
│                                      │
│  Presets:                            │
│  [Daylight] [Tungsten] [Fluorescent] │
│  [Cloudy]   [Shade]    [Flash]       │
└──────────────────────────────────────┘
```

**Implementation:** Slider track backgrounds:
- Temperature: `linear-gradient(to right, #ff8a00, #ffd4a0, #ffffff, #a8c8ff, #4080ff)`
- Tint: `linear-gradient(to right, #00ff88, #ffffff, #ff00ff)`

Presets are common white balance settings with predefined K + tint values. Each preset is a button that sets both params at once. Pure CSS gradients + templ presets.

#### 14. Lift / Gamma / Gain — Three Color Wheels (DaVinci Resolve Style)

**Current:** Three range sliders (lift -0.5 to 0.5, gamma 0.5–2.0, gain 0.5–2.0).

**Proposed:** Three **color wheel controls** arranged horizontally, matching the industry-standard DaVinci Resolve / Premiere Lumetri layout. Each wheel is a circular hue ring with a draggable center point (for color shift) and a slider beneath for the luminance component.

```
┌──────────────────────────────────────────────────────┐
│  Lift / Gamma / Gain                                 │
│                                                      │
│    Shadows        Midtones       Highlights           │
│    ╭─────╮        ╭─────╮        ╭─────╮             │
│   ╱  ● ·  ╲     ╱  · ·  ╲     ╱  · ·  ╲            │
│  │  · · ·  │   │  · · ·  │   │  · · ·  │            │
│   ╲  · ·  ╱     ╲  · ·  ╱     ╲  · ·  ╱            │
│    ╰─────╯        ╰─────╯        ╰─────╯             │
│   ◄━━●━━━━━►     ◄━━━━●━━►     ◄━━━━●━━►            │
│     0.05           1.00           1.00               │
│                                                      │
│  [Reset All]                                         │
└──────────────────────────────────────────────────────┘
```

**Implementation:** Each wheel is an SVG with:
- Outer ring: conic HSL gradient (`conic-gradient(red, yellow, lime, aqua, blue, fuchsia, red)`)
- Inner area: radial gradient to white center
- Draggable point: constrained to circle radius, angle → hue, distance from center → saturation
- Slider below: luminance (the current scalar value — lift, gamma, or gain)

This is the most complex widget but essential for color grading. The color wheels affect RGB channels in shadows/mids/highlights. The center dot position maps to color balance offsets (the existing `rs/gs/bs/rm/gm/bm/rh/gh/bh` parameters from color_balance can be repurposed). The slider below controls overall luminosity.

JS behavior: `initColorWheel(svgElement, onChange)` — drag the center dot, compute angle → hue, distance → saturation, update signal. Double-click center to reset.

#### 15. Color Balance — Color Wheel with Presets

**Current:** Preset dropdown (warm/cool/sunset/moonlight/teal_orange). No manual adjustment of the 9 individual RGB channel sliders.

**Proposed:** A **single color wheel** (same widget as lift/gamma/gain but one instance) representing the overall color cast, with the preset buttons around it. Optionally expand to show shadows/mids/highlights tabs.

```
┌──────────────────────────────────────┐
│  Color Balance                       │
│         ╭───────────╮                │
│       ╱    ·    ·     ╲              │
│     │    ·    ●    ·    │            │
│       ╲    ·    ·     ╱              │
│         ╰───────────╯                │
│  ● = warm offset                     │
│                                      │
│  [Warm] [Cool] [Sunset]             │
│  [Moonlight] [Teal & Orange]         │
│                                      │
│  ▸ Advanced (Shadows / Mids / Hi)    │
└──────────────────────────────────────┘
```

Click a preset → dot moves to the corresponding position, params update. "Advanced" expands to three-wheel view (same as lift/gamma/gain) for per-range adjustment.

#### 16. Curves — Interactive Curve Graph

**Current:** Preset dropdown (vintage, cross_process, lighter, etc.).

**Proposed:** A **curve graph** showing a 0–255 → 0–255 tonal mapping. Preset buttons below set predefined curves. Future: draggable control points for custom curves.

```
┌──────────────────────────────────────┐
│  Curves                              │
│  255 ┤                     ╱         │
│      │                   ╱           │
│      │                 ╱             │
│      │              ╱╱               │
│      │           ╱╱                  │
│      │        ╱╱                     │
│      │     ╱╱                        │
│      │  ╱╱                           │
│    0 ┤╱─────────────────────         │
│      0                    255        │
│      IN                    OUT       │
│                                      │
│  [Vintage] [Cross Process] [Lighter] │
│  [Darker] [↑ Contrast] [Negative]   │
└──────────────────────────────────────┘
```

**Phase 1 (preset-driven):** The graph is an SVG `<path>` visualizing the preset's tone curve. Each preset has a predefined set of control points rendered as an SVG path. Clicking a preset updates the path data.

**Phase 2 (interactive):** Add draggable control points on the curve. User clicks to add a point, drags to adjust. Cubic bezier interpolation between points. This requires extending the `curves` filter to accept custom point arrays instead of FFmpeg preset names, and building a custom `curves=r=...` FFmpeg filter string from the points.

#### 17. LUT Preset — Visual Thumbnail Grid

**Current:** Dropdown with preset names.

**Proposed:** A **grid of thumbnail swatches** showing each LUT's color signature applied to a reference gradient strip (or the video's current frame if available). Click to select.

```
┌──────────────────────────────────────┐
│  LUT Preset                          │
│  ┌─────┐ ┌─────┐ ┌─────┐ ┌─────┐   │
│  │Cine │ │Cine │ │Film │ │Blch │   │
│  │Warm │ │Cool │ │Noir │ │Byps │   │
│  │▇▇▇▇▇│ │▇▇▇▇▇│ │▇▇▇▇▇│ │▇▇▇▇▇│   │
│  └─────┘ └─────┘ └─────┘ └─────┘   │
│  ┌─────┐ ┌─────┐ ┌─────┐ ┌─────┐   │
│  │Org& │ │Vntg │ │Hi-C │ │Pstl │   │
│  │Teal │ │Fade │ │ B&W │ │     │   │
│  │▇▇▇▇▇│ │▇▇▇▇▇│ │▇▇▇▇▇│ │▇▇▇▇▇│   │
│  └─────┘ └─────┘ └─────┘ └─────┘   │
│  ┌─────┐ ┌─────┐                    │
│  │Gold │ │Moon │                    │
│  │Hour │ │lite │                    │
│  │▇▇▇▇▇│ │▇▇▇▇▇│                    │
│  └─────┘ └─────┘                    │
└──────────────────────────────────────┘
```

Each thumbnail shows a standard color bar strip (red/green/blue/cyan/magenta/yellow/white/gray/black) with the LUT's CSS filter approximation applied. The selected preset gets a white border highlight. Since LUTs are CSS-approximated in the preview engine already, these thumbnails can use the same CSS filters at small scale.

**Implementation:** Templ component renders a grid of `<button>` elements. Each button contains a `<div>` with `style="filter: ..."` applying the LUT's CSS approximation to a gradient background. Click sets the `preset` param.

#### 18. Sharpen — Strength Dial with Visual Indicator

**Current:** Range slider 0–5.

**Proposed:** A **circular dial** (like rotate, but smaller) or an enhanced slider with a visual sharpness indicator. The slider track transitions from a blurred icon to a sharp icon.

```
┌──────────────────────────────────────┐
│  Sharpen                             │
│  soft ◄▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓► sharp │
│     0 ────────────────────── 5  1.5  │
│       ⚠ render-only, no preview      │
└──────────────────────────────────────┘
```

Minimal change — mostly a labeled gradient track. The key improvement is adding the semantic labels "soft" → "sharp" and showing the render-only badge clearly.

#### 19. Denoise — Visual Strength Selector

**Current:** Dropdown (Light / Medium / Heavy).

**Proposed:** Three **visual cards** showing noise levels. Each card shows a noise pattern (CSS noise texture or SVG grain) that gets progressively less noisy.

```
┌──────────────────────────────────────┐
│  Denoise                             │
│  ┌────────┐ ┌────────┐ ┌────────┐   │
│  │ .:;:.  │ │  .:.   │ │        │   │
│  │.;::;.; │ │ .:. .  │ │   .    │   │
│  │ :;.:;: │ │  . :   │ │        │   │
│  │ Light  │ │ Medium │ │ Heavy  │   │
│  └────────┘ └────────┘ └────────┘   │
│       ⚠ render-only, no preview      │
└──────────────────────────────────────┘
```

Each card is a button with SVG noise filter (`feTurbulence`) at decreasing intensity. Selected card gets highlight border. This replaces the dropdown with a visual that communicates the tradeoff (more denoising = more detail loss).

#### 20. Vignette — Live Gradient Preview

**Current:** Range slider 0–1 labeled "Amount."

**Proposed:** A **mini gradient preview** that live-updates as the slider moves. The preview shows the radial gradient darkening applied to a neutral gray background.

```
┌──────────────────────────────────────┐
│  Vignette                            │
│  ┌──────────────────────────┐        │
│  │ ██████████████████████████│        │
│  │ ████┌──────────────┐████ │        │
│  │ ████│   visible    │████ │        │
│  │ ████│    area      │████ │        │
│  │ ████└──────────────┘████ │        │
│  │ ██████████████████████████│        │
│  └──────────────────────────┘        │
│  ◄▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓► 0.50   │
│  none ────────────────── heavy       │
└──────────────────────────────────────┘
```

The preview `<div>` uses `background: radial-gradient(ellipse, transparent X%, black Y%)` with X/Y derived from the `angle` parameter. Updates on slider `input` event via inline CSS — no JS needed.

#### 21–22. Grayscale / Sepia — Toggle with Preview Swatch

**Current:** No parameters.

**Proposed:** The card shows a **color gradient swatch** with the effect applied, demonstrating what the filter does. Since these are zero-param toggles, the card is compact with just the preview.

```
┌──────────────────────────────────────┐
│  Grayscale ✕                         │
│  ▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇▇         │
│  (gradient strip in grayscale)       │
└──────────────────────────────────────┘
```

---

### TEMPORAL FILTERS

#### 23. Speed — Logarithmic Dial with Time Readout

**Current:** Range slider 0.25×–4×.

**Proposed:** A **speed dial** with logarithmic scaling (so 0.5× and 2× are equidistant from 1×). Shows the resulting duration change. Snap points at common speeds.

```
┌──────────────────────────────────────┐
│  Speed                               │
│               ╭── 1× ──╮            │
│             ╱             ╲          │
│          0.5×    ┌────┐    2×        │
│            │     │1.50×│     │       │
│          0.25×   └────┘    4×        │
│             ╲             ╱          │
│               ╰─────────╯            │
│                                      │
│  Snap: [0.25×] [0.5×] [1×] [2×] [4×]│
│  Duration: 10.0s → 6.67s            │
└──────────────────────────────────────┘
```

The dial is SVG (same component as rotate dial, but with speed markings and logarithmic mapping). Below: snap buttons for common speeds and a computed duration readout showing "original → result" time. The duration readout requires knowing the clip length from signals.

#### 24–25. Fade In / Fade Out — Fade Curve Preview

**Current:** Duration slider + offset number + color picker.

**Proposed:** A **mini timeline bar** showing where the fade occurs within the clip, plus a color preview.

```
┌──────────────────────────────────────┐
│  Fade In                             │
│  ┌──────────────────────────────┐    │
│  │▓▓▓▓▓▓▓░░░░░░░░░░░░░░░░░░░░░│    │
│  │← 0.5s →│                     │    │
│  │offset 0s                     │    │
│  └──────────────────────────────┘    │
│                                      │
│  Duration ◄▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓► 0.5s  │
│  Start At  [0.0] s from clip start  │
│  Color     [■ #000000]              │
└──────────────────────────────────────┘
```

The top bar is a proportional timeline: the shaded region shows the fade zone relative to clip length. The gradient in the shaded region goes from the fade color to transparent (for fade-in) or transparent to fade color (for fade-out). As duration and offset change, the shaded region updates in real time.

For **fade out**, the shaded region is on the right side:
```
│░░░░░░░░░░░░░░░░░░░░░░░░▓▓▓▓▓▓▓│
│                     │← 0.5s →│
```

**Implementation:** Templ renders the bar as a `<div>` with `position: relative`. The fade zone is an absolutely positioned inner `<div>` with width = `(duration / clipDuration) * 100%` and left = `(offset / clipDuration) * 100%`. Background gradient uses the fade color. Clip duration comes from signals.

#### 26. Reverse — Visual Direction Indicator

**Current:** No parameters.

**Proposed:** A compact card with a **timeline direction arrow** showing original vs reversed playback direction.

```
┌──────────────────────────────────────┐
│  Reverse ✕                           │
│  ←←←←←←←←←←←←  playback direction   │
│  ⚠ render-only, requires full decode │
└──────────────────────────────────────┘
```

Minimal — it's a toggle. But the directional arrow and render-only warning are useful visual cues.

---

### AUDIO FILTERS

#### 27. Volume — VU Meter Style

**Current:** Range slider 0–3×, labeled "Gain."

**Proposed:** A **vertical VU meter** styled like a mixing console fader. The slider is vertical with dB markings. Color coding: green (normal), yellow (boosted), red (clipping risk above 2×).

```
┌──────────────────────────────────────┐
│  Volume                              │
│                                      │
│   +9.5dB ┤              red zone     │
│    +6dB  ┤  ▓                        │
│    +3dB  ┤  ▓         yellow zone    │
│     0dB  ┤──▓──  ← unity gain       │
│    -3dB  ┤  ▓                        │
│    -6dB  ┤  ▓         green zone     │
│    -∞dB  ┤  ▓                        │
│          └──┘                        │
│         [1.00×]                      │
│                                      │
│  [Mute]  [0.5×]  [1×]  [1.5×]  [2×] │
└──────────────────────────────────────┘
```

The fader is a vertical `<input type=range>` styled with CSS to look like a channel strip. The track has three color zones. Quick-set buttons below. The gain value is shown both as a multiplier and in dB (`20 * log10(gain)`).

#### 28. Normalize — Mode Toggle with Visual

**Current:** Dropdown (Peak / RMS / Loudnorm).

**Proposed:** Three **visual mode cards** with icons showing the normalization approach.

```
┌──────────────────────────────────────┐
│  Normalize                           │
│  ┌────────┐ ┌────────┐ ┌────────┐   │
│  │  ╱╲    │ │  ╱\    │ │ ▇▇▇▇▇▇│   │
│  │ ╱  ╲╱╲ │ │╱╲╱\╱╲ │ │ ▇▇▇▇▇▇│   │
│  │╱      ╲│ │      ╲│ │ ▇▇▇▇▇▇│   │
│  │  Peak  │ │  RMS  │ │Loudnorm│   │
│  └────────┘ └────────┘ └────────┘   │
│                                      │
│  Peak: scales to loudest sample      │
│  RMS: scales to average loudness     │
│  Loudnorm: EBU R128 (-23 LUFS)      │
└──────────────────────────────────────┘
```

Each card shows a stylized waveform: Peak shows dynamic range preserved, RMS shows averaged levels, Loudnorm shows uniform broadcast-level blocks. Selected mode gets highlight border. Description text below explains the selected mode.

#### 29. Equalizer — Graphic EQ Display

**Current:** Preset dropdown only. Underlying params (frequency, width, gain) exist but are hidden behind presets.

**Proposed:** A **multi-band graphic EQ graph** showing the frequency response curve. Preset buttons set the curve shape. Future: draggable band control points.

```
┌──────────────────────────────────────────────────────┐
│  Equalizer                                           │
│  +12dB ┤                                             │
│   +6dB ┤                   ╱╲                        │
│    0dB ┤──────────────────╱──╲────────────────       │
│   -6dB ┤                      ╲                      │
│  -12dB ┤                       ╲                     │
│        └─┼────┼────┼────┼────┼────┼────┼────┤        │
│         50   100  250  500  1k   2k   5k   10k  Hz  │
│                                                      │
│  Presets:                                            │
│  [Voice Clarity] [Bass Boost] [Treble Boost]         │
│  [De-Mud]        [Air]        [Sub Cut]              │
│                                                      │
│  ▸ Manual: Freq [3000] Hz  Width [2000]  Gain [+4dB]│
└──────────────────────────────────────────────────────┘
```

**Phase 1:** SVG graph with logarithmic frequency axis (20Hz–20kHz). The EQ curve is an SVG `<path>` computed from the preset's frequency/width/gain parameters using a peaking filter response curve. Preset buttons update the path.

**Phase 2:** Add draggable band nodes on the curve. Click the curve to add a band. Drag vertically for gain, horizontally for frequency. Drag the edges of the node for bandwidth. This requires extending the filter to accept multiple bands (currently single-band only). Multiple `equalizer=` FFmpeg filters chain together.

**Implementation:** The frequency response curve for a peaking EQ filter with center freq `f`, bandwidth `w`, and gain `g` is well-defined. Compute ~50 points across 20Hz–20kHz using the transfer function, render as SVG path. Logarithmic x-axis: `x = log10(freq/20) / log10(20000/20) * width`.

#### 30–31. Bass / Treble — Tilt EQ Visualization

**Current:** Single dB slider for each.

**Proposed:** A **mini frequency response graph** showing the shelf filter curve, integrated with the slider.

```
┌──────────────────────────────────────┐
│  Bass                                │
│  +12dB ┤╲                            │
│    0dB ┤──╲───────────────────       │
│  -12dB ┤                             │
│        └──┼────┼────┼────┼───        │
│          50  100  500  1k  Hz        │
│  ◄▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓► +6.0dB  │
│  cut ──────────────────── boost      │
│                                      │
│  Treble                              │
│  +12dB ┤           ╱                 │
│    0dB ┤──────────╱──────            │
│  -12dB ┤                             │
│        └──┼────┼────┼────┼───        │
│          500  1k   5k  10k Hz        │
│  ◄▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓► +6.0dB  │
│  cut ──────────────────── boost      │
└──────────────────────────────────────┘
```

Each filter shows a mini SVG graph of the shelf response. The curve shape updates as the dB slider moves:
- **Bass:** Low-shelf filter at 200Hz. Gain above 0 → left side of curve rises. Below 0 → dips.
- **Treble:** High-shelf filter at 4kHz. Gain above 0 → right side rises.

The graph is tiny (50×30px per filter) but immediately communicates what the filter does to the frequency spectrum.

#### 32–33. Highpass / Lowpass — Filter Cutoff Visualization

**Current:** Number input for frequency (20–20000 Hz).

**Proposed:** A **mini frequency response graph** with a draggable cutoff point.

```
┌──────────────────────────────────────┐
│  High Pass (removes lows)            │
│   0dB ┤       ╱───────────────       │
│       ┤     ╱                        │
│ -48dB ┤───╱                          │
│       └──┼───┼───┼───┼───┼───        │
│         20  50  200 1k  5k  Hz       │
│                ▲ cutoff = 200 Hz     │
│                                      │
│  ◄▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓► 200Hz│
│  20 Hz ─────────────────── 20kHz     │
│  [80Hz] [120Hz] [200Hz] [500Hz]      │
└──────────────────────────────────────┘
```

```
┌──────────────────────────────────────┐
│  Low Pass (removes highs)            │
│   0dB ┤────────────╲                 │
│       ┤              ╲               │
│ -48dB ┤                ╲───          │
│       └──┼───┼───┼───┼───┼───        │
│         20  200  1k  3k  10k Hz      │
│                    ▲ cutoff = 3kHz   │
│                                      │
│  ◄▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓► 3000 │
│  20 Hz ─────────────────── 20kHz     │
│  [1kHz] [3kHz] [5kHz] [8kHz]        │
└──────────────────────────────────────┘
```

The frequency slider is logarithmic (linear in log-space: 20→20000). The SVG graph shows the filter rolloff curve (12dB/octave for single-pole). The cutoff frequency has a vertical marker. Preset buttons for common cutoffs. The slider track could use a `linear-gradient` showing the frequency spectrum (similar to the EQ graph axis).

**Implementation:** Transfer function for 1st-order butterworth: `H(f) = 1 / sqrt(1 + (f/fc)^2)` for lowpass, reciprocal for highpass. Plot ~30 points, logarithmic x-axis.

#### 34. Compressor — Dynamics Curve Graph

**Current:** Preset dropdown (Light / Medium / Heavy / Broadcast). Underlying params (threshold, ratio, attack, release) hidden.

**Proposed:** A **dynamics transfer curve** showing input level (x-axis) vs output level (y-axis), with threshold knee and ratio slope visible. Attack/release shown as a time constant indicator.

```
┌──────────────────────────────────────────────────────┐
│  Compressor                                          │
│                                                      │
│  0dB  ┤                  ╱╱ ← ratio 4:1             │
│       │               ╱╱                             │
│       │            ╱╱      ← 1:1 (uncompressed)      │
│       │         ╱╱ ╱╱╱╱                               │
│       │      ╱╱╱╱     threshold = -20dB               │
│       │   ╱╱           ↕                              │
│ -60dB ┤╱╱──────────────────────     -60dB → 0dB      │
│       └─────────────────────────                      │
│       -60dB                 0dB  (input)              │
│                                                      │
│  Presets: [Light] [Medium] [Heavy] [Broadcast]       │
│                                                      │
│  ▸ Advanced:                                         │
│  Threshold ◄████████████████░░░░► -20dB              │
│  Ratio     ◄████████░░░░░░░░░░░►  4:1               │
│  Attack    ◄████░░░░░░░░░░░░░░░►  20ms              │
│  Release   ◄██████████████░░░░░► 250ms              │
└──────────────────────────────────────────────────────┘
```

The graph is an SVG showing two lines:
- **Unity line** (1:1): diagonal from bottom-left to top-right (thin, white/20)
- **Compressed line**: follows unity up to the threshold, then bends to the compression ratio

As threshold moves, the knee point shifts left/right. As ratio changes, the slope above the knee changes. Attack and release are shown with labeled sliders in the "Advanced" expander.

**Implementation:** The curve has two segments:
1. Below threshold: `y = x` (unity)
2. Above threshold: `y = threshold + (x - threshold) / ratio`

Plot as SVG path. Update on any parameter change. Preset buttons set all four params at once.

#### 35. Noise Gate — Threshold Meter

**Current:** Range slider -60 to 0 dB.

**Proposed:** A **vertical level meter** with a draggable horizontal threshold line. Audio below the line is silenced (shown in red), above passes through (green).

```
┌──────────────────────────────────────┐
│  Noise Gate                          │
│     0dB ┤                            │
│   -10dB ┤  ▓▓▓  ← audio passes      │
│   -20dB ┤  ▓▓▓                       │
│   -30dB ┤  ▓▓▓                       │
│ -------- -40dB threshold --------    │
│   -40dB ┤  ░░░  ← gated (silent)    │
│   -50dB ┤  ░░░                       │
│   -60dB ┤  ░░░                       │
│                                      │
│  ◄████████████████░░░░░░░░░► -40dB   │
│  -60dB ──────────────────── 0dB      │
└──────────────────────────────────────┘
```

The meter shows a fixed visual level range. The threshold line bisects it: above = green (audio passes), below = red/gray (gated). Dragging the threshold line or using the slider moves the gate threshold. This immediately communicates "everything below this line gets muted."

**Implementation:** SVG with a colored bar (green above threshold, red/gray below) and a horizontal draggable line. Line position = `(threshold - (-60)) / 60 * height`.

#### 36–37. Audio Fade In / Out — Fade Envelope Curve

**Current:** Duration number + offset number + curve type dropdown.

**Proposed:** A **fade envelope graph** showing the gain curve over time, with the selected curve shape visible.

```
┌──────────────────────────────────────┐
│  Audio Fade In                       │
│  1.0 ┤         ╱────────────         │
│      │       ╱                       │
│      │     ╱                         │
│      │   ╱   ← exponential curve     │
│  0.0 ┤──╱                            │
│      └──┼────┼────┼────┼────         │
│        0s  0.5s   1s  1.5s   time    │
│           ▲ dur=0.5s                 │
│                                      │
│  Duration ◄▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓► 0.5s   │
│  Start At  [0.0] s                   │
│  Curve  [Linear ▾] [●  ●  ●  ●  ●  ]│
│          tri  qsin esin log par exp  │
└──────────────────────────────────────┘
```

The graph shows the gain envelope from 0 to 1 (fade-in) or 1 to 0 (fade-out) over time. The curve shape changes with the selected curve type — each mathematical function (`tri` = linear, `qsin` = quarter sine, `esin` = exponential sine, etc.) produces a visibly different path. The curve type selector shows small curve-shape icons instead of text labels.

**Implementation:** SVG path computed from the curve function:
- `tri`: linear `y = t`
- `qsin`: `y = sin(t * π/2)`
- `esin`: `y = 1 - cos(t * π/2)` or similar
- `log`: `y = log(1 + t * (e-1))`
- `par`: `y = t²`
- `exp`: `y = (e^t - 1) / (e - 1)`

Where `t` = normalized time 0→1.

#### 38. Mute — Simple Toggle

No change — already a zero-param toggle. Card shows a speaker-off icon.

---

### OVERLAY FILTERS

#### 39. Text / Watermark — Live Position Preview

**Current:** Text input + position dropdown + size slider + color picker.

**Proposed:** A **9-point position grid** replacing the dropdown, plus a mini preview showing the text positioned on a thumbnail.

```
┌──────────────────────────────────────┐
│  Text / Watermark                    │
│                                      │
│  Text  [Hello World            ]     │
│                                      │
│  Position:                           │
│  ┌───┬───┬───┐                       │
│  │ ◉ │ ○ │ ○ │  top-left selected    │
│  ├───┼───┼───┤                       │
│  │ ○ │ ○ │ ○ │                       │
│  ├───┼───┼───┤                       │
│  │ ○ │ ○ │ ○ │                       │
│  └───┴───┴───┘                       │
│                                      │
│  ┌────────────────────────┐          │
│  │Hello World             │          │
│  │                        │          │
│  │                        │          │
│  │                        │          │
│  └────────────────────────┘          │
│  ▲ live preview                      │
│                                      │
│  Size  ◄▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓► 24px   │
│  Color [■ #ffffff]                   │
└──────────────────────────────────────┘
```

The 9-point grid is a `<table>` or CSS grid with radio-style dots. Clicking a dot sets the `position` param. The preview box below shows a thumbnail with the text rendered at the selected position, size, and color. The preview is a `<div>` with the text absolutely positioned using the same alignment rules as the FFmpeg `drawtext` position expressions.

---

## Shared Widget Library

Several filters share the same underlying graphical widget. Build these as reusable components:

### `DialWidget` (SVG circular knob)
Used by: **rotate**, **speed**, **sharpen** (optional)

Features:
- SVG circle with tick marks at configurable positions
- Draggable handle on the circumference
- `mousedown → mousemove → mouseup` tracking via `atan2()`
- Numeric readout in center
- Configurable: min/max/step, snap points, label format
- Shift+drag for fine control (1/10 step)

### `FreqResponseGraph` (SVG frequency spectrum)
Used by: **equalizer**, **bass**, **treble**, **highpass**, **lowpass**

Features:
- Logarithmic x-axis (20Hz–20kHz)
- dB y-axis (-12dB to +12dB or configurable)
- Frequency grid lines at 50, 100, 250, 500, 1k, 2k, 5k, 10k
- SVG `<path>` from computed filter response points
- Configurable filter type: peaking, lowshelf, highshelf, highpass, lowpass

### `ColorWheelWidget` (SVG/Canvas color wheel)
Used by: **lift_gamma_gain** (×3), **color_balance**

Features:
- Conic gradient hue ring
- Radial gradient saturation (outer = full sat, center = neutral)
- Draggable center dot, constrained to circle radius
- Position → hue (angle) + saturation (distance from center)
- Reset on double-click
- Slider below for luminance/value component

### `GradientSlider` (CSS gradient-backed range input)
Used by: **brightness**, **contrast**, **saturation**, **gamma**, **exposure**, **color_temp**, **tint**, **volume**

Features:
- Standard `<input type=range>` with custom CSS
- Track background is a CSS `linear-gradient` specific to the parameter
- Semantic labels at ends of the range (e.g., "dark ← → bright")
- Custom thumb styling for visibility on gradient backgrounds

### `MiniTimelineBar` (fade position indicator)
Used by: **fade_in**, **fade_out**, **audio_fade_in**, **audio_fade_out**

Features:
- Horizontal bar representing clip duration
- Highlighted region with gradient showing fade zone
- Region width = `duration / clipDuration`, position = `offset / clipDuration`
- Fade color applied to gradient start/end

### `DynamicsCurveGraph` (SVG transfer curve)
Used by: **compressor**, (future: limiter, expander)

Features:
- X/Y axes: input level vs output level (-60dB to 0dB)
- Unity line (1:1 diagonal, thin)
- Transfer curve with knee at threshold
- Curve slope above threshold = 1/ratio
- Update on threshold/ratio drag

### `PositionGrid` (3×3 point selector)
Used by: **text** position param

Features:
- 3×3 CSS grid of radio-button dots
- Click to select position
- Highlighted dot shows current selection
- Maps to 7-value enum: top-left, top-center, top-right, center, bottom-left, bottom-center, bottom-right

---

## Implementation Approach

### Phase 1: CSS-Only Upgrades (No New JS)

These improvements use only CSS changes to existing `<input type=range>` elements and templ structure changes. Zero new JavaScript.

| Filter          | Change                                    |
| --------------- | ----------------------------------------- |
| brightness      | Gradient slider track (dark → light)      |
| contrast        | Gradient slider track (flat → punchy)     |
| saturation      | Rainbow gradient slider track             |
| gamma           | Nonlinear brightness gradient track       |
| exposure EV     | Stop markings on slider                   |
| exposure black  | Dark gradient track                       |
| color_temp temp | Orange → white → blue gradient track      |
| color_temp tint | Green → white → magenta gradient track    |
| volume          | Colored zones (green/yellow/red) on track |
| bass/treble     | dB labels on track ends                   |
| sharpen         | Soft → sharp labels                       |
| vignette        | + Mini gradient preview div above slider  |
| transpose       | Visual icon buttons replacing dropdown    |
| denoise         | Visual icon cards replacing dropdown      |
| normalize       | Visual icon cards replacing dropdown      |
| text position   | 3×3 grid replacing dropdown               |
| grayscale/sepia | Color swatch preview in card              |
| LUT preset      | Thumbnail swatch grid replacing dropdown  |

Effort: Small templ changes per filter + CSS additions. No new JS files. Biggest bang for the buck.

### Phase 2: SVG Graph Widgets

Build the `FreqResponseGraph` and `DynamicsCurveGraph` SVG components. These are server-rendered SVGs that update when parameters change (via SSE re-render on signal change).

| Filter      | Widget                         |
| ----------- | ------------------------------ |
| equalizer   | FreqResponseGraph (peaking)    |
| bass        | FreqResponseGraph (low-shelf)  |
| treble      | FreqResponseGraph (high-shelf) |
| highpass    | FreqResponseGraph (highpass)   |
| lowpass     | FreqResponseGraph (lowpass)    |
| compressor  | DynamicsCurveGraph             |
| noise_gate  | Level meter with threshold     |
| curves      | Tone curve graph               |
| audio fades | Fade envelope curve            |
| fade in/out | Mini timeline bar              |

These graphs are computed in Go (the transfer function math is straightforward) and rendered as SVG `<path>` elements in templ. No client-side computation needed. When a parameter changes, the data-effect triggers `@post()`, the handler recomputes the SVG points, and patches the filter card.

### Phase 3: Interactive Dial & Color Wheel Widgets

Build the JS-interactive widgets that require drag behavior:

| Filter          | Widget                         |
| --------------- | ------------------------------ |
| rotate          | DialWidget (angle)             |
| speed           | DialWidget (logarithmic speed) |
| lift_gamma_gain | 3× ColorWheelWidget            |
| color_balance   | ColorWheelWidget + presets     |

Each widget has:
1. A templ component that renders the SVG/HTML structure
2. A JS module (e.g., `static/js/lib/dial-widget.js`) with `initDialWidget(el, opts, onChange)`
3. `sse.ExecuteScript()` in the handler to wire behavior after SSE patch (same pattern as `initScrubInputs`)

### Phase 4: Live Preview Thumbnails

For filters that benefit from seeing a preview of the effect:
- LUT swatches with CSS filter approximations
- Vignette mini-preview
- Text position preview
- Crop region thumbnail
- Grayscale/sepia color strips

These are pure CSS/HTML rendered in templ — no JS. They update on re-render when parameters change.

---

## Files Modified

### New Files
- `static/js/lib/dial-widget.js` — Circular dial drag behavior
- `static/js/lib/color-wheel-widget.js` — Color wheel drag behavior
- `static/css/filter-controls.css` — Gradient slider tracks, graph styles (or inline in Tailwind)

### Modified Files
- `pkg/filters/filter_defs.go` — New `FilterParamType` values, updated `ParamsForFilterType` to use new types
- `pkg/filters/filter_graphs.go` — (new) SVG path computation for frequency response, dynamics curves, fade envelopes, tone curves
- `cmd/web/templates/components/filter_stack.templ` — New templ branches for each graphical control type
- `cmd/web/handlers/api/video_api/filter_cards.go` — Compute SVG data in handler before rendering
- `static/css/input.css` — Tailwind custom components for gradient sliders

### No Changes
- `pkg/ffmpeg/filter_compiler.go` — FFmpeg compilation unchanged
- Database schema — No migration needed
- `static/js/lib/filter-preview-engine.js` — Live preview engine unchanged
- Signal structure — `_filterStack` format unchanged

---

## Summary of Controls By Filter

| Filter             | Current Control               | Proposed Graphical Control                    |
| ------------------ | ----------------------------- | --------------------------------------------- |
| crop               | select dropdown               | Dropdown + crop region thumbnail              |
| scale              | number input                  | Linked W×H inputs + aspect lock + presets     |
| transpose          | select dropdown               | 4 visual icon buttons                         |
| rotate             | range slider                  | Circular dial with degree marks               |
| hflip / vflip      | (none)                        | Mirrored icon preview                         |
| pad                | numbers + color               | Frame-in-frame diagram + color swatch         |
| brightness         | range slider                  | Gradient slider (dark → light)                |
| contrast           | range slider                  | Gradient slider (flat → punchy)               |
| saturation         | range slider                  | Rainbow gradient slider                       |
| gamma              | range slider                  | Nonlinear brightness gradient slider          |
| exposure           | range sliders                 | EV stop scale + dark gradient for black pt    |
| color_temp         | range sliders                 | Orange→blue gradient + green→magenta gradient |
| lift_gamma_gain    | 3 range sliders               | 3 color wheels (DaVinci style)                |
| color_balance      | preset dropdown               | Color wheel + preset buttons                  |
| curves             | preset dropdown               | Interactive tone curve graph                  |
| lut                | preset dropdown               | Thumbnail swatch grid                         |
| sharpen            | range slider                  | Labeled gradient slider                       |
| denoise            | select dropdown               | 3 visual noise-level cards                    |
| vignette           | range slider                  | Slider + live gradient preview                |
| grayscale / sepia  | (none)                        | Color strip preview                           |
| speed              | range slider                  | Logarithmic speed dial + duration readout     |
| fade_in / fade_out | range + number + color        | Mini timeline bar with fade zone              |
| reverse            | (none)                        | Direction arrow indicator                     |
| volume             | range slider                  | Vertical VU meter fader                       |
| normalize          | select dropdown               | 3 visual mode cards                           |
| equalizer          | preset dropdown               | Frequency response graph + presets            |
| bass / treble      | range slider                  | Mini freq response graph + dB slider          |
| highpass / lowpass | number input                  | Freq response graph + log slider + presets    |
| compressor         | preset dropdown               | Dynamics transfer curve graph                 |
| noise_gate         | range slider                  | Level meter with threshold line               |
| audio fades        | number + select               | Fade envelope graph + curve shape icons       |
| mute               | (none)                        | (no change)                                   |
| text               | text + select + range + color | 9-point position grid + live preview          |
