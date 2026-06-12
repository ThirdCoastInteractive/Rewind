# Proposal: Sitespeed.io Performance Testing

## Summary

Set up a complete performance testing pipeline using sitespeed.io, Graphite, and Grafana. The suite covers every page in the application, with dedicated stress tests for the video player on multi-hour videos where we know performance degrades. All services run via Docker Compose under a `perf` profile so they don't interfere with normal development.

---

## Problem Statement

### No systematic performance measurement

The app has grown to 25+ pages spanning video playback, two NLE editors (Stitch and Compose), a producer/remote player system, admin dashboards, and various settings pages. There is no automated way to measure page load times, layout shift, or interaction responsiveness across this surface area. Regressions are caught by "it feels slow" during manual testing.

### Video player performance on long videos

The video player loads multiple supporting data structures that scale with video duration:

- **Waveform peaks** — binary Int16Array loaded from `/api/videos/:id/waveform/peaks.i16`. At 100ms bucket resolution, a 10-hour video produces ~360K samples (~720KB). The canvas renderer iterates per-pixel (up to 1920 iterations) with a nested loop through peaks buckets. For very long videos this becomes the dominant blocking operation on page load.

- **Marker DOM recreation** — `MarkerManager.render()` clears and recreates all marker tick and range DOM elements on every `render()` call. This is triggered by `renderIfNeeded()` on every `timeupdate` event. A multi-hour video with SponsorBlock segments and user markers can have 50-100+ elements being destroyed and recreated.

- **Clip timeline rendering** — Same pattern as markers. `ClipManager.renderTimeline()` clears and recreates all clip-range DOM elements.

- **Transcript auto-scroll** — `TranscriptManager.onTimeUpdate()` fires on every `timeupdate`, iterating through all visible cue elements to find the active one, then calling `scrollTo({ behavior: 'smooth' })` which can trigger layout thrashing.

- **Seek thumbnail VTT** — Lazy-loaded, but for a 10-hour video the VTT file itself can be large with thousands of cue entries for the sprite sheet grid.

We suspect multi-hour videos cause noticeable jank. The perf suite will provide concrete numbers (Total Blocking Time, Long Tasks, FCP, LCP) to quantify the problem and track improvement.

### Active development churn

The codebase is in the middle of several large changes: the Compose NLE redesign, the editor unification proposal, the graphical filter controls work, and the universal source types implementation. Performance testing needs to run alongside this work to catch regressions early as these complex features land.

---

## Current State Audit

### What existed before

A partial sitespeed.io setup was already scaffolded in `bin/dev/sitespeed/`:

| Component | State | Issues |
|---|---|---|
| `config/budgets.json` | Existed | No per-page overrides for heavy pages |
| `config/environments.json` | Existed | Fine as-is (local + CF tunnel) |
| `config/page-groups.json` | Existed | Missing 8 pages (stitch, compose, job detail, asset health) |
| `scripts/main-pages.mjs` | Existed | Only tested 3 pages (home, videos, jobs) |
| `scripts/scroll-test.mjs` | Existed | Fine as-is |
| `scripts/video-player.mjs` | Existed | Required manual `--videoId` flag; no auto-discovery |
| `scripts/lib/` | Existed | Auth, config, navigation helpers — usable |
| `scripts/suites/` | Empty | No suite scripts at all |
| Docker services | Missing | No Graphite, Grafana, or sitespeed services in docker-compose.yml |
| Makefile targets | Missing | No `make perf*` targets |

### Pages that were NOT being tested

- `/settings` and `/settings/keybindings` — user settings
- `/bookmarklet` — bookmarklet installer
- `/stitch` and `/stitch/:id` — stitch library and editor
- `/compose` and `/compose/:id` — compose library and editor
- `/jobs/:id` — job detail page
- `/admin/asset-health` — asset health check (potentially slow with many assets)
- `/producer/sessions/manage` — session management
- No admin page tests at all (home, settings, users, exports)

---

## Changes Made

### 1. Page groups inventory (`config/page-groups.json`)

Added all missing pages organized into groups:

| Group | Pages | Notes |
|---|---|---|
| `public` | 4 pages | Home, login, register, player join |
| `user` | 6 pages | Videos, jobs, settings, keybindings, cookies, bookmarklet |
| `video-detail` | 2 pages | Video detail + cut (requires videoId) |
| `jobs-detail` | 1 page | Job detail (requires jobId) |
| `stitch` | 1 page | Stitch library |
| `stitch-detail` | 1 page | Stitch editor (requires stitchId) |
| `compose` | 1 page | Compose library |
| `compose-detail` | 1 page | Compose editor (requires composeId) |
| `admin` | 5 pages | Dashboard, settings, users, exports, asset health |
| `producer` | 2 pages | Producer home, session management |
| **Total** | **24 pages** | |

### 2. Performance budgets (`config/budgets.json`)

Added per-page budget overrides for known-heavy pages:

| Page | FCP | LCP | TBT | Long Tasks | Rationale |
|---|---|---|---|---|---|
| Default | 1000ms | 2500ms | 200ms | 5 | Standard web vitals targets |
| `video-player-load` | 1500ms | 3500ms | 400ms | 10 | Player initialization + waveform |
| `video-player-longest` | 2000ms | 5000ms | 800ms | 20 | Worst-case long video; expected to fail initially |
| `video-cut` | 1500ms | 3500ms | 500ms | 10 | Cut page with clip/filter rendering |
| `stitch-editor` | 1500ms | 3000ms | 400ms | — | Timeline + canvas rendering |
| `compose-editor` | 1500ms | 3500ms | 500ms | — | Multi-layer canvas + inspector |

