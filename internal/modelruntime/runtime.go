// Package modelruntime manages private inference, model operations and active-use leases.
package modelruntime

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/runtimecfg"
)

// Message carries native Ollama text, image and tool-call content.
type Message struct {
	Role      string     `json:"role"`
	Content   string     `json:"content"`
	Images    []string   `json:"images,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	ToolName  string     `json:"tool_name,omitempty"`
}

// ToolCall is a model's structured tool invocation.
type ToolCall struct {
	Function struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	} `json:"function"`
}

// ChatRequest preserves model options and the caller's selected tools.
type ChatRequest struct {
	ExpectedDigest string         `json:"expected_digest,omitempty"`
	Model          string         `json:"model"`
	Messages       []Message      `json:"messages"`
	Tools          []any          `json:"tools,omitempty"`
	Stream         bool           `json:"stream"`
	Think          bool           `json:"think"`
	Options        map[string]any `json:"options,omitempty"`
	KeepAlive      int            `json:"keep_alive"`
}

// Chunk is one streamed inference response.
type Chunk struct {
	Message Message `json:"message"`
	Done    bool    `json:"done"`
	Error   string  `json:"error,omitempty"`
}

var leases = struct {
	sync.Mutex
	active    map[string]int
	exclusive map[string]bool
}{active: map[string]int{}, exclusive: map[string]bool{}}

var admission sync.Mutex

// reservations include admitted models whose first load has not yet appeared in /api/ps.
var reservations = map[string]int{}

func leaseKey(model string) string { return strings.TrimSuffix(model, ":latest") }

// AcquireOllama makes room using idle models only, then protects the admitted model.
func AcquireOllama(ctx context.Context, model string) (func(), error) {
	admission.Lock()
	defer admission.Unlock()
	model = leaseKey(model)
	release, err := Acquire(ctx, model)
	if err != nil {
		return nil, err
	}
	probe, done := context.WithTimeout(ctx, 10*time.Second)
	defer done()
	data, err := ollamaJSON(probe, "/api/ps", nil)
	if err != nil {
		release()
		return nil, err
	}
	var running struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err = json.Unmarshal(data, &running); err != nil {
		release()
		return nil, err
	}
	for _, loaded := range running.Models {
		if loaded.Name == model || loaded.Name == model+":latest" {
			return release, nil
		}
	}
	resident := map[string]bool{}
	for _, loaded := range running.Models {
		resident[leaseKey(loaded.Name)] = true
	}
	for name := range reservations {
		resident[name] = true
	}
	remaining := len(resident)
	for _, loaded := range running.Models {
		if resident[model] || remaining < runtimecfg.Int(context.TODO(), "ml.resident") {
			break
		}
		leases.Lock()
		busy := leases.active[leaseKey(loaded.Name)] > 0 || leases.exclusive[leaseKey(loaded.Name)]
		if !busy {
			leases.exclusive[leaseKey(loaded.Name)] = true
		}
		leases.Unlock()
		if busy {
			continue
		}
		_, err = ollamaJSON(probe, "/api/chat", map[string]any{"model": loaded.Name, "messages": []any{}, "keep_alive": 0, "stream": false})
		leases.Lock()
		delete(leases.exclusive, leaseKey(loaded.Name))
		leases.Unlock()
		if err != nil {
			release()
			return nil, fmt.Errorf("waiting_capacity: idle model eviction failed")
		}
		remaining--
	}
	if !resident[model] && remaining >= runtimecfg.Int(context.TODO(), "ml.resident") {
		release()
		return nil, fmt.Errorf("waiting_capacity: resident models are actively used")
	}
	reservations[model]++
	var once sync.Once
	return func() {
		once.Do(func() {
			admission.Lock()
			defer admission.Unlock()
			release()
			reservations[model]--
			if reservations[model] == 0 {
				delete(reservations, model)
			}
		})
	}, nil
}

// Acquire protects a model from removal/unload and enforces dynamic inference admission.
func Acquire(ctx context.Context, model string) (func(), error) {
	model = leaseKey(model)
	leases.Lock()
	defer leases.Unlock()
	total := 0
	for _, n := range leases.active {
		total += n
	}
	if leases.exclusive[model] || total >= runtimecfg.Int(context.TODO(), "ml.concurrent") {
		return nil, fmt.Errorf("waiting_capacity")
	}
	leases.active[model]++
	var once sync.Once
	return func() { once.Do(func() { leases.Lock(); leases.active[model]--; leases.Unlock() }) }, nil
}

// Active returns current model usage counts.
func Active() map[string]int {
	leases.Lock()
	defer leases.Unlock()
	out := map[string]int{}
	for k, n := range leases.active {
		if n > 0 {
			out[k] = n
		}
	}
	return out
}

// Token derives a control credential without exposing a user API token to inference.
func Token() string {
	secret := os.Getenv("ML_CONTROL_TOKEN")
	if secret == "" {
		secret = os.Getenv("ENCRYPTION_KEY")
	}
	if secret == "" {
		return ""
	}
	h := sha256.Sum256([]byte("rewind-ml-control-v1:" + secret))
	return hex.EncodeToString(h[:])
}

// URL is the internal sidecar address, never sent to browser clients.
func URL() string {
	if s := os.Getenv("ML_CONTROL_URL"); s != "" {
		return strings.TrimRight(s, "/")
	}
	return "http://rewind-ml:8090"
}
func ollamaURL() string {
	if s := os.Getenv("OLLAMA_HOST"); s != "" {
		return strings.TrimRight(s, "/")
	}
	return "http://127.0.0.1:11434"
}

// Request calls the authenticated sidecar with a bounded response.
func Request(ctx context.Context, path string, payload any) (json.RawMessage, error) {
	var body io.Reader
	method := "GET"
	if payload != nil {
		raw, e := json.Marshal(payload)
		if e != nil {
			return nil, e
		}
		body = bytes.NewReader(raw)
		method = "POST"
	}
	req, e := http.NewRequestWithContext(ctx, method, URL()+path, body)
	if e != nil {
		return nil, e
	}
	req.Header.Set("Authorization", "Bearer "+Token())
	req.Header.Set("Content-Type", "application/json")
	resp, e := http.DefaultClient.Do(req)
	if e != nil {
		return nil, e
	}
	defer resp.Body.Close()
	raw, e := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if e != nil {
		return nil, e
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("ML runtime HTTP %d: %.500s", resp.StatusCode, raw)
	}
	return raw, nil
}

// Chat streams text and native tool calls while honoring cancellation.
func Chat(ctx context.Context, in ChatRequest, onChunk func(Chunk) error) (Message, error) {
	in.Stream = true
	b, e := json.Marshal(in)
	if e != nil {
		return Message{}, e
	}
	req, e := http.NewRequestWithContext(ctx, "POST", URL()+"/v1/chat", bytes.NewReader(b))
	if e != nil {
		return Message{}, e
	}
	req.Header.Set("Authorization", "Bearer "+Token())
	req.Header.Set("Content-Type", "application/json")
	resp, e := http.DefaultClient.Do(req)
	if e != nil {
		return Message{}, e
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return Message{}, fmt.Errorf("inference HTTP %d: %s", resp.StatusCode, raw)
	}
	out := Message{Role: "assistant"}
	scan := bufio.NewScanner(resp.Body)
	scan.Buffer(make([]byte, 4096), 4<<20)
	done := false
	for scan.Scan() {
		var c Chunk
		if e = json.Unmarshal(scan.Bytes(), &c); e != nil {
			return out, e
		}
		if c.Error != "" {
			return out, fmt.Errorf("%s", c.Error)
		}
		out.Content += c.Message.Content
		out.ToolCalls = append(out.ToolCalls, c.Message.ToolCalls...)
		if onChunk != nil {
			if err := onChunk(c); err != nil {
				return out, err
			}
		}
		done = done || c.Done
	}
	if e = scan.Err(); e != nil {
		return out, e
	}
	if !done {
		return out, fmt.Errorf("incomplete inference stream")
	}
	return out, nil
}
func upstream(ctx context.Context, path string, payload any) (*http.Response, error) {
	var r io.Reader
	method := "GET"
	if payload != nil {
		b, _ := json.Marshal(payload)
		r = bytes.NewReader(b)
		method = "POST"
	}
	if path == "/api/delete" {
		method = "DELETE"
	}
	req, e := http.NewRequestWithContext(ctx, method, ollamaURL()+path, r)
	if e != nil {
		return nil, e
	}
	req.Header.Set("Content-Type", "application/json")
	return http.DefaultClient.Do(req)
}
func ollamaJSON(ctx context.Context, path string, payload any) (json.RawMessage, error) {
	r, e := upstream(ctx, path, payload)
	if e != nil {
		return nil, e
	}
	defer r.Body.Close()
	b, e := io.ReadAll(io.LimitReader(r.Body, 16<<20))
	if r.StatusCode != 200 {
		return nil, fmt.Errorf("ollama %s: %.500s", r.Status, b)
	}
	return b, e
}

// Serve starts the internal authenticated inference API and durable model-operation worker.
func Serve(ctx context.Context, dbc *db.DatabaseConnection) error {
	if Token() == "" {
		return fmt.Errorf("ML_CONTROL_TOKEN or ENCRYPTION_KEY is required")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/model", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Model string `json:"model"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in) != nil {
			http.Error(w, "invalid model", 400)
			return
		}
		info, err := inspectModel(r.Context(), in.Model)
		if err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		writeJSON(w, info)
	})
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		probeCtx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		r = r.WithContext(probeCtx)
		tags, e := ollamaJSON(r.Context(), "/api/tags", nil)
		health := map[string]string{"ollama": "healthy"}
		if e != nil {
			health["ollama"] = "unavailable"
			tags = json.RawMessage(`{"models":[]}`)
		}
		running, _ := ollamaJSON(r.Context(), "/api/ps", nil)
		whisper := []map[string]any{}
		for _, name := range WhisperModels {
			path := filepath.Join(whisperRoot(), "ggml-"+name+".bin")
			st, e := os.Stat(path)
			item := map[string]any{"name": name, "installed": e == nil}
			if e == nil {
				item["size"] = st.Size()
			}
			whisper = append(whisper, item)
		}
		out := map[string]any{"ollama": tags, "running": running, "active": Active(), "whisper": whisper, "health": health, "hardware": detectHardware(r.Context())}
		if endpoint := strings.TrimRight(os.Getenv("VISION_URL"), "/"); endpoint != "" {
			probe, done := context.WithTimeout(r.Context(), 3*time.Second)
			req, _ := http.NewRequestWithContext(probe, "GET", endpoint+"/v1/capabilities", nil)
			req.Header.Set("Authorization", "Bearer "+os.Getenv("VISION_TOKEN"))
			if response, err := http.DefaultClient.Do(req); err == nil {
				body, err := io.ReadAll(io.LimitReader(response.Body, 64<<10))
				response.Body.Close()
				if err == nil && response.StatusCode == 200 {
					out["vision"] = json.RawMessage(body)
				}
			}
			done()
		}
		var list struct {
			Models []struct {
				Name string `json:"name"`
			}
		}
		_ = json.Unmarshal(tags, &list)
		details := map[string]json.RawMessage{}
		for _, m := range list.Models {
			if b, e := ollamaJSON(r.Context(), "/api/show", map[string]any{"model": m.Name}); e == nil {
				var d map[string]json.RawMessage
				_ = json.Unmarshal(b, &d)
				clean, _ := json.Marshal(map[string]any{"capabilities": d["capabilities"], "details": d["details"]})
				details[m.Name] = clean
			}
		}
		out["details"] = details
		writeJSON(w, out)
	})
	mux.HandleFunc("/v1/chat", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		var in ChatRequest
		if e := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<20)).Decode(&in); e != nil {
			http.Error(w, "invalid chat", 400)
			return
		}
		release, e := AcquireOllama(r.Context(), in.Model)
		if e != nil {
			http.Error(w, e.Error(), http.StatusTooManyRequests)
			return
		}
		defer release()
		info, err := inspectModel(r.Context(), in.Model)
		if err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		if in.ExpectedDigest != "" && in.ExpectedDigest != info.Digest {
			http.Error(w, "assigned model digest changed; start a new run", http.StatusConflict)
			return
		}
		in.ExpectedDigest = ""
		resp, e := upstream(r.Context(), "/api/chat", in)
		if e != nil {
			http.Error(w, "inference unavailable", http.StatusServiceUnavailable)
			return
		}
		defer resp.Body.Close()
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(resp.StatusCode)
		buf := make([]byte, 8192)
		for {
			n, e := resp.Body.Read(buf)
			if n > 0 {
				if _, err := w.Write(buf[:n]); err != nil {
					return
				}
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
			}
			if e != nil {
				return
			}
		}
	})
	server := &http.Server{Addr: ":8090", ReadHeaderTimeout: 10 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := "Bearer " + Token()
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte(want)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		mux.ServeHTTP(w, r)
	})}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	go operations(ctx, dbc)
	return server.ListenAndServe()
}

