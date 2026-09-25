package templates

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"thirdcoast.systems/rewind/internal/db"
)

func TestVideoProcessingMissingTranscriptAndFailure(t *testing.T) {
	var out bytes.Buffer
	err := VideoProcessing("video", []*db.MlJob{{Kind: "context_windows", Status: "waiting_model", LastError: "model <failed>"}}, nil, false, 0).Render(context.Background(), &out)
	if err != nil {
		t.Fatal(err)
	}
	html := out.String()
	for _, want := range []string{"No transcript yet. Generate captions first", "Generate captions", "disabled", "waiting for model", "model &lt;failed&gt;", ">Retry</button>", "0 context windows", "regenerate-assets?render=1&amp;scope=captions"} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q in rendered processing panel", want)
		}
	}
	if !strings.Contains(html, "Generate context") {
		t.Fatal("missing generate context control")
	}
}

func TestVideoProcessingRunningJobCannotRetry(t *testing.T) {
	var out bytes.Buffer
	err := VideoProcessing("video", []*db.MlJob{{Kind: "context_windows", Status: "processing", LockedBy: "worker-1"}}, nil, true, 12).Render(context.Background(), &out)
	if err != nil {
		t.Fatal(err)
	}
	html := out.String()
	if strings.Contains(html, ">Retry</button>") {
		t.Fatal("running job exposes retry")
	}
	if strings.Contains(html, "Generate captions") {
		t.Fatal("captions CTA should not be primary when a transcript exists")
	}
	for _, want := range []string{"Transcript available", "worker-1", "12 context windows", "context-windows/generate?render=1"} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q", want)
		}
	}
}

func TestGeneratedDataStatusCopy(t *testing.T) {
	t.Parallel()
	oom := `inference runtime unavailable: ollama chat HTTP 500 Internal Server Error: {"error":"llama runner process has terminated: error loading model: llama-server killed"}`
	cuda := `inference runtime unavailable: whisper.cpp needs a CUDA runtime (output=libcuda.so.1: cannot open shared object file)`

	cases := []struct {
		name          string
		jobs          []*db.MlJob
		health        []*db.MlRuntimeHealth
		hasTranscript bool
		windows       int
		want          []string
		not           []string
	}{
		{
			name:          "no transcript",
			hasTranscript: false,
			want:          []string{"No transcript yet. Generate captions first · 0 context windows"},
			not:           []string{"Transcript available", "blocked on the local model"},
		},
		{
			name:          "transcribe blocked as unhealthy queue",
			jobs:          []*db.MlJob{{Kind: "transcribe", Status: "queued", Attempts: 17, Priority: 10}},
			hasTranscript: false,
			want:          []string{"Captions are blocked on the transcription runtime", "waiting after retries", "Claimed 17 times", "not a healthy queue", "Generate captions"},
			not:           []string{"Captions · queued"},
		},
		{
			name:          "transcribe blocked on CUDA",
			jobs:          []*db.MlJob{{Kind: "transcribe", Status: "waiting_model", Attempts: 4, LastError: cuda}},
			health:        []*db.MlRuntimeHealth{{Kind: "transcribe", LastError: cuda}},
			hasTranscript: false,
			want:          []string{"Captions are blocked on the transcription runtime", "Whisper needs a CUDA runtime", "waiting for model", "Generate captions"},
			not:           []string{"Captions · queued"},
		},
		{
			name:          "context OOM is not dumped as JSON",
			jobs:          []*db.MlJob{{Kind: "context_windows", Status: "waiting_model", Attempts: 3, LastError: oom}},
			health:        []*db.MlRuntimeHealth{{Kind: "context_windows", LastError: oom}},
			hasTranscript: true,
			windows:       0,
			want:          []string{"Context windows are blocked on the local model", "The context model crashed, often from running out of GPU memory.", "llama-server killed"},
			not:           []string{`"error":"llama runner`, "context_windows runtime:"},
		},
		{
			name:          "existing windows are not described as a local-model block",
			jobs:          []*db.MlJob{{Kind: "context_windows", Status: "retry_wait", Attempts: 4, LastError: "workers-ai"}},
			hasTranscript: true,
			windows:       5,
			want:          []string{"Transcript available · 5 context windows"},
			not:           []string{"blocked on the local model", "Context generation failed"},
		},
		{
			name:          "workers chapters failure is not a local model block",
			jobs:          []*db.MlJob{{Kind: "context_windows", Status: "retry_wait", Attempts: 3, LastError: `chapters: 502 {"error":"bad chapters"}`}},
			hasTranscript: true,
			windows:       0,
			want:          []string{"Transcript available. Context generation failed · 0 context windows", "chapters: 502"},
			not:           []string{"blocked on the local model"},
		},
		{
			name:          "succeeded",
			jobs:          []*db.MlJob{{Kind: "context_windows", Status: "succeeded", Attempts: 1}},
			hasTranscript: true,
			windows:       8,
			want:          []string{"Transcript available · 8 context windows", "Context · succeeded"},
			not:           []string{"blocked", "Generate captions"},
		},
		{
			name:          "global context health hidden until transcript exists",
			jobs:          []*db.MlJob{{Kind: "transcribe", Status: "queued", Attempts: 17}},
			health:        []*db.MlRuntimeHealth{{Kind: "context_windows", LastError: oom}},
			hasTranscript: false,
			want:          []string{"Captions are blocked on the transcription runtime", "Generate captions"},
			not:           []string{"Context model runtime", "llama-server killed", "blocked on the local model"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			summary := generatedDataSummary(tc.jobs, tc.health, tc.hasTranscript, tc.windows)
			var out bytes.Buffer
			if err := VideoProcessing("video", tc.jobs, tc.health, tc.hasTranscript, tc.windows).Render(context.Background(), &out); err != nil {
				t.Fatal(err)
			}
			html := out.String()
			for _, want := range tc.want {
				if !strings.Contains(summary, want) && !strings.Contains(html, want) {
					t.Errorf("missing %q\nsummary=%s\nhtml=%s", want, summary, html)
				}
			}
			for _, not := range tc.not {
				if strings.Contains(summary, not) || strings.Contains(html, not) {
					t.Errorf("unexpected %q in summary=%q", not, summary)
				}
			}
		})
	}
}