The `video-player-longest` budget is deliberately relaxed but will still likely fail on multi-hour videos — this gives us a concrete baseline target to optimize toward.

### 3. Test scripts

**`main-pages.mjs`** — Rewritten to test all 24 pages in sequence: public → login → authenticated user pages → stitch/compose libraries → producer → admin. Each page is measured individually with `clearPage()` between measurements.

**`video-player.mjs`** — Rewritten with auto-discovery:
1. Navigates to `/videos?sort=duration-desc` to sort by longest duration
2. Scrapes the first video card link (the longest video)
3. Tests video detail page load with extended wait (3s for waveform/markers)
4. Tests player interactions (play button click, seek bar interaction, sidebar scroll)
5. Tests the cut page load and interaction for the same video
6. Falls back to any available video if sorting fails

**`suites/editors.mjs`** — New. Tests stitch and compose library pages, then discovers and opens the first available project in each to test editor load and interaction.

**`suites/admin.mjs`** — New. Tests all 5 admin pages including asset health.

**`scroll-test.mjs`** — Unchanged (already good for CLS measurement).

### 4. Docker Compose services

Added three services under the `perf` profile:

```yaml
graphite:   # graphiteapp/graphite-statsd:1.1.10-5 — Carbon + StatsD on ports 2003, 8125
grafana:    # grafana/grafana:11.4.0 — Dashboard UI on port 3000
sitespeed:  # sitespeedio/sitespeed.io:35.12.1 — Browsertime runner with 1g shared memory
```

All use Docker Compose profiles so `docker compose up` won't start them. They're activated by `--profile perf` or individual `docker compose run --rm sitespeed`.

### 5. Makefile targets

| Target | What it does |
|---|---|
| `make perf` | Run all main page tests |
| `make perf-player` | Run video player stress test (longest video) |
| `make perf-scroll` | Run scroll/CLS test |
| `make perf-editors` | Run stitch/compose editor tests |
| `make perf-admin` | Run admin page tests |
| `make perf-graphite` | Run ALL test suites and push metrics to Graphite |
| `make perf-dashboards` | Start Grafana + Graphite (http://localhost:3000) |
| `make perf-clean` | Delete results and video recordings |

---

## Expected Results

### Pages that should pass budgets easily

Public pages, settings, bookmarklet, producer — these are lightweight server-rendered pages with minimal JS. Expected FCP < 500ms, TBT near zero.

### Pages that should be borderline

- **Videos index** — loads a grid of video cards with thumbnails. SSE-driven, so initial paint is fast but content pops in. May show elevated CLS.
- **Jobs page** — similar SSE pattern, card grid.
- **Admin exports / asset health** — potentially large lists depending on data volume.

### Pages expected to FAIL budgets

- **`video-player-longest`** — Multi-hour videos will likely blow past TBT and LCP budgets. The waveform canvas render, marker DOM rebuild, and transcript initialization all block the main thread. This is the primary optimization target.
- **`video-cut-longest`** — Same video, plus the clip manager and filter card system. Even more JS initialization.
- **Stitch/Compose editors** — If projects have many segments/layers, timeline rendering and canvas setup can block. Depends on project complexity.

### Metrics to focus on

1. **Total Blocking Time (TBT)** — directly measures main-thread jank during page load. The primary signal for "player feels slow."
2. **Long Tasks** — individual tasks > 50ms. Identifies specific bottlenecks.
3. **Largest Contentful Paint (LCP)** — when the video player element or main content area renders.
4. **Cumulative Layout Shift (CLS)** — important for SSE-driven pages where content pops in asynchronously.
5. **Speed Index** — visual completeness over time; catches slow waveform renders.

---

## Recommended Next Steps

### Immediate (use perf data to prioritize)

1. **Run `make perf-player`** and record baseline TBT/LCP for the longest video. This is the known pain point.
2. **Run `make perf`** to get a baseline across all pages. Identify any surprising failures.
3. **Set up Grafana dashboards** (`make perf-dashboards`) and import the [sitespeed.io dashboard templates](https://www.sitespeed.io/documentation/sitespeed.io/performance-dashboard/#dashboards).

### Short-term optimizations (based on expected bottlenecks)

1. **Virtualize marker/clip rendering** — instead of clearing and recreating all DOM elements, diff against existing state or use a virtual list for large collections.
2. **Debounce transcript auto-scroll** — batch `onTimeUpdate` calls with `requestAnimationFrame` and avoid `scrollTo` on every event.
3. **Offload waveform rendering** — move the per-pixel peaks loop to a Web Worker or use OffscreenCanvas so it doesn't block the main thread.
4. **Lazy-load waveform for long videos** — only render the visible viewport of the waveform and load peak data on-demand as the user scrolls/seeks.

### Longer-term integration

1. **CI integration** — run `make perf-graphite` in CI after deploys and fail builds that exceed budgets.
2. **Budget tightening** — as optimizations land, reduce the relaxed budgets for `video-player-longest` toward the standard targets.
3. **Comparison testing** — run tests against both local and CF tunnel environments to measure real-world latency impact.
