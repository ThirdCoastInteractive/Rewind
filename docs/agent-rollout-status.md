# Agent and live-settings rollout status

Updated 2026-09-05. This is an implementation checkpoint, not a declaration that the full agent proposal has passed acceptance.

## Implemented in the working tree

- Database-backed live operational settings, user preferences, model operations, and service reconciliation.
- Private sidecar inference/model API and model usage leases.
- Local coordinator, owned conversations, persisted events and operation journal, cancellation, and continuation.
- Shared MCP registry and local sessions using the initiating user.
- Continued runs restore completed tool results missing from the last checkpoint, preserve the model digest, and reject uncertain outcomes.
- Settings section navigation, grouped controls, switches, visible model-operation buttons, and a compact assistant launcher.
- Per-account Mono, Federal, Amber Terminal, and Ultraviolet palettes, each with Light/Dark/System selection. Mono/System defaults. Square UI chrome. Changes apply through DataStar and persist across navigation.

The coordinated service rebuild completed on September 5. A concurrent deployment interrupted container replacement; starting the built images reconciled the stack successfully. PostgreSQL is healthy, the migrator exited successfully, and application services are running.

## Evidence

Go tests and disposable PostgreSQL integration tests passed, including preference merging, live configuration propagation, ownership, runtime-aware claims, and MCP clipping workflows. Browser checks covered eight explicit palette/mode combinations, mobile overflow, correct control values, assistant expansion, live theme switching, reload persistence, and Jobs styling.

Two actual Ollama Qwen 3.5 4B compilation trials failed to produce the required project within seven minutes on CPU. The first exposed a tool schema incompatibility that was fixed; the second repeated invalid creator IDs. This model has not passed compilation acceptance.

An LM Studio comparison transport was added to the real-model fixture. Qwen 27B was loaded for comparison; compatibility testing caught missing properties in zero-argument tool schemas. After that fix, the trial ended with an incomplete stream. The user reported GPU pressure, so the comparison model was explicitly unloaded. No successful 27B workflow result is claimed.

## Remaining release gates

- Grok ACP and Codex App Server protocol clients have tested wire contracts and passed installed CLI handshakes (Grok 1.0.13, Codex 0.153.1). Built-in provider selection, isolated execution, credential handling, and durable external-run recovery remain incomplete.
- Authenticated A2A SendMessage/GetTask/CancelTask endpoints now provide owned, deduplicated local tasks. Agent-card discovery, streaming, interactive input, and full conformance remain incomplete.
- Broader mutation reconciliation, model capacity/removal race coverage, adapter management tests, and live application reporting.
- Real-model archive acceptance for ambiguity, pagination, context selection, project assembly, rendering, admin actions, and unauthorized requests.
- Verify the complete operational-setting matrix across owning services without subsequent rebuilds; the coordinated deployment itself is complete.


## September 5 follow-up

- Removed face identification, grouping, matching, People UI/routes, face MCP tools, face settings, the Buffalo model preset, and InsightFace inference/dependency. Visual CLIP search and explicit scene indexing remain.
- Migration 66 retires unfinished face jobs and rejects attempts to create or restart them. Historical observations, completed jobs, model weights, and archived media are retained.
- Added regression checks for retired routes, rejected inference tasks/models, rejected job insertion/restart, and preservation of completed historical jobs.
- Coordinator context now retains complete tool exchanges and usable vision payloads, journals and events are fenced to current leases, and disabled MCP users/foreign watch deletion are rejected.
- Capacity admission tracks pending model loads and uses live concurrency limits; service settings show application status.
- A third real Qwen 3.5 4B CPU trial reached the 12-minute checkpoint limit after six calls. It resolved the fixture creator and found passages, then twice supplied string timestamps to a numeric transcript schema. No project artifact was created. This preset still fails compilation acceptance; API and handshake tests do not establish model reliability.

Deployment verification: migration 66 applied, zero unfinished face jobs, retirement trigger installed, removed HTTP routes return 404, and the running vision service advertises only ready ViT-B-32__openai. The authenticated Visual search page renders scene search and indexing controls with the assistant collapsed. Go, disposable PostgreSQL integration, vision-service tests (4), and browser asset generation passed.
