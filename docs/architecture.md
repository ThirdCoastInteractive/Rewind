# Architecture

Rewind is a self-hosted video archive. Three Compose services share Postgres and a downloads volume. The `rewind` binary runs several roles in one process. `rewind-ml` is the only process that runs Whisper, Ollama, and vision.

Worker counts (`downloads.workers`, `processing.ingest_workers`, `processing.encoder_workers`, `ml.concurrent`) are live admin settings, not Compose replicas.

## System as a whole

```mermaid
flowchart TB
  subgraph clients [Clients]
    Browser[Browser :9115]
    Ext[Browser extension]
    Agent[In-app assistant]
    MCPClient[MCP client]
  end

  subgraph compose [Compose network]
    subgraph rewindProc [rewind container]
      Web[web role :8080]
      DL[download role]
      IN[ingest role]
      ENC[encode role]
      SFU[sfu role :8081 loopback]
    end

    subgraph mlProc [rewind-ml container]
      MLW[ml workers]
      Whisper[whisper-cli]
      Ollama[Ollama :11434]
      Vision[vision uvicorn :3003]
      Textcls[textcls uvicorn :3004]
    end

    PG[(postgres + pgvector)]
  end

  Disk["./bin/download → /downloads/{video-id}/"]
  Spool["./bin/spool → /spool/{job-id}/"]
  Exports["./bin/exports"]

  Browser --> Web
  Ext --> Web
  Agent --> Web
  MCPClient --> Web
  Web --> SFU
  Web --> PG
  DL --> PG
  IN --> PG
  ENC --> PG
  MLW --> PG
  DL --> Spool
  IN --> Spool
  IN --> Disk
  MLW --> Disk
  ENC --> Disk
  ENC --> Exports
  Web --> Disk
  MLW --> Whisper
  MLW --> Ollama
  MLW --> Vision
  MLW --> Textcls
  Web -.->|"VISION_URL"| Vision
```

Postgres is the queue: `download_jobs`, `ingest_jobs`, `ml_jobs`, `clip_exports`, `stitch_jobs`. LISTEN/NOTIFY wakes workers. Disk is not a message bus.

## Rewind process — roles in one binary

`pkg/rewindapp.Run` reads `REWIND_ROLES` (default `web,download,ingest,encode,sfu,migrate`). Each role is a goroutine in the same process.

```mermaid
flowchart LR
  Args[rewind serve] --> Parse[parse REWIND_ROLES]
  Parse --> Migrate{migrate?}
  Migrate -->|yes| Goose[goose on postgres]
  Goose --> Roles
  Migrate -->|no| Roles[start role goroutines]

  Roles --> Web[web]
  Roles --> DL[download]
  Roles --> IN[ingest]
  Roles --> ENC[encode]
  Roles --> SFU[sfu]

  Web --> Plug[plugin.Use / Defaults]
  Plug --> Authn[Authn]
  Plug --> Blob[Blob]
  Plug --> ML[ML]
  Plug --> Live[Live nil on OSS]
```

Compose does not run the leftover `cmd/downloader`, `cmd/ingest`, `cmd/encoder`, or `cmd/sfu` binaries.

## Archive pipeline

```mermaid
sequenceDiagram
  participant U as User / extension
  participant W as web
  participant Q as postgres
  participant D as download role
  participant I as ingest role
  participant M as rewind-ml
  participant Disk as /downloads

  U->>W: POST archive URL
  W->>Q: archival.EnqueueURL → download_jobs
  Q-->>D: NOTIFY download_jobs
  D->>D: yt-dlp into /spool/{job-id}/
  D->>Q: insert ingest_jobs
  Q-->>I: NOTIFY ingest_jobs
  I->>I: probe, faststart, move files
  I->>Q: InsertVideo
  I->>Disk: /downloads/{video-id}/{id}.video.mp4
  I->>Q: plugin.Transcribe → ml_jobs
  I->>Q: EnqueuePostIngestAssetsJob
  Q-->>M: NOTIFY ml_jobs
  M->>Disk: whisper-cli on master
  M->>Q: captions / context_windows
  W->>Disk: RequireVideo then Blob stream
```

Download also runs side loops: comment catchup, metadata refresh, channel watches, catalog crawl, subtitle backfill.

Self-hosted ingest stamps `tenant_id` as the OSS UUID. Private live import (`pkg/archive.ImportMaster`) can stamp a real tenant.

## HTTP video access

Every `/videos/:id` HTTP route loads the actor once (`ActorFrom`), then `RequireVideo`. Lists call `WithActorTenant`.

