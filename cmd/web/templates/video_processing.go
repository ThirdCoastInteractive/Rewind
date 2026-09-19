package templates

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"thirdcoast.systems/rewind/internal/db"
)

type mlCopy struct {
	Headline string
	Detail   string
}

func mlQueueSummary(counts []*db.CountMLJobsRow) string {
	if len(counts) == 0 {
		return "No ML jobs."
	}
	var queued, processing, cancelled, waiting, succeeded, failed int64
	for _, row := range counts {
		if row == nil {
			continue
		}
		switch row.Status {
		case "queued", "retry_wait":
			queued += row.N
		case "processing":
			processing += row.N
		case "cancelled", "paused":
			cancelled += row.N
		case "waiting_model", "waiting_assets":
			waiting += row.N
		case "succeeded":
			succeeded += row.N
		case "failed":
			failed += row.N
		}
	}
	return fmt.Sprintf("%d queued · %d running · %d waiting · %d succeeded · %d failed · %d cancelled", queued, processing, waiting, succeeded, failed, cancelled)
}

func mlCountByKind(counts []*db.CountMLJobsRow, kind string) int64 {
	var n int64
	for _, row := range counts {
		if row == nil || (kind != "" && kind != "all" && row.Kind != kind) {
			continue
		}
		switch row.Status {
		case "queued", "retry_wait", "processing", "waiting_model", "waiting_assets":
			n += row.N
		}
	}
	return n
}

func mlCountByStatus(counts []*db.CountMLJobsRow, status string) int64 {
	var n int64
	for _, row := range counts {
		if row == nil {
			continue
		}
		if status == "all" || status == "" {
			n += row.N
			continue
		}
		switch status {
		case "queued":
			if row.Status == "queued" || row.Status == "retry_wait" {
				n += row.N
			}
		case "processing":
			if row.Status == "processing" || row.Status == "waiting_model" || row.Status == "waiting_assets" {
				n += row.N
			}
		default:
			if row.Status == status {
				n += row.N
			}
		}
	}
	return n
}

func generatedDataSummary(jobs []*db.MlJob, health []*db.MlRuntimeHealth, hasTranscript bool, windows int) string {
	win := fmt.Sprintf("%d context windows", windows)
	transcribe := latestMLJob(jobs, "transcribe")
	contextJob := latestMLJob(jobs, "context_windows")
	tHealth := mlHealth(health, "transcribe")
	cHealth := mlHealth(health, "context_windows")
	if !hasTranscript {
		if transcribe != nil && transcribe.Status == "processing" {
			return "Captions are running. Context windows wait on the transcript · " + win
		}
		if mlKindBlocked(transcribe, tHealth) {
			return "No transcript yet. Captions are blocked on the transcription runtime · " + win
		}
		if transcribe != nil && transcribe.Status == "queued" {
			return "Captions are queued. Context windows wait on the transcript · " + win
		}
		return "No transcript yet. Generate captions first · " + win
	}
	if contextJob != nil && contextJob.Status == "processing" {
		return "Transcript available. Context generation is running · " + win
	}
	if mlKindBlocked(contextJob, cHealth) {
		return "Transcript available. Context windows are blocked on the local model · " + win
	}
	if contextJob != nil && contextJob.Status == "queued" {
		return "Transcript available. Context generation is queued · " + win
	}
	return "Transcript available · " + win
}

func relevantMLHealth(jobs []*db.MlJob, health []*db.MlRuntimeHealth, hasTranscript bool) []*db.MlRuntimeHealth {
	want := map[string]bool{}
	if !hasTranscript {
		want["transcribe"] = true
	}
	for _, j := range jobs {
		if j == nil {
			continue
		}
		want[j.Kind] = true
	}
	var out []*db.MlRuntimeHealth
	for _, h := range health {
		if h == nil || strings.TrimSpace(h.LastError) == "" || !want[h.Kind] {
			continue
		}
		out = append(out, h)
	}
	return out
}

func latestMLJob(jobs []*db.MlJob, kind string) *db.MlJob {
	for _, j := range jobs {
		if j != nil && j.Kind == kind {
			return j
		}
	}
	return nil
}

