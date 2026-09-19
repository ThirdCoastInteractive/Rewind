# Release audit — 2026-09-05

**Update: the six findings below have fixes and passing targeted checks.** Hold the public release until the remaining broader acceptance work is complete, including the concurrent agent integration. The original findings below are retained as the audit record.

## Fix verification

- R1: the actual v0.0.3 migration SQL now upgrades in an isolated schema, preserves a saved legacy producer scene, and tolerates a repeat upgrade. Only the known historical gaps 37–39 are allowed; migration 39 no longer destroys legacy state. Previously deleted development data cannot be recovered by this change.
- R2: anonymous offline access is denied; an owner can still preview offline; an anonymous live stream closes within the one-second authorization heartbeat after the show goes offline. Scene updates also recheck authorization before sending.
- R3: preceding complete conversational turns are retained within the available budget. The follow-up reproduction passes. Concurrent agent integration changes in this module have been preserved.
- R4: serialized autosave retains edits made during an in-flight write. Navigation flushes writes before requesting replacement content, prevents further page interaction during the transition, and leaves the editor available if saving fails. Native unload prompts while edits remain unsaved.
- R5: successful host changes cancel the room's existing collaboration requests, including pending upgrades. Peers reconnect through current authorization. Tests exercise a real ygo WebSocket and revocation during pending authorization.
- R6: all nine Rewind services render with the same `REWIND_VERSION` pin; tested with `audit-pin`.

Passing checks: `make release-audit` (three Go regression fixtures plus eight JavaScript checks), `make test`, `make integration-test`, `make lint`, and all 40 release guard checks. On this Windows setup the latter ran using `bash -c 'make test-tools'` to keep path handling within one Linux shell. Template/SQL generation completed; a transient shared-file error during asset generation cleared on retry.

The watch page additionally replaces its description and archive accordions with open sections. A live browser check found zero content accordions and no horizontal overflow at desktop and narrow-window sizes. Soft navigation preserved the document shell.

## Scope and evidence

Baseline verified against the public remote: `v0.0.3` and `public/main` resolve to `1f7cf4be0a8dcbcf83d2d22d0910b60c1f0ce23d` (August 29). Reviewed against dev HEAD `2b83ec282e84d1766a0dbce5d741788a0bb85cb0`, including the current modified and untracked implementation. The source inventory is in `release-audit-files.txt` (351 entries). It is an inventory, not a claim that every line has been audited.

Priority coverage: upgrade compatibility, agent context and runtime, collaboration permissions, scene streaming, navigation cleanup and persistence, worker recovery migrations, and container release configuration. Generated files and vendor code were inspected only where needed to trace behavior. No release was published or product fix applied during this audit. Database reproductions used only the disposable integration database on port 15439 with synthetic fixtures.

## Findings

### R1 — P1: v0.0.3 upgrades are rejected before migrations run (reproduced)

Location: `internal/db/database.go:125`; newly added migrations `00037`, `00038`, `00039`.

The released migration tree has versions 1–36 and 40–48. The new tree fills the gaps below the installed version. Goose's ordinary `UpToContext` rejects those missing historical migrations. A database with the released ledger fails with `found 3 missing migrations before current version 48`. A fresh/current development database does not exercise this path.

Reproduction: `go test -tags=releaseaudit ./internal/integration -run TestV003MigrationLedgerUpgrade -count=1 -v`.

The fixture models the exact released ledger in an isolated schema. It proves the preflight rejection; it does **not** install the complete old application schema and is not a full upgrade acceptance test. Once this rejection is repaired, replace/extend this fixture with an actual v0.0.3 schema and representative records.

Required repair: reconcile migration ordering with already installed release ledgers, including installations that already applied 37–39. Validate both upgrade paths on disposable copies before selecting a renumbering or missing-migration strategy. Verify archive records and existing producer state survive; migration 39 drops legacy player sessions and deserves an explicit compatibility decision.

### R2 — P1: offline scene state remains anonymously accessible (reproduced)

Location: `cmd/web/handlers/api/shownote_api/live_stream.go:90`; public route in `cmd/web/internal/web/server.go:499`.

An unauthenticated request with a known show-note UUID receives the scene SSE stream even when the note is offline. The handler loads any note and emits its scene without checking live status or editor access. The UUID is treated as a capability; a previous viewer can retain it. This is not an assertion that UUIDs are guessable. Existing streams also have no live-status/revocation check.

Reproduction: `go test -tags=releaseaudit ./cmd/web/handlers/api/shownote_api -run TestOfflineSceneRejectsAnonymousViewer -count=1 -v`. A synthetic offline note returns HTTP 200 and 750 bytes of SSE without a session.

Required repair: distinguish authenticated private previews from public live viewers, validate the live capability, and terminate public streams when live access ends. Acceptance should cover private preview, anonymous offline access, valid live access, and stopping a live show with a viewer already connected.

### R3 — P1: follow-up agent requests discard preceding conversation (reproduced)

