package textcls

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/pkg/plugin"
)

type fakeML struct {
	jobs []plugin.Job
}

func (f *fakeML) Enqueue(_ context.Context, job plugin.Job) (string, error) {
	f.jobs = append(f.jobs, job)
	return "job", nil
}

func TestEnqueueCommentClassifyJob(t *testing.T) {
	f := &fakeML{}
	plugin.Use(plugin.Set{ML: f})
	var id pgtype.UUID
	_ = id.Scan("33333333-3333-3333-3333-333333333333")
	if err := EnqueueCommentClassifyJob(context.Background(), id, "h1", "d1"); err != nil {
		t.Fatal(err)
	}
	if len(f.jobs) != 1 {
		t.Fatalf("jobs=%d", len(f.jobs))
	}
	if f.jobs[0].Kind != plugin.KindClassify {
		t.Fatalf("kind %s", f.jobs[0].Kind)
	}
	if f.jobs[0].PromptVersion != PromptVersion || f.jobs[0].Priority != Priority {
		t.Fatalf("comment_classify meta %+v", f.jobs[0])
	}
}
