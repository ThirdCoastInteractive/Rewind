//go:build realmodel

package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/internal/application"
	"thirdcoast.systems/rewind/internal/config"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/modelruntime"
	"thirdcoast.systems/rewind/internal/runtimecfg"
)

func TestQwenArchiveCompilation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 13*time.Minute)
	defer cancel()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	// The provider is real; every archive operation uses only the disposable database.
	ollama := func(ctx context.Context, path string, payload []byte, out io.Writer) error {
		args := []string{"compose", "exec", "-T", "rewind-ml", "curl", "--no-buffer", "-fsS", "-H", "Content-Type: application/json"}
		if payload != nil {
			args = append(args, "-d", "@-")
		}
		args = append(args, "http://127.0.0.1:11434"+path)
		cmd := exec.CommandContext(ctx, "docker", args...)
		cmd.Dir = root
		cmd.Stdin = bytes.NewReader(payload)
		cmd.Stdout = out
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("Ollama: %w: %s", err, stderr.String())
		}
		return nil
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if model := os.Getenv("LMSTUDIO_EVAL_MODEL"); model != "" {
			serveLMStudioEvaluation(w, r, model)
			return
		}
		if r.URL.Path == "/v1/model" {
			var tags bytes.Buffer
			if err := ollama(r.Context(), "/api/tags", nil, &tags); err != nil {
				http.Error(w, err.Error(), 503)
				return
			}
			var parsed struct {
				Models []struct {
					Name   string `json:"name"`
					Digest string `json:"digest"`
				} `json:"models"`
			}
			_ = json.Unmarshal(tags.Bytes(), &parsed)
			digest := ""
			for _, m := range parsed.Models {
				if m.Name == "qwen3.5:4b" {
					digest = m.Digest
				}
			}
			var show bytes.Buffer
			if err := ollama(r.Context(), "/api/show", []byte(`{"model":"qwen3.5:4b"}`), &show); err != nil {
				http.Error(w, err.Error(), 503)
				return
			}
			var info modelruntime.ModelInfo
			_ = json.Unmarshal(show.Bytes(), &info)
			info.Digest = digest
			_ = json.NewEncoder(w).Encode(info)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 16<<20))
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		if err := ollama(r.Context(), "/api/chat", body, flushWriter{w}); err != nil {
			t.Log(err)
		}
	}))
	defer server.Close()
	t.Setenv("ML_CONTROL_URL", server.URL)
	pool, err := application.OpenDBPoolWithRetry(ctx, config.Config{DatabaseDSN: "postgres://rewind_test:disposable-test-only@127.0.0.1:15439/rewind_test?sslmode=disable", DatabaseRetries: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	dbc := &db.DatabaseConnection{Pool: pool}
	if err = dbc.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	id := func() pgtype.UUID { return pgtype.UUID{Bytes: uuid.New(), Valid: true} }
	user, creator, channel := id(), id(), id()
	speaker := "Jeremy Hambley archive " + strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return 'g' + r - '0'
		}
		return 'q' + r - 'a'
	}, creator.String()[:8])
	sql := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	sql("INSERT INTO users(id,user_name,email,password,enabled) VALUES($1,$2,$2,'fixture',true)", user, user.String())
	sql("INSERT INTO creators(id,name) VALUES($1,$2)", creator, speaker)
	sql("INSERT INTO channels(id,platform,identity_key,uploader,creator_id) VALUES($1,'youtube',$2,$3,$4)", channel, channel.String(), speaker, creator)
	videos := []pgtype.UUID{id(), id()}
	for i, v := range videos {
		sql("INSERT INTO videos(id,src,archived_by,title,uploader,media,video_path,duration_seconds,channel_row_id) VALUES($1,$2,$3,$4,$5,'video','/fixture.mp4',120,$6)", v, v.String(), user, fmt.Sprintf("Discussion %d", i+1), speaker, channel)
		text := "Jeremy: Adam Sellers was talking about hot tubs. I disagree with Adam Sellers on hot tubs."
		cues, _ := json.Marshal([]map[string]any{{"start": 10, "end": 20, "text": text}})
		sql("INSERT INTO video_transcripts(video_id,lang,format,text,raw,cues,search) VALUES($1,'en','vtt',$2,'',$3,to_tsvector('simple',$2))", v, text, cues)
	}
	q := dbc.Queries(ctx)
	conv, err := q.CreateAgentConversation(ctx, &db.CreateAgentConversationParams{UserID: user, Title: "Qwen acceptance fixture"})
	if err != nil {
		t.Fatal(err)
	}
	messages, _ := json.Marshal([]modelruntime.Message{{Role: "user", Content: "Find every time " + speaker + " talks about Adam, Adam Sellers, and hot tubs, and create a new stitch project compiling them together. Use only this exact creator in this fixture. Include citations and inspect the returned evidence."}})
	settings := runtimecfg.Defaults()
	if model := os.Getenv("LMSTUDIO_EVAL_MODEL"); model != "" {
		settings["agent.model"] = model
	}
	settings["agent.max_calls"] = 24
	settings["agent.max_minutes"] = 12
	snapshot, _ := json.Marshal(settings)
	run, err := q.CreateAgentRun(ctx, &db.CreateAgentRunParams{UserID: user, ConversationID: conv.ID, Messages: messages, Settings: snapshot})
	if err != nil {
		t.Fatal(err)
	}
	sql("UPDATE agent_runs SET lease_owner='realmodel-fixture',lease_until=now()+interval '60 seconds',status='running' WHERE id=$1", run.ID)
	run.LeaseOwner = "realmodel-fixture"
	start := time.Now()
	execute(ctx, dbc, run)
	final, err := q.GetAgentRun(ctx, &db.GetAgentRunParams{ID: run.ID, UserID: user})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("model=%s digest=%s elapsed=%s calls=%d status=%s error=%s", settings["agent.model"], final.ModelDigest, time.Since(start), final.Calls, final.Status, final.LastError)
	journal, err := q.ListAgentToolCalls(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, call := range journal {
		t.Logf("tool=%s args=%s result=%.1000s", call.Name, call.Arguments, call.Result)
		if call.Name != "create_stitch_project" {
			continue
		}
		var wire struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		}
		if json.Unmarshal(call.Result, &wire) != nil || len(wire.Content) == 0 {
			continue
		}
		var result struct {
			ProjectID string `json:"project_id"`
		}
		if json.Unmarshal([]byte(wire.Content[0].Text), &result) != nil {
			continue
		}
		var projectID pgtype.UUID
		if projectID.Scan(result.ProjectID) != nil {
			continue
		}
		project, err := q.GetStitchProject(ctx, projectID)
		if err != nil {
			t.Fatal(err)
		}
		var segments []struct {
			VideoID string  `json:"video_id"`
			Start   float64 `json:"start_ts"`
			End     float64 `json:"end_ts"`
		}
		if json.Unmarshal(project.Segments, &segments) != nil {
			t.Fatal("invalid project")
		}
		seen := map[string]bool{}
		for _, segment := range segments {
			seen[segment.VideoID] = true
			if segment.Start > 10 || segment.End < 20 {
				t.Fatal("clip does not cover fixture evidence")
			}
		}
		if !seen[videos[0].String()] || !seen[videos[1].String()] || len(seen) != 2 {
			t.Fatalf("wrong fixture clips: %+v", segments)
		}
		found = true
	}
	if !found {
		t.Fatal("model did not create the required database artifact")
	}
	if final.Status != "completed" {
		t.Fatalf("run did not complete: %s", final.LastError)
	}
}

type flushWriter struct{ http.ResponseWriter }

func (w flushWriter) Write(p []byte) (int, error) {
	n, err := w.ResponseWriter.Write(p)
	w.ResponseWriter.(http.Flusher).Flush()
	return n, err
}
