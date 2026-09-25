// Package runtimecfg supplies validated, versioned operational settings at job boundaries.
package runtimecfg

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/pkg/useragent"
)

// Definition describes a live setting and the service that imports its bootstrap value.
type Definition struct {
	Key     string   `json:"key"`
	Group   string   `json:"group"`
	Label   string   `json:"label"`
	Kind    string   `json:"kind"`
	Default any      `json:"default"`
	Env     string   `json:"env,omitempty"`
	Owner   string   `json:"owner"`
	Min     float64  `json:"min,omitempty"`
	Max     float64  `json:"max,omitempty"`
	Choices []string `json:"choices,omitempty"`
}

// Registry is the complete allowlist of live configuration keys.
var Registry = []Definition{
	{"agent.model", "Agent", "Agent model", "string", "qwen3.5:4b", "AGENT_MODEL", "ml", 0, 0, nil},
	{"agent.max_calls", "Agent", "Tool calls per run", "number", 64, "", "web", 1, 1000, nil},
	{"agent.max_minutes", "Agent", "Minutes per run", "number", 30, "", "web", 1, 240, nil},
	{"agent.context_tokens", "Agent", "Context tokens", "number", 16384, "", "web", 4096, 131072, nil},
	{"agent.temperature", "Agent", "Temperature", "number", 0.2, "", "web", 0, 2, nil},
	{"agent.max_output", "Agent", "Output tokens per step", "number", 2048, "", "web", 256, 8192, nil},
	{"agent.concurrency", "Agent", "Concurrent conversations", "number", 2, "", "web", 1, 8, nil},
	{"ml.concurrent", "Models & ML", "Concurrent jobs per ML kind", "number", 2, "", "ml", 1, 8, nil},
	{"ml.resident", "Models & ML", "Resident Ollama models", "number", 2, "OLLAMA_MAX_LOADED_MODELS", "ml", 1, 8, nil},
	{"ml.keep_alive", "Models & ML", "Idle residency seconds", "number", 300, "", "ml", 0, 3600, nil},
	{"ml.context_model", "Models & ML", "Context window model", "string", "qwen3.8:27b", "CONTEXT_MODEL", "ml", 0, 0, nil},
	{"ml.challengers", "Models & ML", "Context model fallbacks (comma separated)", "string", "gemma4:26b,gemma4:12b,qwen3.5:4b", "CONTEXT_MODEL_CHALLENGERS", "ml", 0, 0, nil},
	{"whisper.model", "Models & ML", "Whisper model", "string", "large-v3-turbo", "WHISPER_MODEL", "ml", 0, 0, []string{"tiny", "base", "small", "medium", "large-v3", "large-v3-turbo"}},
	{"whisper.language", "Models & ML", "Transcription language", "string", "en", "WHISPER_LANGUAGE", "ml", 0, 0, nil},
	{"whisper.task", "Models & ML", "Transcription task", "string", "transcribe", "WHISPER_TASK", "ml", 0, 0, []string{"transcribe", "translate"}},
	{"whisper.device", "Models & ML", "Whisper device", "string", "cpu", "WHISPER_DEVICE", "ml", 0, 0, []string{"cpu", "cuda", "auto"}},
	{"whisper.timeout", "Models & ML", "Transcription timeout seconds (0 unlimited)", "number", 0, "WHISPER_TIMEOUT_SECONDS", "ml", 0, 86400, nil},
	{"vision.device", "Models & ML", "Vision device", "string", "cpu", "VISION_DEVICE", "ml", 0, 0, []string{"cpu", "cuda"}},
	{"vision.clip_model", "Models & ML", "Visual search model", "string", "ViT-B-32__openai", "", "ml", 0, 0, []string{"ViT-B-32__openai"}},
	{"vision.backfill", "Models & ML", "Visual backfill enabled", "boolean", false, "VISUAL_BACKFILL_ENABLED", "ml", 0, 0, nil},
	{"textcls.sentiment_model", "Models & ML", "Comment/speech sentiment model", "string", "twitter-roberta-sentiment", "", "ml", 0, 0, []string{"twitter-roberta-sentiment"}},
	{"textcls.toxicity_model", "Models & ML", "Comment/speech toxicity model", "string", "unbiased-toxic-roberta", "", "ml", 0, 0, []string{"unbiased-toxic-roberta"}},
	{"textcls.device", "Models & ML", "Text classifier device", "string", "cpu", "TEXTCLS_DEVICE", "ml", 0, 0, []string{"cpu", "cuda"}},
	{"diarize.model", "Models & ML", "Diarization model", "string", "off", "DIARIZE_MODEL", "ml", 0, 0, []string{"off", "nemotron-3"}},
	{"diarize.device", "Models & ML", "Diarization device", "string", "cpu", "DIARIZE_DEVICE", "ml", 0, 0, []string{"cpu", "cuda"}},
	{"diarize.backfill", "Models & ML", "Diarization backfill", "boolean", false, "DIARIZE_BACKFILL", "ml", 0, 0, nil},
	{"osint.toxicity_flag", "OSINT", "Toxicity flag threshold", "number", 0.7, "", "ml", 0, 1, nil},
	{"osint.raid_ratio", "OSINT", "Raid burst vs median ratio", "number", 4, "", "ml", 1, 20, nil},
	{"osint.style_min_n", "OSINT", "Min comments before style sock suggest", "number", 8, "", "ml", 3, 100, nil},
	{"downloads.workers", "Downloads", "Download workers per container", "number", 2, "DOWNLOAD_WORKERS", "downloader", 1, 32, nil},
	{"downloads.max_height", "Downloads", "Maximum video height (0 unlimited)", "number", 0, "", "web", 0, 8640, []string{"0", "360", "480", "720", "1080", "1440", "2160"}},
	{"downloads.user_agent", "Downloads", "Download user agent", "string", "", "USER_AGENT", "downloader", 0, 0, nil},
	{"processing.ingest_workers", "Processing & Exports", "Ingest workers per container", "number", 2, "INGEST_WORKERS", "ingest", 1, 32, nil},
	{"processing.asset_workers", "Processing & Exports", "Asset generation workers per container", "number", 2, "ASSET_WORKERS", "ingest", 1, 32, nil},
	{"processing.ffmpeg_threads", "Processing & Exports", "FFmpeg threads for ingest/assets (keeps the host decoder free)", "number", 2, "FFMPEG_THREADS", "ingest", 1, 8, nil},
	{"processing.encoder_workers", "Processing & Exports", "Encoder workers per container", "number", 2, "ENCODER_WORKERS", "encoder", 1, 32, nil},
	{"seek.xfine", "Processing & Exports", "Extra-fine seek previews", "boolean", false, "SEEK_ENABLE_XFINE", "ingest", 0, 0, nil},
	{"seek.xxfine", "Processing & Exports", "Double-fine seek previews", "boolean", false, "SEEK_ENABLE_XXFINE", "ingest", 0, 0, nil},
	{"seek.xxxfine", "Processing & Exports", "Triple-fine seek previews", "boolean", false, "SEEK_ENABLE_XXXFINE", "ingest", 0, 0, nil},
	{"exports.format", "Processing & Exports", "Default export format", "string", "mp4", "", "web", 0, 0, []string{"mp4", "webm"}},
	{"exports.quality", "Processing & Exports", "Default export quality", "string", "high", "", "web", 0, 0, []string{"high", "max"}},
	{"exports.storage_bytes", "Processing & Exports", "Export storage limit bytes (0 unlimited)", "number", 0, "", "web", 0, 1e15, nil},
}