```mermaid
flowchart TB
  Req[HTTP /videos/:id/...] --> Sess[SessionManager.GetSession]
  Sess -->|no cookie| Unauth[401]
  Sess --> UUID[RequireUUIDParam]
  UUID --> RV[common.RequireVideo]

  RV --> Actor[ActorFrom]
  Actor --> Tenant{Actor.TenantID}
  Tenant -->|empty OSS / staff| ByID[GetVideoByID]
  Tenant -->|valid UUID| ByTen[GetVideoByIDAndTenant]
  Tenant -->|garbage| Dead[404]

  ByID --> Allow[plugin.Guards.Allow]
  ByTen --> Allow
  Allow -->|deny or miss| NF[404]
  Allow -->|ok| Handler[handler body]

  Handler --> Blob[plugin.Blobs Open or PublicURL]
  Blob -->|PublicURL set| Redir[302]
  Blob -->|Open ok| Serve[http.ServeContent]
  Blob -->|miss| Missing[404]
```

Show-note program stream (`/api/show-notes/:id/content/:videoId`) is the exception: unauthenticated, gated by a live note that references the video. Bytes still come from Blob.

OSS delete requires admin or `ArchivedBy` match. `LocalAuthz` allows any logged-in user for `video.write`, so ownership stays in the handler.

Workers and MCP still use unscoped `GetVideoByID`. They are not HTTP actors.

## Plugin slots

Public Rewind fills Authn (cookie), Authz, Disk blob, and LocalML. Live is unset. A private binary calls `plugin.Use` before `rewindapp.Run`.

```mermaid
flowchart TB
  Private[Private main plugin.Use] --> Set[plugin.Set]
  OSS[OSS rewind serve] --> Def[builtin.Defaults + LocalAuth]

  Set --> Authn[Authn]
  Set --> Authz[Authz]
  Set --> Blob[Blob]
  Set --> ML[ML]
  Set --> Live[Live]

  Def --> LocalAuth[cookie session]
  Def --> LocalAuthz[any user, admin role]
  Def --> Disk[Disk DOWNLOADS_DIR]
  Def --> LocalML[writes ml_jobs]
  Def --> NoLive[Live = nil]
```

Blob keys are `{videoID}/{filename}` on OSS. `plugin.Enqueue` is the only ML enqueue door. The ML image does not copy `cmd/web`.

## rewind-ml process

```mermaid
flowchart TB
  Main[cmd/ml main] --> Defaults[builtin.Defaults]
  Main --> Listen[LISTEN ml_jobs]
  Main --> Maint[maintenance loop]
  Main --> Kids[child processes]

  Kids --> WhisperBin[whisper-cli]
  Kids --> OllamaProc[Ollama]
  Kids --> VisionUV[vision :3003]
  Kids --> TextclsUV[textcls :3004]

  Listen --> Workers[mlWorker groups]
  Workers --> Claim[ClaimMLJob]
  Claim --> Kind{kind}
  Kind --> T[transcribe]
  Kind --> V[visual_index]
  Kind --> C[context_windows]
  Kind --> R[refine_boundaries]
  Kind --> Cls[comment_classify / speech_tone]
```

On CUDA/ROCm, GPU kinds share one worker so Whisper and the large context model do not fight the same card. CPU keeps separate groups. Ingest never runs Whisper; it only enqueues `transcribe`.

## Encode / stitch

```mermaid
flowchart LR
  UI[Cut / stitch editor] --> PG[(clip_exports / stitch_jobs)]
  PG -->|NOTIFY clip_exports| EW[encoderWorker]
  PG -->|NOTIFY stitch_jobs| SW[stitchWorker]
  EW --> FF[ffmpeg]
  SW --> FF
  FF --> Out[/exports]
  EW --> Disk[(/downloads)]
  SW --> Disk
```

## Live / SFU / show notes

```mermaid
flowchart TB
  Director[Show live UI] --> Web[web]
  Web --> Scene[scene + producer hubs]
  Web --> Proxy[proxy /signal to 127.0.0.1:8081]
  Proxy --> SFU[Pion SFU]
  SFU --> UDP[UDP 50000-50100]
  Talent[Browser / OBS] --> SFU
  Viewer[Viewer page] --> Web
  Viewer --> Content["content stream via Blob"]
  LivePlug{plugin.Live}
  LivePlug -->|OSS nil| LocalOnly[yt-dlp live archive]
  LivePlug -->|private| CF[CreateInput / outputs]
```

## Other doors

```mermaid
flowchart LR
  Pages[HTML + DataStar SSE] --> RequireVideo
  ExtAPI["/api/extension"] --> Enqueue[archival.EnqueueURL]
  MCP["/mcp bearer token"] --> Tools[search / clip / wiki / OSINT]
  AgentAPI[in-process MCP] --> Tools
  Enqueue --> DJ[download_jobs]
```

MCP lists are not tenant-filtered. The MCP desk is the local archive.