func mlHealth(health []*db.MlRuntimeHealth, kind string) *db.MlRuntimeHealth {
	for _, h := range health {
		if h != nil && h.Kind == kind {
			return h
		}
	}
	return nil
}

func mlKindBlocked(job *db.MlJob, health *db.MlRuntimeHealth) bool {
	if job == nil {
		return false
	}
	switch job.Status {
	case "succeeded", "superseded", "processing":
		return false
	}
	if mlJobLooksUnhealthy(job.Status, job.Attempts, job.LastError) {
		return true
	}
	return job.Status == "queued" && healthHasError(health)
}

func mlJobLooksUnhealthy(status string, attempts int32, lastError string) bool {
	switch status {
	case "waiting_model", "waiting_assets", "retry_wait", "failed", "paused", "cancelled":
		return true
	case "queued":
		return attempts > 1 || strings.TrimSpace(lastError) != ""
	default:
		return false
	}
}

func healthHasError(h *db.MlRuntimeHealth) bool {
	return h != nil && strings.TrimSpace(h.LastError) != ""
}

func mlKindLabel(kind string) string {
	switch kind {
	case "transcribe":
		return "Captions"
	case "context_windows":
		return "Context"
	case "visual_index":
		return "Visual index"
	case "comment_classify":
		return "Comment tone"
	case "speech_tone":
		return "Speech tone"
	case "refine_boundaries":
		return "Boundaries"
	default:
		if kind == "" {
			return "ML job"
		}
		return kind
	}
}

func mlJobStatusLabel(status string, attempts int32, lastError string) string {
	switch status {
	case "succeeded":
		return "succeeded"
	case "processing":
		return "running"
	case "failed":
		return "failed"
	case "paused":
		return "paused"
	case "cancelled":
		return "cancelled"
	case "superseded":
		return "superseded"
	case "waiting_model":
		return "waiting for model"
	case "waiting_assets":
		return "waiting for video file"
	case "retry_wait":
		return "waiting to retry"
	case "queued":
		if mlJobLooksUnhealthy(status, attempts, lastError) {
			return "waiting after retries"
		}
		return "queued"
	default:
		if status == "" {
			return "unknown"
		}
		return status
	}
}

func mlJobHeadline(kind, status string, attempts int32, lastError string) string {
	return mlKindLabel(kind) + " · " + mlJobStatusLabel(status, attempts, lastError)
}

func mlJobProgress(raw []byte) string {
	if len(raw) == 0 || string(raw) == "{}" || string(raw) == "null" {
		return ""
	}
	var p struct {
		Message string `json:"message"`
		Chunk   int    `json:"chunk"`
		Chunks  int    `json:"chunks"`
	}
	if json.Unmarshal(raw, &p) != nil {
		return ""
	}
	if strings.TrimSpace(p.Message) != "" {
		return p.Message
	}
	if p.Chunks > 0 {
		return fmt.Sprintf("Chunk %d/%d", p.Chunk, p.Chunks)
	}
	return ""
}

func mlJobAttemptLine(attempts, priority int32, status, lastError string) string {
	if status == "queued" && mlJobLooksUnhealthy(status, attempts, lastError) {
		return fmt.Sprintf("Claimed %d times · priority %d", attempts, priority)
	}
	return fmt.Sprintf("Attempts: %d · Priority: %d", attempts, priority)
}

func mlJobRetryable(status string) bool {
	switch status {
	case "failed", "retry_wait", "waiting_model", "waiting_assets", "paused", "cancelled":
		return true
	default:
		return false
	}
}

func mlJobNotice(kind, status string, attempts int32, lastError string) mlCopy {
	copy := mlErrorCopy(lastError)
	if copy.Headline != "" {
		return copy
	}
	if status == "queued" && attempts > 1 {
		return mlCopy{Headline: "This is not a healthy queue. The worker keeps reclaiming the job, usually because the runtime is unavailable."}
	}
	if status == "waiting_model" {
		if kind == "transcribe" {
			return mlCopy{Headline: "Waiting for the Whisper model or transcription runtime."}
		}
		return mlCopy{Headline: "Waiting for the local context model."}
	}
	if status == "waiting_assets" {
		return mlCopy{Headline: "Waiting for the video file to finish downloading."}
	}
	return mlCopy{}
}

