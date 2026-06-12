# E2E Testing Plan

## Overview

Playwright-based end-to-end tests that run against the live Docker Compose stack.
Tests verify user-facing behavior through browser automation — DataStar SSE
roundtrips, filter stack mutations, editor workflows, and export pipelines.

## Quick Start

```bash
# Prerequisites: Docker stack running
make up

# Run all E2E tests
make e2e

# Run with browser visible
pnpm run e2e:headed

# Run with Playwright UI
pnpm run e2e:ui
```

## Architecture

```
tests/e2e/
  helpers.ts              # Shared login, navigation, filter stack helpers
  filter-stack.spec.ts    # Filter stack add/remove/reorder/param tests
```

- **Config:** `playwright.config.ts` — Chromium only, single worker,
  connects to `http://localhost:$WEBSERVER_PORT`
- **No dev server management** — tests assume the full stack is already
  running via `docker compose up`
- **Auth:** Tests log in via session form; cookies persist within each test

## Test Suites

### 1. Filter Stack (`filter-stack.spec.ts`) — IMPLEMENTED

Tests the Cut page filter stack UI, covering the bugs reported in the
filter system (add/remove sometimes failing, values resetting):

| Suite | Tests | Covers |
|-------|-------|--------|
| Add / Remove | 6 | Add single, add multiple, remove from start/end, remove all, re-add |
| Parameter Persistence | 3 | Values survive adding/removing other filters, default values correct |
| Reorder | 3 | Move up, move down, values preserved after reorder |
| Rapid Operations (regression) | 3 | Rapid double-add, add+remove, slider+remove race |

### 2. Clip Lifecycle (PLANNED)

```
tests/e2e/clip-lifecycle.spec.ts
```

- Create clip → verify appears in clip bank
- Select clip → verify inspector populates
- Edit clip timing → save → verify persistence on reload
- Delete clip → verify removed from bank
- Clip with filters → save → reload → verify filter stack restored

### 3. Compose Editor (PLANNED)

```
tests/e2e/compose-editor.spec.ts
```

- Create composition → add sources → save
- Canvas presets → verify resolution display
- Source picker → search → add
- Export → verify status indicator

### 4. Stitch Editor (PLANNED)

```
tests/e2e/stitch-editor.spec.ts
```

- Create stitch → add videos → reorder
- Format/quality toggle → export → verify status
- Remove videos from stitch

### 5. Video Library (PLANNED)

```
tests/e2e/video-library.spec.ts
```

- Video list renders
- Search/filter
- Navigate to watch page
- Navigate to cut page

## Bug Fixes Applied

The following signal mutation bugs were fixed in `pkg/filters/filter_defs.go`
as part of this testing effort:

### Immutable Signal Updates

**Before (buggy):** All `FilterParam*Expr` functions used shallow array copies
with in-place object mutation:

```js
let s = [...$_filterStack];     // shallow copy — objects are shared refs
s[i].params.key = value;         // MUTATES the original object
$_filterStack = s;               // new array ref, but inner objects aliased
```

**After (fixed):** Immutable update pattern — creates new entry + new params:

```js
let s = [...$_filterStack];
s[i] = {...s[i], params: {...(s[i].params||{}), key: value}};  // new objects
$_filterStack = s;
```

**Affected functions:** `FilterParamRangeExpr`, `FilterParamSelectExpr`,
`FilterParamSetValueExpr`, `FilterParamNumberExpr`, `FilterParamTextExpr`,
`FilterParamColorExpr`, `FilterPresetExpr`

### Root Cause Analysis

| Bug | Cause | Fix |
|-----|-------|-----|
| "Add does nothing" | Possible SSE request deduplication when overlapping @post() calls | Immutable updates prevent stale state from being sent |
| "Remove resets values" | Shallow copy mutation caused aliased params objects; morph re-rendered with stale server-side values | Immutable updates ensure each signal mutation creates fresh objects |
| "Values reset randomly" | Shared object references between old and new signal arrays led to desync | Spread operator on both entry and params creates isolated copies |

## Writing New Tests

1. Create `tests/e2e/your-feature.spec.ts`
2. Import helpers from `./helpers`
3. Use `test.beforeEach` for login + navigation
4. Use Playwright locators for element interaction
5. Wait for SSE morphs with `expect(locator).toHaveCount(n)` or
   `expect(locator).toBeVisible()`
6. Run: `pnpm exec playwright test tests/e2e/your-feature.spec.ts`
