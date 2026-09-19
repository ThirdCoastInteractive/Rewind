# Wiki

The wiki is a local vault over your archive. Pages nest, wikilinks connect them, and topic pages can show playable windows from generated context — not as cuts, as citations.

Open **Wiki** (`/wiki`) to search and browse trees.

## Trees

| Tree | What it is for |
| ---- | -------------- |
| `creator` | A person |
| `channel` | A platform account |
| `clipping` | How you cut a show: slots, title voice, dated airings |
| `topic` | A durable subject (`[[topic/flock-alpr]]`), not a procedural label like "intro" |

URLs are `/wiki/{tree}/{slug}`. Nested pages use slashes in the slug (`/wiki/clipping/ben-avery/2026-04-16`).

## Wikilinks

Write `[[tree/slug]]` in a page body. Links populate backlinks and can appear on the [network graph](library.md) as vault edges.

Topic pages may attach a creator or channel. Harvested context-window topic binds are separate from wiki prose: the wiki is memory, the windows are evidence.

## Archive windows

On a topic page, Rewind can list playable context windows bound to that topic across channels. Use those timestamps to *find* a moment. Do not copy chapter bounds onto a clip; prefer a nested short or a Cut/Stitch range you actually reviewed.

Agents should call `list_topic_windows` (see [MCP](mcp.md)) rather than treating wiki timestamps as cuts.
