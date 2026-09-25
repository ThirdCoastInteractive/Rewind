# Release verification — 2026-09-20

Status: release regression gates passed and integrated local stack deployed. Public release not published; remaining acceptance limits are listed below.

## Scope and data protection

The initial development baseline was `137fb176`; the locally available public
release was `ed061ef3` (2026-09-19). Their trees differed in 107 files. Public
history is squashed, so comparisons use trees rather than a merge base. Existing
and concurrent UI/player/preview changes were preserved and included in checks.

Database tests used the disposable integration PostgreSQL service on port 15439.
Browser mutation tests used a separate synthetic database and generated video on
port 19115. No archive videos, downloaded media, or video records were deleted.
The live archive contained 3,534 videos at migration version 92 before deployment.

## Findings addressed

- Restored checked-in Go vendor consistency; ordinary checkout tests now run.
- Fixed imported blob media resolution, nonexistent master-format selection, and
  legacy absolute local media paths, with focused regression tests.
- Closed the settings LISTEN startup/reconnect notification race.
- Added historical v0.1.0 upgrade coverage for preserved media references, tenant
  defaults, cross-tenant sources, exact uniqueness rejection, and repeat migration.
- Corrected stale integration fixtures to explicitly test authenticated shared
  reads, owner-only writes, and demand-only ML scheduling. Clipping assertions
  retain video/order/bounds checks with one-microsecond serialization tolerance.
- Added concurrent indexes and direct UNION candidate selection for network commenter relationship lookups after
  detecting repeated scans against approximately 1.27 million commenters.
- Repaired the Python 3.10 vision dependency lock, GPU-image alignment inclusion,
  text-classifier rebuild detection, and ML canary runtime environment.
- Fixed faststart remux format selection, preserved all media streams and source permissions, and tested source preservation on failure using synthetic FFmpeg media.
- Updated vulnerable Go networking and JavaScript dependencies; preserved card hover behavior under patched Tailwind dependencies.
- Added portable comprehensive JS tests, authenticated navigation and network
  browser checks, explicit synthetic-video opt-in for mutating player tests,
  and release CI preflight. The release gate now includes integration/MCP tests.

## Verification results

| Check | Result |
| --- | --- |
| `make release-gates` | Passed in ordinary vendor mode: Go packages, 15 Stitch JS tests, 28 release guard tests, lint, 24 general JS tests, historical upgrades, integration and MCP |
| `make integration-test` | 72 top-level tests passed across both suites, plus new network candidate semantic regression |
| `go vet ./...` | Passed |
| Linux `go test -race` | Eight packages passed: plugin, builtins, auth, common handlers, events, runtime settings, ingest, Stitch |
| `make test-stitch-render` | Passed synthetic FFmpeg rendering and caption/transition checks |
| Wiki integration tests | Passed |
| Alignment Python tests | Three passed |
| Text classifier API contract tests | Four passed |
| Vision Python 3.10 lock install and contract tests | Actual clean install succeeded; final five tests passed, including restricted-umask installation permissions |
| Installed Qwen 27B inference canary | Passed on CUDA with 32,768-token context; returned expected JSON; approximately 154 seconds including cold load |
| Go vulnerability scan | No reachable vulnerabilities; scanner also listed uncalled package/module advisories |
| Frozen-install full JavaScript audit | Zero advisories, including development dependencies |
| Rebuilt browser suite | 24/24 passed against synthetic application, including network readiness |
| Installed CLIP CPU and CUDA canaries | Both passed image/text embeddings and repeatability; live CUDA run 16.7 seconds |
| Installed Whisper large-v3-turbo | Installed at user request (1,624,555,275 bytes); CUDA inference and generated spoken-sentence transcription passed |
| Synthetic media range request | HTTP 206, exactly 1,024 requested bytes |
| Final live network page | HTTP 200 with graph data; 1.94-second server latency / 2.00-second request, versus 12–16 seconds before query rewrite |
| Final deployment | Web and CUDA ML containers rebuilt and running; PostgreSQL healthy; migration 93; 3,534 archive videos retained; no faststart failures observed after final restart |
| Initial live HTTP smoke | Health, home, login, registration 200; library redirects unauthenticated users to login |

Windows selected WSL's `bash` initially, causing a tooling timeout. The full
release gate passed after placing Git for Windows `bin` and `usr/bin` first in
PATH. Native Windows race testing encountered a linker failure; the equivalent
Linux race run passed. Incomplete attempts are not counted as passing results.

## Remaining release boundary

Public squash, tag, image publication, and push have not been performed. The
working tree still contains the user's concurrent UI/player/preview changes and
rebuilt assets; those changes were tested locally but remain unstaged for their
owning task. Freeze and commit the intended release tree, then run release gates
and the public-tree check before publishing.

The optional text-classification models are absent, so sentiment/toxicity jobs
remain in `waiting_model`; a separate installation choice was presented to the
user. The alignment Python runtime is still completing its first installation
on the persistent volume; its three unit tests passed, but live model alignment
has not been accepted. External-platform downloads, full real-model agent
workflows, ROCm, and browsers other than Chromium were not exhaustively exercised.

The vision model files' ownership was repaired for the unprivileged worker,
and future installations now publish readable files and manifests. The CUDA
image now includes cuDNN 9, verified by actual GPU embedding inference. Whisper
large-v3-turbo was installed with the user's explicit authorization and verified
on CUDA; existing model selections were preserved.

The synthetic browser server and disposable integration database were stopped
after testing. Diagnostic logs and synthetic media remain under the ignored
`.worktrees/` directory. The normal archive stack remains running.