// Snapshot is an immutable settings view carried by a running operation.
type Snapshot map[string]any
type snapshotKey struct{}

var current atomic.Value

// Defaults returns a fresh defaults map without reading deployment secrets.
func Defaults() Snapshot {
	s := Snapshot{}
	for _, d := range Registry {
		s[d.Key] = d.Default
	}
	return s
}

// Current returns a copy of the process's latest applied settings.
func Current() Snapshot {
	s := Defaults()
	if v := current.Load(); v != nil {
		for k, x := range v.(Snapshot) {
			s[k] = x
		}
	}
	return s
}

// WithSnapshot freezes effective configuration for a unit of work.
func WithSnapshot(ctx context.Context, s Snapshot) context.Context {
	copy := Snapshot{}
	for k, v := range s {
		copy[k] = v
	}
	if value, ok := copy["downloads.user_agent"]; ok {
		ctx = useragent.WithValue(ctx, fmt.Sprint(value))
	}
	return context.WithValue(ctx, snapshotKey{}, copy)
}

// Job captures configuration once so retries retain the original operational choices.
func Job(ctx context.Context, dbc *db.DatabaseConnection, kind string, id pgtype.UUID) (context.Context, error) {
	values, err := Read(ctx, dbc)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return nil, err
	}
	raw, err = dbc.Queries(ctx).CaptureJobConfiguration(ctx, &db.CaptureJobConfigurationParams{Kind: kind, JobID: id, Snapshot: raw})
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(raw, &values); err != nil {
		return nil, err
	}
	return WithSnapshot(ctx, values), nil
}

