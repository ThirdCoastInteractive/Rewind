# Collaborative show workspace

The show-note workspace makes a Markdown document the production source of
truth. It is the default interface; application startup requires every legacy
note to pass migration preflight.

## Rollout

```sh
make show-notes-migrate
make show-notes-preflight
```

The targets run inside the migrator container, so they use the same database
configuration and network as the application. Legacy block rows remain
read-only for recovery. The migration and preflight are idempotent and never
delete video or download content.

## Markdown rundown

```md
# Cold open

1. [Saved opener](rewind://clip/CLIP_ID) @ 0:18–0:42
   Let it breathe before talking.

2. [Source](https://youtu.be/abc123) @ 12:34–13:10
   Start after the pause.

> Break — sponsor read
```

Headings define sections. A supported media link at the start of a list item or
paragraph enters the rundown in document order. An indented paragraph is that
reference's show-specific context. `M:SS` and `H:MM:SS` points create markers;
ranges create clips after explicit confirmation. Bare or external URLs only
receive previews while being typed—archival and object creation begin only from
an inline confirmation or acceptance of an agent suggestion.

## Surfaces

`/show-notes/{id}` contains Planning, Recording, and Directing layouts. Notes,
Conversation/Review, Call, Program, and Controls also have standalone
`/show-notes/{id}/panel/{panel}` URLs. `/show-notes/{id}/live` redirects to the
Directing layout; `/show/{code}` remains the public program/OBS output.

The document WebSocket carries Yjs content and awareness. Database-derived
room, review, reference, download, and production events keep their monotonic
cursor and remain available through ordinary HTTP event waits.