func mlRuntimeHeadline(kind string) string {
	switch kind {
	case "transcribe":
		return "Transcription runtime"
	case "context_windows":
		return "Context model runtime"
	default:
		return mlKindLabel(kind) + " runtime"
	}
}

func mlHealthCooldown(h *db.MlRuntimeHealth) string {
	if h == nil || !h.RetryAt.Valid || !h.RetryAt.Time.After(time.Now()) {
		return ""
	}
	return "Cooldown until " + h.RetryAt.Time.UTC().Format("Jan 2 15:04:05 UTC")
}

func mlErrorCopy(raw string) mlCopy {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return mlCopy{}
	}
	extracted := extractJSONError(raw)
	stripped := collapseSpace(stripJSONBlob(raw))
	haystack := strings.ToLower(raw + " " + extracted + " " + stripped)
	detail := extracted
	if detail == "" {
		detail = stripped
	}
	var headline string
	switch {
	case strings.Contains(haystack, "libcuda") || strings.Contains(haystack, "cuda_error") || strings.Contains(haystack, "needs a cuda"):
		headline = "Whisper needs a CUDA runtime that isn't available."
	case strings.Contains(haystack, "llama-server") || strings.Contains(haystack, "llama runner") || strings.Contains(haystack, "out of memory") || strings.Contains(haystack, "cuda oom"):
		headline = "The context model crashed, often from running out of GPU memory."
	case strings.Contains(haystack, "waiting_capacity"):
		headline = "Waiting for GPU capacity."
	case strings.Contains(haystack, "not installed") || strings.Contains(haystack, "waiting_model"):
		if strings.Contains(haystack, "whisper") {
			headline = "The Whisper model is not installed."
		} else if strings.Contains(haystack, "ollama") || strings.Contains(haystack, "tag") {
			headline = "The context model is not installed."
		} else {
			headline = "A required local model is not installed."
		}
	case strings.Contains(haystack, "waiting_assets"):
		headline = "Waiting for the video file."
	case strings.Contains(haystack, "ollama unreachable") || strings.Contains(haystack, "inference runtime unavailable"):
		headline = "The local inference runtime is unavailable."
	default:
		headline = firstSentence(stripped, 160)
		if headline == "" {
			headline = firstSentence(extracted, 160)
		}
		if headline == "" {
			headline = "The job reported an error."
		}
	}
	detail = firstSentence(detail, 240)
	if strings.EqualFold(strings.TrimRight(detail, "."), strings.TrimRight(headline, ".")) {
		detail = ""
	}
	if looksLikeJSON(detail) {
		if extracted != "" && !looksLikeJSON(extracted) {
			detail = firstSentence(extracted, 240)
		} else {
			detail = ""
		}
	}
	return mlCopy{Headline: headline, Detail: detail}
}

func extractJSONError(raw string) string {
	for i := strings.Index(raw, "{"); i >= 0; {
		var payload map[string]any
		dec := json.NewDecoder(strings.NewReader(raw[i:]))
		if dec.Decode(&payload) == nil {
			for _, key := range []string{"error", "message", "msg"} {
				s, _ := payload[key].(string)
				if strings.TrimSpace(s) != "" {
					return strings.TrimSpace(s)
				}
			}
		}
		next := strings.Index(raw[i+1:], "{")
		if next < 0 {
			break
		}
		i += 1 + next
	}
	return ""
}

func stripJSONBlob(s string) string {
	out := s
	for {
		i := strings.Index(out, "{")
		j := strings.LastIndex(out, "}")
		if i < 0 || j <= i {
			break
		}
		out = strings.TrimSpace(out[:i] + " " + out[j+1:])
	}
	return strings.Trim(out, ": ")
}

func looksLikeJSON(s string) bool {
	s = strings.TrimSpace(s)
	return strings.HasPrefix(s, "{") || strings.HasPrefix(s, "[")
}

func firstSentence(s string, max int) string {
	s = collapseSpace(s)
	if s == "" {
		return ""
	}
	if idx := strings.IndexAny(s, "\n"); idx > 0 {
		s = strings.TrimSpace(s[:idx])
	}
	if max > 0 && len(s) > max {
		s = strings.TrimSpace(s[:max]) + "…"
	}
	return s
}

func collapseSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
