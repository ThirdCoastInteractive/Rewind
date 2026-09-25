# Rewind marketing preview

This command renders the real Rewind templates with deterministic fictional
data for sales pages, docs, and screenshot capture. It does not open Postgres,
call a network service, or use application sessions. The LiveProduct request
context flag is set so the navbar and recordings copy use Rewind Live language.

Build and run it inside the repository's Docker workflow. The command accepts
the same flags when it is the container entrypoint; keep the container port
mapped to 18082 (or pass -addr :8080 for an 8080-based container).

The default listener is http://localhost:18082. Set MARKETING_PREVIEW_ADDR
or pass -addr :8080 when a container expects port 8080. Static assets are
served read-only from static (override with -static-root). On startup the
preview creates a deterministic synthetic MP4 and matching thumbnail,
seek-sprite/VTT, waveform, and caption assets under a local cache directory.
Override that directory with -assets or MARKETING_PREVIEW_ASSETS; it is safe
to delete and regenerate.

Routes useful for captures are /recordings, /editor, /playback,
/stitch, and /stitch/00000000-0000-4000-8000-000000000201. The /editor and
/playback aliases redirect to canonical video routes so the editor and player
initialize exactly as they do in Rewind. An optional local MP4 can be exposed
to the player with -media C:\path\to\demo.mp4 or MARKETING_PREVIEW_MEDIA;
otherwise the generated sample MP4 is served.
