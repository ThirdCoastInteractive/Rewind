# MCP — connect an agent to Rewind

Rewind serves [Model Context Protocol](https://modelcontextprotocol.io) at **`/mcp`** (Streamable HTTP) so you can point Claude Desktop, Cursor, or similar at your archive.

## Token

1. Sign in and open **Settings**.
2. Under **MCP**, label a token (e.g. “Claude Desktop”) and click **Create token**.
3. Copy the `rw_…` secret. It is shown once.

## Client config

```json
{
  "mcpServers": {
    "rewind": {
      "url": "http://localhost:9115/mcp",
      "headers": {
        "Authorization": "Bearer rw_YOUR_SECRET"
      }
    }
  }
}
```

Use your real Rewind origin instead of `localhost:9115` if you expose it elsewhere.

## Tools (read)

- `search_library` — title, uploader, tags, comments, transcripts
- `search_transcripts` — timestamped cue hits
- `get_transcript` — cleaned text / cues, optional time range
- `get_video` — metadata
- `library_stats`
- `resolve_uri` — `rewind://video/{id}`

Write tools (download, follow, link creators) require a token with `mcp:write`. Current tokens are minted with both `mcp:read` and `mcp:write`.
