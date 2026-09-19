package mlcore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// DefaultContextModel is the production default. Challengers (gemma4:26b,
// gemma4:12b) are selected via CONTEXT_MODEL / CONTEXT_MODEL_CHALLENGERS.
// gpt-oss-20b is never a default.
const DefaultContextModel = "qwen3.8:27b"

// Ollama talks to a running Ollama HTTP API. It never calls /api/pull.
type Ollama struct {
	BaseURL        string
	HTTP           *http.Client
	Model          string
	VerifiedDigest string // Successful canary for this worker process, invalidated on infrastructure errors.
	initOnce       sync.Once
	skip           *modelSkip
	verified       *verifiedModel
}

// verifiedModel is the one loaded Ollama weights all context jobs should share.
type verifiedModel struct {
	mu       sync.Mutex
	canaryMu sync.Mutex // serializes first load so sibling jobs wait instead of each acquiring
	name     string
	digest   string
}

// modelSkip is heap state shared by ForJob copies so a killed 27B stays skipped.
type modelSkip struct {
	mu   sync.Mutex
	dead map[string]string
}

func modelKey(name string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(name)), ":latest")
}

// ForJob returns a per-job client that shares unusable-model and loaded-digest
// state with o, so many context jobs can pile onto one resident model.
func (o *Ollama) ForJob(model string) *Ollama {
	if o == nil {
		return &Ollama{Model: strings.TrimSpace(model), skip: &modelSkip{dead: map[string]string{}}, verified: &verifiedModel{}}
	}
	o.ensureStores()
	return &Ollama{
		BaseURL:  o.BaseURL,
		HTTP:     o.HTTP,
		Model:    strings.TrimSpace(model),
		skip:     o.skip,
		verified: o.verified,
	}
}

func (o *Ollama) ensureStores() {
	if o == nil {
		return
	}
	o.initOnce.Do(func() {
		if o.skip == nil {
			o.skip = &modelSkip{dead: map[string]string{}}
		}
		if o.verified == nil {
			o.verified = &verifiedModel{}
		}
	})
}

func (o *Ollama) skipStore() *modelSkip {
	if o == nil {
		return &modelSkip{dead: map[string]string{}}
	}
	o.ensureStores()
	return o.skip
}

func (o *Ollama) verifiedStore() *verifiedModel {
	if o == nil {
		return &verifiedModel{}
	}
	o.ensureStores()
	return o.verified
}

// AlreadyLoaded is true when a sibling job already canaried this digest.
func (o *Ollama) AlreadyLoaded(digest string) bool {
	if o == nil || digest == "" {
		return false
	}
	v := o.verifiedStore()
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.digest == digest
}

// Resident is true after a successful canary. Context workers claim one loader
// job until this is true, then a few jobs share the loaded weights.
func (o *Ollama) Resident() bool {
	if o == nil {
		return false
	}
	v := o.verifiedStore()
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.digest != ""
}

// MarkUnusable records that name killed llama-server (or otherwise cannot run).
func (o *Ollama) MarkUnusable(name, reason string) {
	name = modelKey(name)
	if name == "" || o == nil {
		return
	}
	if strings.TrimSpace(reason) == "" {
		reason = ErrInfrastructure.Error()
	}
	s := o.skipStore()
	s.mu.Lock()
	if s.dead == nil {
		s.dead = map[string]string{}
	}
	s.dead[name] = reason
	s.mu.Unlock()
	o.VerifiedDigest = ""
	v := o.verifiedStore()
	v.mu.Lock()
	if modelKey(v.name) == name {
		v.name, v.digest = "", ""
	}
	v.mu.Unlock()
}

// Unusable reports whether name was marked dead after an infrastructure failure.
func (o *Ollama) Unusable(name string) (reason string, ok bool) {
	if o == nil || o.skip == nil {
		return "", false
	}
	o.skip.mu.Lock()
	defer o.skip.mu.Unlock()
	reason, ok = o.skip.dead[modelKey(name)]
	return reason, ok
}

func (o *Ollama) client() *http.Client {
	if o.HTTP != nil {
		return o.HTTP
	}
	return &http.Client{Timeout: 15 * time.Minute}
}

func (o *Ollama) base() string {
	b := strings.TrimRight(strings.TrimSpace(o.BaseURL), "/")
	if b == "" {
		return "http://127.0.0.1:11434"
	}
	return b
}

type ollamaTag struct {
	Name   string `json:"name"`
	Model  string `json:"model"`
	Digest string `json:"digest"`
}

type ollamaTags struct {
	Models []ollamaTag `json:"models"`
}

