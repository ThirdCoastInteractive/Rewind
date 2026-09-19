package ingest

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgtype"
	"os"
	"path/filepath"
	"testing"
	"thirdcoast.systems/rewind/internal/db"
)

type mediaPublisherFunc func(context.Context, *db.PublishVideoMediaParams) (int64, error)

func (f mediaPublisherFunc) PublishVideoMedia(c context.Context, p *db.PublishVideoMediaParams) (int64, error) {
	return f(c, p)
}
func TestPublishIngestMedia(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source.mp4")
	if err := os.WriteFile(path, []byte("stored-media"), 0600); err != nil {
		t.Fatal(err)
	}
	var called bool
	q := mediaPublisherFunc(func(_ context.Context, p *db.PublishVideoMediaParams) (int64, error) {
		called = true
		if *p.VideoPath != path || *p.FileSize != 12 {
			t.Fatalf("wrong publication: %+v", p)
		}
		return 1, nil
	})
	if err := publishIngestMedia(context.Background(), q, pgtype.UUID{}, path, nil, nil); err != nil || !called {
		t.Fatalf("publication failed: %v", err)
	}
	called = false
	if err := publishIngestMedia(context.Background(), q, pgtype.UUID{}, path+".missing", nil, nil); err == nil || called {
		t.Fatal("missing source published")
	}
	want := errors.New("database unavailable")
	fail := mediaPublisherFunc(func(context.Context, *db.PublishVideoMediaParams) (int64, error) { return 0, want })
	if err := publishIngestMedia(context.Background(), fail, pgtype.UUID{}, path, nil, nil); !errors.Is(err, want) {
		t.Fatalf("lost database failure: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("source file was not preserved")
	}
}