// Value resolves the job snapshot before the process cache.
func Value(ctx context.Context, key string) any {
	if ctx != nil {
		if s, ok := ctx.Value(snapshotKey{}).(Snapshot); ok {
			if v, ok := s[key]; ok {
				return v
			}
		}
	}
	return Current()[key]
}

// String returns a string setting.
func String(ctx context.Context, key string) string { return fmt.Sprint(Value(ctx, key)) }

// Int returns an integral numeric setting.
func Int(ctx context.Context, key string) int {
	f, _ := strconv.ParseFloat(String(ctx, key), 64)
	return int(f)
}

// Env replaces an operational environment read while retaining bootstrap behavior before initialization.
func Env(ctx context.Context, name string) string {
	if current.Load() != nil || (ctx != nil && ctx.Value(snapshotKey{}) != nil) {
		for _, d := range Registry {
			if d.Env == name {
				return String(ctx, d.Key)
			}
		}
	}
	return os.Getenv(name)
}

// Validate rejects unknown keys and invalid or unbounded values.
func Validate(key string, value any) error {
	for _, d := range Registry {
		if d.Key != key {
			continue
		}
		switch d.Kind {
		case "boolean":
			if _, ok := value.(bool); !ok {
				return fmt.Errorf("%s must be boolean", key)
			}
		case "number":
			switch value.(type) {
			case float64, float32, int, int32, int64, json.Number:
			default:
				return fmt.Errorf("%s must be numeric", key)
			}
			f, err := strconv.ParseFloat(fmt.Sprint(value), 64)
			if err != nil || math.IsNaN(f) || math.IsInf(f, 0) || f < d.Min || f > d.Max {
				return fmt.Errorf("%s must be between %g and %g", key, d.Min, d.Max)
			}
			if key != "agent.temperature" && key != "osint.toxicity_flag" && f != math.Trunc(f) {
				return fmt.Errorf("%s must be an integer", key)
			}
		case "string":
			s, ok := value.(string)
			if !ok || len(s) > 1024 || strings.ContainsAny(s, "\r\n\x00") {
				return fmt.Errorf("invalid %s", key)
			}
			if len(d.Choices) > 0 {
				found := false
				for _, v := range d.Choices {
					found = found || v == s
				}
				if !found {
					return fmt.Errorf("unsupported %s", key)
				}
			}
			if strings.HasSuffix(key, "model") && strings.TrimSpace(s) == "" {
				return fmt.Errorf("model is required")
			}
		}
		return nil
	}
	return fmt.Errorf("unknown live setting %s", key)
}

// Read returns authoritative saved values over registry defaults.
func Read(ctx context.Context, dbc *db.DatabaseConnection) (Snapshot, error) {
	rows, e := dbc.Queries(ctx).ListRuntimeSettings(ctx)
	if e != nil {
		return nil, e
	}
	s := Defaults()
	for _, r := range rows {
		if _, known := s[r.Key]; !known {
			continue
		}
		var v any
		if e = json.Unmarshal(r.Value, &v); e != nil {
			return nil, e
		}
		if e = Validate(r.Key, v); e != nil {
			return nil, e
		}
		s[r.Key] = v
	}
	return s, nil
}