// HasModel reports whether name is already installed. Missing → waiting_model.
// Connection errors are also waiting_model (daemon not up); never pulls.
func (o *Ollama) HasModel(ctx context.Context, name string) (digest string, err error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("%w: empty model name", ErrWaitingModel)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.base()+"/api/tags", nil)
	if err != nil {
		return "", err
	}
	resp, err := o.client().Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: ollama unreachable: %v", ErrWaitingModel, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode >= 500 {
			return "", fmt.Errorf("%w: ollama /api/tags HTTP %s", ErrInfrastructure, resp.Status)
		}
		return "", fmt.Errorf("%w: ollama /api/tags HTTP %s", ErrWaitingModel, resp.Status)
	}
	var tags ollamaTags
	if err := json.Unmarshal(body, &tags); err != nil {
		return "", fmt.Errorf("%w: ollama tags JSON: %v", ErrWaitingModel, err)
	}
	want := strings.ToLower(name)
	for _, m := range tags.Models {
		cand := strings.ToLower(strings.TrimSpace(m.Name))
		if cand == "" {
			cand = strings.ToLower(strings.TrimSpace(m.Model))
		}
		if cand == want || strings.TrimSuffix(cand, ":latest") == strings.TrimSuffix(want, ":latest") {
			d := strings.TrimSpace(m.Digest)
			if d == "" {
				d = cand
			}
			return d, nil
		}
	}
	return "", fmt.Errorf("%w: ollama tag %q not installed", ErrWaitingModel, name)
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Think     bool           `json:"think"`
	Options   map[string]any `json:"options"`
	KeepAlive int            `json:"keep_alive"`
	Model     string         `json:"model"`
	Stream    bool           `json:"stream"`
	Format    string         `json:"format,omitempty"`
	Messages  []chatMessage  `json:"messages"`
}

type chatResponse struct {
	Message chatMessage `json:"message"`
	Error   string      `json:"error"`
}

// ChatJSON asks the model for a JSON object. No pull.
func (o *Ollama) ChatJSON(ctx context.Context, model, system, user string) (string, error) {
	user = trimUserToFit(system, user)
	if !chatFits(system, user) {
		return "", fmt.Errorf("context input exceeds the configured %d-token budget", contextBudget)
	}
	model = strings.TrimSpace(model)
	if model == "" {
		model = o.Model
	}
	payload, err := json.Marshal(chatRequest{
		Options:   map[string]any{"num_ctx": contextBudget, "num_predict": predictTokens, "temperature": 0, "use_mmap": false},
		KeepAlive: 300,
		Model:     model,
		Stream:    false,
		Format:    "json",
		Messages: []chatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.base()+"/api/chat", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := o.client().Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: ollama unreachable: %v", ErrWaitingModel, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if resp.StatusCode != http.StatusOK {
		msg := strings.TrimSpace(string(body))
		if extracted := ollamaErrorMessage(body); extracted != "" {
			msg = extracted
		}
		if resp.StatusCode >= 500 || isOllamaInfrastructure(msg) {
			err := fmt.Errorf("%w: ollama chat HTTP %s: %s", ErrInfrastructure, resp.Status, msg)
			// A load-time 500 (cancelled canary, llama-server still coming up)
			// must not poison the skip list or every context job parks.
			if o.Resident() {
				o.MarkUnusable(model, err.Error())
			}
			return "", err
		}
		return "", fmt.Errorf("ollama chat HTTP %s: %s", resp.Status, msg)
	}
	var out chatResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("ollama chat JSON: %w", err)
	}
	if strings.TrimSpace(out.Error) != "" {
		err := classifyOllamaAPIError(out.Error)
		if errors.Is(err, ErrInfrastructure) && o.Resident() {
			o.MarkUnusable(model, err.Error())
		}
		return "", err
	}
	return out.Message.Content, nil
}

func ollamaErrorMessage(body []byte) string {
	var wrap struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &wrap) != nil {
		return ""
	}
	return strings.TrimSpace(wrap.Error)
}

func isOllamaInfrastructure(msg string) bool {
	low := strings.ToLower(msg)
	if strings.Contains(low, "signal: killed") {
		return true
	}
	if strings.Contains(low, "llama-server process has terminated") {
		return true
	}
	if strings.Contains(low, "llama-server") {
		return true
	}
	if strings.Contains(low, "timed out") {
		return true
	}
	return false
}

func classifyOllamaAPIError(msg string) error {
	if isOllamaInfrastructure(msg) {
		return fmt.Errorf("%w: %s", ErrInfrastructure, msg)
	}
	low := strings.ToLower(msg)
	if strings.Contains(low, "not found") || strings.Contains(low, "pull") {
		return fmt.Errorf("%w: %s", ErrWaitingModel, msg)
	}
	return fmt.Errorf("ollama: %s", msg)
}

