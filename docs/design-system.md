# Rewind interface primitives

Rewind supports independently selected palettes and color modes, saved per account. Mono is the default palette. Mode defaults to System, with explicit Light and Dark options. Panel and button chrome uses square corners.

## Tokens

`static/css/themes.css` defines `--rw-canvas`, `--rw-surface`, `--rw-surface-raised`, `--rw-field`, `--rw-line`, `--rw-ink`, `--rw-muted`, `--rw-accent`, and `--rw-accent-soft`, with RGB channels for opacity-aware Tailwind utilities. `--rw-radius` is zero. Use semantic tokens instead of per-component color literals.

Mono uses grayscale. Federal uses navy, archival cream, and crimson, inspired by thirdcoast.systems. Amber Terminal uses amber and warm paper. Ultraviolet uses violet, lavender, and cyan details. Each palette supplies light and dark values.

The HTML element carries `data-theme` and `data-color-mode`, initialized from database preferences before rendering. DataStar patches appearance signals on changes. There is no localStorage theme source of truth. Sound/motion saves and appearance saves merge only their own keys so they cannot erase each other.

## Layout

- `settings-layout`: section navigation beside a flexible content column. On mobile, navigation becomes a horizontally scrollable row.
- `settings-section` and `rw-section-heading`: visible category headings and concise descriptions. Categories are not accordions.
- `rw-panel` and `rw-panel-heading`: related controls in a bounded surface with its own description.
- `rw-field-grid`: two columns on desktop and one on mobile.
- `rw-field`: a labeled native input or select. Numeric settings use validated number inputs.
- `rw-toggle-row` and `rw-switch`: a native checkbox with switch semantics for binary preferences; checked state includes both position and color.
- `rw-badge`, `rw-notice`, and `rw-footnote`: compact metadata, live feedback, and secondary guidance.

Every field has a label and stable ID. Focus uses a visible accent outline. Native controls retain keyboard behavior. Settings updates continue through DataStar and server validation.

## Assistant

`agent-dock` starts closed. Its 44-pixel launcher is an icon with an accessible name and tooltip. Opening it exposes the assistant title and a close indicator in the same native disclosure control. The panel stays within the viewport on mobile. Closing it changes only presentation; durable execution continues.

## Migration

Legacy black/white and neutral utilities map to semantic colors so existing forms, navigation, and cards follow the selected theme. New code should prefer canvas/surface/ink/muted/primary utilities. Video-player containers retain dark media controls in every mode. Use literal colors only for media presentation or intentional palette swatches.
