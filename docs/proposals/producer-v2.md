# Proposal: Producer v2 - Self-Hosted Live Production

## Summary

A self-hosted live production system built into Rewind. Multiple hosts join a session with webcams and microphones, arrange content using Show Notes, and output a composited program feed suitable for OBS browser sources or direct viewing.

The goal is to provide the core functionality of cloud-based production tools like Streamyard, but running entirely on your own hardware with full control over quality, bitrate, and routing.

---

## Motivation

Live show production currently relies on either cloud services with limited quality controls or cobbling together Discord/Zoom calls with OBS. Both approaches have significant drawbacks:

- Cloud services limit bitrate and add latency through relay servers
- Voice chat tools (Discord, Zoom) prioritize voice over video quality, leading to frequent drops and low resolution
- No integration with local video libraries or clip workflows

Rewind already has a video archive, clip editor, and export pipeline. Adding live production capabilities lets hosts pull content directly from their library during a show, with webcam compositing and scene management built in.

---

## Architecture

### Show Notes

Show Notes replace the current producer session as the central entity. They contain both content curation and live session state.

A Show Note is an ordered, nested structure of blocks:

- **Section** - a titled container that can hold other blocks (supports nesting)
- **Video** - a reference to an archived video with optional cue notes
- **Clip** - a reference to a saved clip with optional cue notes
- **Break** - a placeholder for intermission, ad reads, or transitions

Hosts collaborate on Show Notes in real time. All changes sync immediately via SSE. Drag-and-drop reordering, expand/collapse for sections, and inline editing for cue notes.

### WebRTC via Pion SFU

A dedicated Pion SFU container handles media routing:

- Each host captures camera and microphone via `getUserMedia`
- Streams route through the SFU to all other participants
- Supports up to 6 simultaneous camera feeds
- Signaling handled via WebSocket through the main web service
- TURN configuration is optional (localhost/LAN works out of the box, external TURN supported for remote hosts)

This approach gives full control over bitrate and encoding. No cloud relay, no artificial quality caps, no dependency on third-party infrastructure.

### Three.js Scene Compositor

The composited output renders in a Three.js scene graph rather than a flat 2D canvas. This provides several advantages:

- The presentation surface (video/clip playback) exists as a positioned 3D mesh
- Webcam feeds render as textured planes that can be positioned, scaled, and rotated in 3D space
- Background effects and overlays compose naturally via the scene graph
- Camera system supports both orthographic (flat layouts) and perspective (depth effects)
- Post-processing pipeline available for bloom, color grading, and other effects
- Scene presets save the full state (camera, mesh transforms, effect parameters) as JSON for quick switching

**Layout presets:**

| Layout        | Description                              |
| ------------- | ---------------------------------------- |
| Solo          | Single camera fullscreen                 |
| Duo           | Side-by-side                             |
| Grid          | 2x2 or 2x3 arrangement                   |
| Content + PiP | Video fullscreen with cameras in corners |
| Content only  | No cameras visible                       |
| Perspective   | Cameras angled toward content with depth |

### Producer Session

When a Show Note goes live, hosts join the Producer Session:

- All hosts connect via WebRTC (camera + microphone)
- Shared view of Show Notes with a "now playing" indicator
- One host acts as director (controls playback for the group)
- Director role can be passed between hosts
- Connection status and stream health visible at a glance

### Program Output

The viewer page receives the composited scene and displays it in a simple fullscreen player:

- Loadable as an OBS browser source for streaming or recording
- Low-latency delivery via WebSocket
- No authentication required for viewers (session URL is the access control)

---

## Implementation Phases

### Phase 1: Show Notes

Database schema and CRUD for Show Notes, blocks, and host access. Nested block editor with drag-and-drop, real-time collaboration via SSE.

### Phase 2: Pion SFU

Dedicated Docker container running the Pion SFU. WebSocket signaling, getUserMedia capture, stream routing, and optional TURN configuration.

### Phase 3: Producer Session UI

Host join flow, camera grid display (up to 6 feeds), director control handoff, and connection status indicators.

### Phase 4: Three.js Compositor

Scene graph setup, video and webcam streams as VideoTexture meshes, layout presets, background effects, and scene preset save/load with animated transitions.

### Phase 5: Program Output

Viewer page, low-latency optimization, OBS browser source compatibility, and session URL sharing.

---

## Technical Decisions

| Decision        | Choice                 | Rationale                                                                |
| --------------- | ---------------------- | ------------------------------------------------------------------------ |
| SFU             | Pion                   | Pure Go, self-hostable, right-sized for small group production           |
| SFU deployment  | Separate container     | Process isolation, independent scaling                                   |
| Compositor      | Three.js               | Scene graph, 3D transforms, particles, post-processing pipeline          |
| TURN            | Optional               | LAN works by default, configurable for remote hosts (Cloudflare, coturn) |
| Host limit      | 6 cameras              | Covers typical podcast and panel configurations                          |
| Program output  | Web page via WebSocket | OBS browser source compatible, simplest delivery path                    |
| Bitrate control | Full local             | No cloud relay bottleneck, configurable quality per stream               |

---

## Out of Scope

These are potential future additions but are not part of this proposal:

- ISO recording (per-source capture to individual files)
- Local RTMP output
- Public guest access via invite links
- Mobile producer interface

---

## Requirements

- Docker (for Pion SFU container)
- Modern browser with WebRTC support (Chrome, Firefox, Edge)
- Webcam and microphone (for hosts)
- NVIDIA GPU optional (for accelerated encoding if recording locally)

---

## References

- [Pion WebRTC](https://github.com/pion/webrtc) - Go WebRTC implementation
- [Three.js](https://threejs.org/) - 3D rendering engine
- [Cloudflare TURN](https://developers.cloudflare.com/calls/turn/) - Managed TURN service
- [Streamyard](https://streamyard.com/) - UX reference