Location: `internal/agent/context.go:13`, especially construction of `base` and iteration after the last user message.

The request handler restores previous messages, but `BoundContext` keeps only the system message, latest user request, and tool exchanges after that request. It drops earlier user/assistant messages even when the entire conversation fits comfortably. A follow-up such as “make that one ten seconds longer” loses the selected clip and the preceding instructions.

Reproduction: `go test -tags=releaseaudit ./internal/agent -run TestFollowupRetainsPriorConversationWhenItFits -count=1 -v`. The four-message fixture returns only the system message and final follow-up with an 8192-token budget.

Required repair: retain complete recent conversational exchanges within budget, preserving tool-call/result pairing. Add checks for small histories, long histories, image budgets, and follow-ups referencing previous artifact IDs. A real-model multi-turn clipping trial remains necessary after the deterministic fix.

### R4 — P1: content navigation can silently cancel Stitch autosave (code traced)

Location: `static/js/stitch-page.js:1099`; `static/js/lib/page-scope.js:15`; `static/js/lib/navigation.js:61`.

Stitch delays its save by 800 ms using `pageTimeout`. Navigation disposes the page scope before replacement, which clears that timer. Editing a project and following an internal link before the debounce expires therefore cancels the pending save. If the PUT already began, `pageFetch` also aborts it, leaving completion uncertain. Cleanup does not flush or await a dirty save.

This affects the recent navigation changes and needs to be addressed in that implementation. Resource cleanup tests currently confirm that timers/requests are disposed; they do not establish that user edits survive.

Required repair: give persistence an explicit lifecycle, flush/await dirty writes before swapping, and keep navigation recoverable on save failure. Acceptance: edit title and segments, navigate immediately through both navbar and agent links, return, and verify persisted values. Also test navigation during a slow or failed save. This finding has not yet been reproduced against a live project to avoid modifying the user's work.

### R5 — P1: removing a host does not revoke an existing collaborative socket (code traced)

Location: `cmd/web/internal/web/server.go:120`; `cmd/web/handlers/api/shownote_api/hosts.go:77`; vendored ygo websocket server authorization at handshake and peer initialization.

Collaboration authorization and `ReadOnly` are set at connection establishment. Removing a host updates the database and roster but does not disconnect that user's existing socket or change its stored permissions. An already connected editor can retain write access until disconnection, even though new connections are denied. Role demotion has the same stale-permission concern.

Required repair: revoke/downgrade active connections on access changes or enforce current permissions before accepting updates. Acceptance requires two independent sessions: connect as editor, remove/demote from the owner session, then attempt a document update on the original socket. This is a source-traced finding; a multi-session runtime reproduction remains outstanding.

### R6 — P2: the release version selector pins only part of the stack (code traced)

Location: `docker-compose.example.yml:3,20,33,60,103,125,151,188,210`.

`REWIND_VERSION` selects postgres-vector, ML, SFU, and vision images. The migrator, web, downloader, ingest, and encoder remain hardcoded to `latest`. Selecting a version can therefore combine a pinned service set with a different application/schema release, defeating reproducible installation and rollback.

Required repair: apply the same release selection to every Rewind image, retaining explicit variant suffixes where needed. Validate the rendered compose configuration and published tags for both CPU and GPU variants.

## Checks run

| Check | Result |
| --- | --- |
| `make test` | Passed |
| `make integration-test` | Passed against disposable integration database |
| `make lint` | Passed; includes public Docker base checks, not a newly built public release commit |
| Node page-scope and SponsorBlock tests | All four passed |
| Three `releaseaudit` checks above | Failed as expected, reproducing R1–R3 |
| `make test-tools` | Could not complete: Bash could not resolve `Y:/rewind-app/scripts/check-release.sh`; expected success got exit 127 |

Audit Go fixtures use the `releaseaudit` build tag, so the ordinary suite does not include these known failing checks. The release-tool failure is an environment/path execution problem, not evidence that its assertions passed or that a public release is valid.

## Remaining acceptance work

1. Resolve R1–R5 and run the specified regressions. Complete a real old-schema upgrade with retained data, then a clean install.
2. Repair/verify version pinning and run release guardrail tests in a compatible shell. Build the actual public squash and run its commit-level release checker before any public push.
3. Exercise assistant SSE disclosure state, scroll behavior, back/forward navigation, and narrow mobile layouts in the browser. Prior visual changes are not certified by this code audit.
4. Exercise worker shutdown/restart, lease expiry, cancellation, model removal and replacement, queued-job configuration snapshots, and cross-service deployment ordering with disposable jobs.
5. Complete a successful real-model, multi-turn archive-to-clips-to-Stitch scenario. Existing rollout notes do not establish that acceptance.
6. Broaden endpoint authorization and dependency review beyond the high-risk paths covered here; run the safe E2E cases against an isolated instance. No blanket security or full-app readiness claim is made by this first pass.