// ModelInfo pins installed weights and exposes advertised inference capabilities.
type ModelInfo struct {
	Digest       string   `json:"digest"`
	Capabilities []string `json:"capabilities"`
}

func inspectModel(ctx context.Context, model string) (*ModelInfo, error) {
	data, err := ollamaJSON(ctx, "/api/tags", nil)
	if err != nil {
		return nil, err
	}
	var tags struct {
		Models []struct {
			Name   string `json:"name"`
			Digest string `json:"digest"`
		} `json:"models"`
	}
	if err = json.Unmarshal(data, &tags); err != nil {
		return nil, err
	}
	info := &ModelInfo{}
	for _, tag := range tags.Models {
		if tag.Name == model || tag.Name == model+":latest" {
			info.Digest = tag.Digest
			break
		}
	}
	if info.Digest == "" {
		return nil, fmt.Errorf("model is not installed: %s", model)
	}
	data, err = ollamaJSON(ctx, "/api/show", map[string]any{"model": model})
	if err != nil {
		return nil, err
	}
	var show struct {
		Capabilities []string `json:"capabilities"`
	}
	if err = json.Unmarshal(data, &show); err != nil {
		return nil, err
	}
	info.Capabilities = show.Capabilities
	return info, nil
}
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// WhisperModels lists explicitly installable whisper.cpp models.
var WhisperModels = []string{"tiny", "base", "small", "medium", "large-v3", "large-v3-turbo"}