// ErrInfrastructure distinguishes runtime failures from invalid content or missing assets.
var ErrInfrastructure = fmt.Errorf("inference runtime unavailable")

// ShareLoaded makes digest resident once. Concurrent callers wait for the first
// canary instead of each taking an admission slot; afterward they share the
// loaded weights. load runs only on the first miss (AcquireOllama); pass nil
// when the caller only needs the canary.
func (o *Ollama) ShareLoaded(ctx context.Context, model, digest string, load func() (func(), error)) (release func(), err error) {
	v := o.verifiedStore()
	v.canaryMu.Lock()
	defer v.canaryMu.Unlock()
	if digest != "" && o.AlreadyLoaded(digest) {
		o.VerifiedDigest = digest
		return func() {}, nil
	}
	release = func() {}
	if load != nil {
		release, err = load()
		if err != nil {
			return nil, err
		}
	}
	if err = o.canaryLocked(ctx, model, digest); err != nil {
		release()
		return nil, err
	}
	return release, nil
}

// Canary verifies actual inference at the production context size using the installed model.
func (o *Ollama) Canary(ctx context.Context, model, digest string) error {
	_, err := o.ShareLoaded(ctx, model, digest, nil)
	return err
}

func (o *Ollama) canaryLocked(ctx context.Context, model, digest string) error {
	if digest != "" && (o.VerifiedDigest == digest || o.AlreadyLoaded(digest)) {
		o.VerifiedDigest = digest
		return nil
	}
	// 27B CUDA load on a 24 GiB card is ~5 minutes; the canary must outlast that
	// or the first ShareLoaded caller times out and siblings retry for nothing.
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	raw, err := o.ChatJSON(ctx, model, "Return a JSON object only.", "Return exactly {\"ok\":true}.")
	if err != nil {
		return err
	}
	var out struct {
		OK bool `json:"ok"`
	}
	if json.Unmarshal([]byte(raw), &out) != nil || !out.OK {
		return fmt.Errorf("canary returned invalid JSON")
	}
	o.VerifiedDigest = digest
	v := o.verifiedStore()
	v.mu.Lock()
	v.name, v.digest = model, digest
	v.mu.Unlock()
	return nil
}

// GenerateWindows chats once, and on invalid JSON retries repair exactly once.
func (o *Ollama) GenerateWindows(ctx context.Context, model, user string) ([]ModelWindow, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Minute)
	defer cancel()
	raw, err := o.ChatJSON(ctx, model, SystemPrompt(), user)
	if err != nil {
		return nil, err
	}
	return DecodeWindows(raw, func(prev string) (string, error) {
		return o.ChatJSON(ctx, model, RepairSystemPrompt(), "Previous reply:\n"+prev)
	})
}

// PickInstalledModel returns the first installed candidate that has not been
// marked unusable after an infrastructure failure. It never pulls.
func PickInstalledModel(ctx context.Context, o *Ollama, primary string, challengers []string) (name, digest string, err error) {
	candidates := contextModelCandidates(primary, challengers)
	var last error
	var skipped []string
	for _, name := range candidates {
		if reason, dead := o.Unusable(name); dead {
			skipped = append(skipped, name+": "+reason)
			last = fmt.Errorf("%w: %s unusable: %s", ErrWaitingModel, name, reason)
			continue
		}
		d, err := o.HasModel(ctx, name)
		if err == nil {
			return name, d, nil
		}
		last = err
		if errors.Is(err, ErrInfrastructure) {
			return "", "", err
		}
	}
	if len(skipped) > 0 {
		return "", "", fmt.Errorf("%w: no smaller context model installed after infrastructure failure (%s)", ErrWaitingModel, strings.Join(skipped, "; "))
	}
	if last == nil {
		last = fmt.Errorf("%w: no context model installed", ErrWaitingModel)
	}
	return "", "", last
}

func contextModelCandidates(primary string, challengers []string) []string {
	candidates := make([]string, 0, 1+len(challengers))
	if strings.TrimSpace(primary) != "" {
		candidates = append(candidates, strings.TrimSpace(primary))
	}
	seen := map[string]struct{}{}
	for _, c := range candidates {
		seen[modelKey(c)] = struct{}{}
	}
	for _, c := range challengers {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if strings.EqualFold(c, "gpt-oss-20b") && !strings.EqualFold(primary, c) {
			// Never implicit-default this tag; only use if the user set it as primary.
			continue
		}
		key := modelKey(c)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		candidates = append(candidates, c)
	}
	if len(candidates) == 0 {
		return []string{DefaultContextModel}
	}
	return candidates
}