func TestMLErrorCopyStripsJSON(t *testing.T) {
	got := mlErrorCopy(`inference runtime unavailable: ollama chat HTTP 500 Internal Server Error: {"error":"llama runner process has terminated: error loading model: llama-server killed"}`)
	if got.Headline != "The context model crashed, often from running out of GPU memory." {
		t.Fatalf("headline=%q", got.Headline)
	}
	if strings.Contains(got.Headline, "{") || strings.Contains(got.Detail, `"error"`) {
		t.Fatalf("json leaked: %+v", got)
	}
	if !strings.Contains(got.Detail, "llama-server killed") {
		t.Fatalf("detail=%q", got.Detail)
	}
	cuda := mlErrorCopy("whisper.cpp needs a CUDA runtime (output=libcuda.so.1: cannot open shared object file)")
	if cuda.Headline != "Whisper needs a CUDA runtime that isn't available." {
		t.Fatalf("cuda headline=%q", cuda.Headline)
	}
}

func TestMLJobStatusLabelUnhealthyQueue(t *testing.T) {
	if got := mlJobStatusLabel("queued", 17, ""); got != "waiting after retries" {
		t.Fatalf("got %q", got)
	}
	if got := mlJobStatusLabel("queued", 1, ""); got != "queued" {
		t.Fatalf("healthy queued got %q", got)
	}
	if got := mlJobStatusLabel("waiting_model", 17, "x"); got != "waiting for model" {
		t.Fatalf("got %q", got)
	}
}

func TestMLJobProgress(t *testing.T) {
	if got := mlJobProgress(nil); got != "" {
		t.Fatalf("empty: %q", got)
	}
	if got := mlJobProgress([]byte(`{"message":"Generating chunk 3/15 (cues c0120–c0400)","chunk":3,"chunks":15}`)); got != "Generating chunk 3/15 (cues c0120–c0400)" {
		t.Fatalf("got %q", got)
	}
	if got := mlJobProgress([]byte(`{"chunk":2,"chunks":8}`)); got != "Chunk 2/8" {
		t.Fatalf("got %q", got)
	}
}

func TestMLQueueSummary(t *testing.T) {
	got := mlQueueSummary([]*db.CountMLJobsRow{
		{Kind: "context_windows", Status: "queued", N: 12},
		{Kind: "context_windows", Status: "processing", N: 2},
		{Kind: "context_windows", Status: "cancelled", N: 3000},
		{Kind: "transcribe", Status: "waiting_model", N: 1},
	})
	if !strings.Contains(got, "12 queued") || !strings.Contains(got, "2 running") || !strings.Contains(got, "3000 cancelled") || !strings.Contains(got, "succeeded") {
		t.Fatalf("summary=%q", got)
	}
	if got := mlCountByKind([]*db.CountMLJobsRow{
		{Kind: "context_windows", Status: "queued", N: 12},
		{Kind: "context_windows", Status: "cancelled", N: 3000},
		{Kind: "context_windows", Status: "succeeded", N: 80},
	}, "context_windows"); got != 12 {
		t.Fatalf("live context count=%d", got)
	}
}

func TestMLJobsListClearNotice(t *testing.T) {
	var out bytes.Buffer
	err := MLJobsList(nil, nil, []*db.CountMLJobsRow{{Kind: "context_windows", Status: "cancelled", N: 12}}, "Cancelled 12 jobs.").Render(context.Background(), &out)
	if err != nil {
		t.Fatal(err)
	}
	html := out.String()
	for _, want := range []string{"Cancelled 12 jobs.", "Cancel queue", "12 cancelled", `id="ml-jobs-list"`} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q in %s", want, html)
		}
	}
}