// Save validates an atomic settings patch and maintains legacy instance settings consumers.
func Save(ctx context.Context, dbc *db.DatabaseConnection, user pgtype.UUID, values Snapshot) error {
	if err := RequireAdmin(ctx, dbc, user); err != nil {
		return err
	}
	for k, v := range values {
		if e := Validate(k, v); e != nil {
			return e
		}
	}
	q, tx, e := dbc.NewWithTX(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	for k, v := range values {
		raw, _ := json.Marshal(v)
		if _, e = q.SaveRuntimeSetting(ctx, &db.SaveRuntimeSettingParams{Key: k, Value: raw, UpdatedBy: user}); e != nil {
			return e
		}
		switch k {
		case "downloads.max_height":
			n, _ := strconv.Atoi(fmt.Sprint(v))
			e = q.UpsertMaxDownloadHeight(ctx, int32(n))
		case "exports.storage_bytes":
			f, _ := strconv.ParseFloat(fmt.Sprint(v), 64)
			e = q.UpsertClipExportStorageLimit(ctx, int64(f))
		}
		if e != nil {
			return e
		}
	}
	return tx.Commit(ctx)
}

// RequireAdmin checks current account status for each operational mutation.
func RequireAdmin(ctx context.Context, dbc *db.DatabaseConnection, id pgtype.UUID) error {
	user, err := dbc.Queries(ctx).SelectUserByID(ctx, id)
	if err != nil || !user.Enabled || user.Role != "admin" {
		return fmt.Errorf("administrator access required")
	}
	return nil
}

// Start imports owning-service defaults once and refreshes by notification plus reconciliation.
func Start(ctx context.Context, dbc *db.DatabaseConnection, service string) error {
	q := dbc.Queries(ctx)
	existing, err := q.ListRuntimeSettings(ctx)
	if err != nil {
		return err
	}
	saved := map[string]bool{}
	for _, row := range existing {
		saved[row.Key] = true
	}
	for _, d := range Registry {
		if d.Owner != service || saved[d.Key] {
			continue
		}
		v := d.Default
		if raw, ok := os.LookupEnv(d.Env); ok && raw != "" {
			var parseErr error
			switch d.Kind {
			case "boolean":
				v, parseErr = strconv.ParseBool(raw)
			case "number":
				v, parseErr = strconv.ParseFloat(raw, 64)
			default:
				v = raw
			}
			if parseErr != nil {
				return fmt.Errorf("invalid bootstrap value for %s", d.Env)
			}
		}
		if service == "web" {
			if legacy, e := q.GetInstanceSettings(ctx); e == nil {
				switch d.Key {
				case "downloads.max_height":
					v = legacy.MaxDownloadHeight
				case "exports.storage_bytes":
					v = legacy.ClipExportStorageLimitBytes
				}
			}
		}
		if e := Validate(d.Key, v); e != nil {
			return e
		}
		raw, _ := json.Marshal(v)
		if e := q.SeedRuntimeSetting(ctx, &db.SeedRuntimeSettingParams{Key: d.Key, Value: raw}); e != nil {
			return e
		}
	}
	host, _ := os.Hostname()
	var appliedHash string
	reload := func() error {
		s, e := Read(ctx, dbc)
		if e != nil {
			return e
		}
		h := snapshotHash(s)
		if h == "" || h != appliedHash {
			current.Store(s)
			useragent.Configure(fmt.Sprint(s["downloads.user_agent"]))
			appliedHash = h
		}
		// Ack is the consumer liveness heartbeat; skip re-applying unchanged snapshots.
		return q.AckRuntimeSettings(ctx, &db.AckRuntimeSettingsParams{
			Service: service, Snapshot: []byte(h), Hostname: host,
		})
	}
	if e := reload(); e != nil {
		return e
	}
	wake := make(chan struct{}, 1)
	wakeReload := func() {
		select {
		case wake <- struct{}{}:
		default:
		}
	}
	go db.RunListenLoop(ctx, dbc, []string{"runtime_settings"}, func(*pgconn.Notification) {
		wakeReload()
	}, wakeReload)
	go func() {
		tick := time.NewTicker(30 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				stopCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				if e := dbc.Queries(stopCtx).StopRuntimeSettings(stopCtx, service); e != nil {
					slog.Warn("settings shutdown mark failed", "service", service, "error", e)
				}
				return
			case <-tick.C:
			case <-wake:
			}
			if e := reload(); e != nil {
				slog.Warn("settings refresh failed", "service", service, "error", e)
			}
		}
	}()
	return nil
}

// snapshotHash fingerprints a snapshot as canonical JSON (encoding/json sorts map keys).
func snapshotHash(s Snapshot) string {
	raw, err := json.Marshal(s)
	if err != nil {
		return ""
	}
	return string(raw)
}

// WaitWorker parks surplus worker slots between jobs without cancelling active work.
func WaitWorker(ctx context.Context, key string, index int) bool {
	for index >= Int(ctx, key) {
		select {
		case <-ctx.Done():
			return false
		case <-time.After(time.Second):
		}
	}
	return ctx.Err() == nil
}