func whisperRoot() string {
	if s := os.Getenv("WHISPER_MODEL_DIR"); s != "" {
		return s
	}
	return "/models/whisper"
}

var modelName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,200}$`)

// Enqueue validates and persists an explicit model-management request.
func Enqueue(ctx context.Context, dbc *db.DatabaseConnection, user pgtype.UUID, runtime, model, action string, options map[string]any) (*db.ModelOperation, error) {
	if err := runtimecfg.RequireAdmin(ctx, dbc, user); err != nil {
		return nil, err
	}
	if !modelName.MatchString(model) || strings.Contains(model, "..") || strings.Contains(model, "://") {
		return nil, fmt.Errorf("invalid model name")
	}
	switch runtime {
	case "ollama":
	case "whisper":
		if action == "load" || action == "unload" {
			return nil, fmt.Errorf("whisper uses a process per job and unloads on completion; use test to load and verify weights")
		}
		ok := false
		for _, m := range WhisperModels {
			ok = ok || model == m
		}
		if !ok {
			return nil, fmt.Errorf("unsupported Whisper preset")
		}
	case "vision":
		if model != "ViT-B-32__openai" {
			return nil, fmt.Errorf("unsupported vision preset")
		}
	default:
		return nil, fmt.Errorf("unknown runtime")
	}
	switch action {
	case "install", "load", "unload", "test", "remove":
	default:
		return nil, fmt.Errorf("unknown model action")
	}

	raw, _ := json.Marshal(options)
	if string(raw) == "null" {
		raw = []byte("{}")
	}
	return dbc.Queries(ctx).CreateModelOperation(ctx, &db.CreateModelOperationParams{UserID: user, Runtime: runtime, Model: model, Action: action, Options: raw})
}
func operations(ctx context.Context, dbc *db.DatabaseConnection) {
	q := dbc.Queries(ctx)
	_ = q.RecoverModelOperations(ctx)
	for ctx.Err() == nil {
		op, e := q.ClaimModelOperation(ctx)
		if e != nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
			continue
		}
		lastProgress := map[string]any{}
		progress := func(v any) {
			if fields, ok := v.(map[string]any); ok {
				for key, value := range fields {
					lastProgress[key] = value
				}
			}
			b, _ := json.Marshal(v)
			_ = q.UpdateModelOperation(ctx, &db.UpdateModelOperationParams{ID: op.ID, Status: "running", Progress: b})
		}
		work, cancel := context.WithTimeout(ctx, 2*time.Hour)
		user, e := q.SelectUserByID(work, op.UserID)
		if e == nil && (!user.Enabled || user.Role != "admin") {
			e = fmt.Errorf("admin access required")
		}
		if e == nil {
			e = operate(work, dbc, op, progress)
		}
		cancel()
		state := "completed"
		data := lastProgress
		data["status"] = "completed"
		if e != nil {
			state = "failed"
			data = map[string]any{"error": e.Error()}
		}
		b, _ := json.Marshal(data)
		_ = q.UpdateModelOperation(ctx, &db.UpdateModelOperationParams{ID: op.ID, Status: state, Progress: b})
	}
}
func operate(ctx context.Context, dbc *db.DatabaseConnection, op *db.ModelOperation, progress func(any)) error {
	if op.Action == "remove" || op.Action == "install" {
		used, err := dbc.Queries(ctx).AgentUsesModel(ctx, op.Model)
		if err != nil {
			return err
		}
		if used {
			return fmt.Errorf("model is pinned by an active agent run")
		}
	}
	key := leaseKey(op.Model)
	if op.Runtime == "ollama" && (op.Action == "load" || op.Action == "test") {
		release, err := AcquireOllama(ctx, op.Model)
		if err != nil {
			return err
		}
		defer release()
	} else {
		leases.Lock()
		if leases.active[key] > 0 || leases.exclusive[key] {
			leases.Unlock()
			return fmt.Errorf("model is in active use")
		}
		leases.exclusive[key] = true
		leases.Unlock()
		defer func() { leases.Lock(); delete(leases.exclusive, key); leases.Unlock() }()
	}
	if op.Action == "remove" {
		s, e := runtimecfg.Read(ctx, dbc)
		if e != nil {
			return e
		}
		for k, v := range s {
			if strings.HasSuffix(k, "model") && leaseKey(fmt.Sprint(v)) == key {
				return fmt.Errorf("model is assigned to %s", k)
			}
			if k == "ml.challengers" {
				for _, name := range strings.Split(fmt.Sprint(v), ",") {
					if leaseKey(strings.TrimSpace(name)) == key {
						return fmt.Errorf("model is assigned as a context fallback")
					}
				}
			}
		}
	}
	if op.Runtime == "vision" {
		return visionOperation(ctx, op)
	}
	if op.Runtime == "whisper" {
		return whisperOperation(ctx, op, progress)
	}
	if op.Action == "install" {
		resp, e := upstream(ctx, "/api/pull", map[string]any{"model": op.Model, "stream": true})
		if e != nil {
			return e
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return fmt.Errorf("pull HTTP %d", resp.StatusCode)
		}
		scan := bufio.NewScanner(resp.Body)
		scan.Buffer(make([]byte, 4096), 1<<20)
		success := false
		for scan.Scan() {
			var v map[string]any
			if e = json.Unmarshal(scan.Bytes(), &v); e != nil {
				return e
			}
			progress(v)
			if msg, ok := v["error"].(string); ok {
				return fmt.Errorf("%s", msg)
			}
			success = success || v["status"] == "success"
		}
		if e = scan.Err(); e != nil {
			return e
		}
		if !success {
			return fmt.Errorf("model installation did not finish")
		}
		return nil
	}
	if op.Action == "remove" {
		_, e := ollamaJSON(ctx, "/api/delete", map[string]any{"model": op.Model})
		return e
	}
	keep := runtimecfg.Int(ctx, "ml.keep_alive")
	messages := []Message{}
	if op.Action == "unload" {
		keep = 0
	}
	if op.Action == "test" {
		messages = []Message{{Role: "user", Content: "Reply with the word ready."}}
	}
	data, e := ollamaJSON(ctx, "/api/chat", ChatRequest{Model: op.Model, Messages: messages, Stream: false, KeepAlive: keep, Options: map[string]any{"num_predict": 32, "num_ctx": 4096}})
	if e != nil {
		return e
	}
	var result Chunk
	if e = json.Unmarshal(data, &result); e != nil {
		return e
	}
	if !result.Done || result.Error != "" {
		return fmt.Errorf("model operation did not finish: %s", result.Error)
	}
	progress(map[string]any{"reply": result.Message.Content, "status": "verified"})
	return nil
}
func whisperOperation(ctx context.Context, op *db.ModelOperation, progress func(any)) error {
	root := whisperRoot()
	path := filepath.Join(root, "ggml-"+op.Model+".bin")
	if op.Action == "remove" {
		return os.Remove(path)
	}
	if op.Action != "install" {
		st, e := os.Stat(path)
		if e != nil {
			return e
		}
		if st.Size() < 1024 {
			return fmt.Errorf("invalid model weights")
		}
		if op.Action != "test" {
			return fmt.Errorf("whisper residency is scoped to each job")
		}
		file, err := os.CreateTemp("", "rewind-model-test-*.wav")
		if err != nil {
			return err
		}
		defer os.Remove(file.Name())
		// One second of PCM silence exercises weight loading and the inference executable.
		wav := make([]byte, 44+32000)
		copy(wav, "RIFF")
		binary.LittleEndian.PutUint32(wav[4:], uint32(len(wav)-8))
		copy(wav[8:], "WAVEfmt ")
		binary.LittleEndian.PutUint32(wav[16:], 16)
		binary.LittleEndian.PutUint16(wav[20:], 1)
		binary.LittleEndian.PutUint16(wav[22:], 1)
		binary.LittleEndian.PutUint32(wav[24:], 16000)
		binary.LittleEndian.PutUint32(wav[28:], 32000)
		binary.LittleEndian.PutUint16(wav[32:], 2)
		binary.LittleEndian.PutUint16(wav[34:], 16)
		copy(wav[36:], "data")
		binary.LittleEndian.PutUint32(wav[40:], 32000)
		_, err = file.Write(wav)
		closeErr := file.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		command := os.Getenv("WHISPER_CMD")
		if command == "" {
			command = "whisper-cli"
		}
		args := []string{"-m", path, "-f", file.Name(), "-nt"}
		if runtimecfg.Env(ctx, "WHISPER_DEVICE") == "cpu" {
			args = append(args, "-ng")
		}
		cmd := exec.CommandContext(ctx, command, args...)
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("whisper test: %w: %.500s", err, output)
		}
		return nil
	}
	if e := os.MkdirAll(root, 0755); e != nil {
		return e
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-"+op.Model+".bin", nil)
	resp, e := http.DefaultClient.Do(req)
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("download HTTP %d", resp.StatusCode)
	}
	f, e := os.CreateTemp(root, ".model-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	hash := sha256.New()
	n, e := io.Copy(io.MultiWriter(f, hash), &progressReader{reader: resp.Body, total: resp.ContentLength, report: progress})
	closeErr := f.Close()
	if e != nil {
		return e
	}
	if closeErr != nil {
		return closeErr
	}
	if n < 1<<20 || (resp.ContentLength > 0 && n != resp.ContentLength) {
		return fmt.Errorf("incomplete weights")
	}
	progress(map[string]any{"completed": n})
	if err := os.Rename(f.Name(), path); err != nil {
		return err
	}
	manifest, _ := json.Marshal(map[string]any{"sha256": hex.EncodeToString(hash.Sum(nil)), "size": n, "source": req.URL.String(), "installed_at": time.Now().UTC()})
	return os.WriteFile(path+".json", manifest, 0600)
}

type progressReader struct {
	reader           io.Reader
	total, completed int64
	last             time.Time
	report           func(any)
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, e := p.reader.Read(b)
	p.completed += int64(n)
	if time.Since(p.last) > time.Second {
		p.report(map[string]any{"completed": p.completed, "total": p.total})
		p.last = time.Now()
	}
	return n, e
}
func visionOperation(ctx context.Context, op *db.ModelOperation) error {
	var options map[string]any
	_ = json.Unmarshal(op.Options, &options)
	raw, _ := json.Marshal(map[string]any{"model": op.Model, "action": op.Action, "accept_license": options["accept_license"]})
	url := strings.TrimRight(os.Getenv("VISION_URL"), "/")
	if url == "" {
		return fmt.Errorf("vision runtime unavailable")
	}
	req, _ := http.NewRequestWithContext(ctx, "POST", url+"/v1/models/manage", bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer "+os.Getenv("VISION_TOKEN"))
	req.Header.Set("Content-Type", "application/json")
	resp, e := http.DefaultClient.Do(req)
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != 200 {
		return fmt.Errorf("vision: %.500s", b)
	}
	return nil
}
