# Rewind Cut Studio mockup

This is a static concept for a calmer editing flow built around one project and three focused states: **Edit** for source selection and timing, **Captions** for cue text and live preview, and **Layers** for text, callouts, drawings, captions, and video composition. Following feedback on the first version, a single wider left panel contains workspace tabs, selected-item controls, and collapsible supporting content. The preview and timeline occupy the remaining area; there is no right-hand tool panel or separate tool rail.

The chapter name comes from the supplied screenshot. The transcript, source cards, and illustrated frame are invented demonstration content; they are not a reconstruction of that episode. The working sample is a fixed 25-second excerpt inside the 23:47 chapter, shown with project timing 01:50–02:15.

The design uses charcoal panels with a restrained teal accent, square controls, mono timecodes, and a bespoke inline SVG sample frame so it works offline. Export opens a simple review dialog instead of exposing encoding settings. The small `styles.css` and `script.js` files are shared by all pages.

## Demo behavior

- Edit, Captions, and Layers links work between pages.
- Export opens a preview-only dialog showing the proposed scope, 1080p MP4 default and caption delivery choices. It performs no export.
- Caption text updates the stage immediately.
- Play buttons simulate playback and advance the scrubber; the scrubber can be clicked.
- Clear, Karaoke and Broadcast presets change the caption preview. Word highlighting uses simulated timing, not speech recognition.
- Layer visibility toggles hide/show their corresponding objects. The title inspector updates the title on the canvas.
- Trim, split, undo, remove, drawing creation and other editing operations are proposed controls; they do not edit media in this demo.
- These are desktop editor mockups. Panels scroll when space is limited.

## Proposed product behavior

This demo does not save edits, process video, persist state between pages, or generate an export. Production work would connect the selected range and layer model to Rewind’s backend, use actual media frames and waveform data, and support keyboard shortcuts, undo history, accessibility announcements, and real export progress.

Open `index.html`, `captions.html` or `layers.html` in a browser. The files use only sibling CSS/JavaScript and inline SVG, and need no build or network service. See `FINDINGS.md` for the current implementation audit and recommended next steps.
