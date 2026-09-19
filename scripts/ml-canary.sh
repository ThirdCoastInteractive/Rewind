#!/bin/sh
# Isolated model diagnostic: no database connection, downloads, or archive writes.
set -eu
export OLLAMA_HOST=127.0.0.1:11434
export OLLAMA_DEBUG=1
export OLLAMA_KEEP_ALIVE=0
ollama serve &
server_pid=$!
trap 'kill "$server_pid" 2>/dev/null || true' EXIT INT TERM
i=0
until curl -fsS http://127.0.0.1:11434/api/tags >/dev/null; do
  i=$((i+1)); [ "$i" -lt 20 ] || exit 1; sleep 1
done
printf '{"model":"%s","messages":[{"role":"user","content":"Return only {\"ok\":true}"}],"stream":false,"format":"json","think":false,"keep_alive":0,"options":{"num_ctx":%s,"num_predict":32,"use_mmap":false}}' "${CONTEXT_MODEL:-qwen3.8:27b}" "${CANARY_CONTEXT:-2048}" >/tmp/rewind-canary-request.json
curl --max-time 180 -fSs http://127.0.0.1:11434/api/chat -H 'Content-Type: application/json' --data-binary @/tmp/rewind-canary-request.json
