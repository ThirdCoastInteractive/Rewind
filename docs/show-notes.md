# Show notes

Show notes are collaborative Markdown rundowns for a live show. The document is the source of truth; the directing layout and public program output follow it.

Open **Show notes** (`/show-notes`) to create or open a note.

## Rundown

Headings are sections. A supported media link at the start of a list item or paragraph enters the rundown in document order. An indented paragraph is show-specific context for that reference.

```md
# Cold open

1. [Saved opener](rewind://clip/CLIP_ID) @ 0:18–0:42
   Let it breathe before talking.

2. [Source](https://youtu.be/abc123) @ 12:34–13:10
   Start after the pause.

> Break — sponsor read
```

`M:SS` and `H:MM:SS` points create markers; ranges create clips after you confirm. Pasting a URL only previews it until you accept archival or an agent suggestion.

## Layouts

`/show-notes/{id}` has Planning, Recording, and Directing layouts. Individual panels (notes, conversation, call, program, controls) also have `/show-notes/{id}/panel/{panel}` URLs.

`/show-notes/{id}/live` is the directing layout. `/show/{code}` is the public program output — the page you put in an OBS browser source.

## Live output

Going live publishes the current scene to viewers who have the public code. Take the show offline to revoke that stream. Authenticated owners can still preview.

WebRTC (camera, mic, program video) runs in the `rewind` container. STUN/TURN settings are in `.env` if you need to traverse NAT. See [Configuration](configuration.md).

The more detailed workspace contract (migration, Yjs, panels) is in [show-workspace.md](show-workspace.md).
