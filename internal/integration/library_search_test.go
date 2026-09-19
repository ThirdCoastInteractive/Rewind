//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/internal/application"
	"thirdcoast.systems/rewind/internal/config"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/search"
)

func TestLibraryScopedSearch(t *testing.T) {
	ctx := context.Background()
	pool, err := application.OpenDBPoolWithRetry(ctx, config.Config{DatabaseDSN: "postgres://rewind_test:disposable-test-only@127.0.0.1:15439/rewind_test?sslmode=disable", DatabaseRetries: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	dbc := &db.DatabaseConnection{Pool: pool}
	if err := dbc.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	q := dbc.Queries(ctx).WithTx(tx)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	user := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	video := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	uploader := uuid.NewString()
	exec("INSERT INTO users(id,user_name,email,password) VALUES($1,$2,$2,'fixture')", user, user.String())
	exec(`INSERT INTO videos(id,src,archived_by,title,uploader,media,video_path,duration_seconds,assets_status) VALUES($1,'https://example.com/watch?v=literal_%',$2,'Interview with Redbar',$3,'video','/fixture.mp4',120,'{"waveform":true,"preview":false}')`, video, user, uploader)
	exec(`INSERT INTO video_transcripts(video_id,lang,format,text,search,raw,cues) VALUES($1,'en','vtt','Redbar is watching. Title only is absent.',to_tsvector('simple','Redbar is watching. Title only is absent.'),'','[]')`, video)
	window, err := q.CreateContextWindow(ctx, &db.CreateContextWindowParams{VideoID: video, StartTs: 0, EndTs: 10, Title: "Comedy discussion", Summary: "A live interview", Topics: []string{}, Entities: []string{}, Origin: "mcp", TranscriptCueEvidence: []byte("[]"), BoundaryQuality: "cue", CreatedBy: user})
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		query  string
		assets []string
		want   int
	}{
		{"", nil, 1},
		{`interview intranscript:"Redbar is watching"`, nil, 1},
		{`unfindable intranscript:"Redbar is watching"`, nil, 0},
		{`intranscript:Redbar is watching`, nil, 1},
		{`intranscript:"Redbar watching"`, nil, 0},
		{`intranscript:Interview with Redbar`, nil, 0},
		{`intitle:"Interview" intranscript:"Redbar is watching"`, nil, 1},
		{`intitle:"missing" intranscript:"Redbar is watching"`, nil, 0},
		{`incontext:"Comedy discussion"`, nil, 1},
		{`inurl:"literal_%"`, nil, 1},
		{`inurl:"literal__"`, nil, 0},
		{`intitle:""`, nil, 0},
		{`intranscript:"' & ! |"`, nil, 0},
		{"", []string{"context", "transcript", "waveform"}, 1},
		{"", []string{"context", "preview"}, 0},
		{"", []string{"seek"}, 0},
	} {
		t.Run(tt.query+":"+strings.Join(tt.assets, ","), func(t *testing.T) {
			compiled := search.ParseLibrary(tt.query)
			rows, err := q.ListVideosPaginated(ctx, &db.ListVideosPaginatedParams{Uploader: &uploader, Query: &compiled.General.Raw, Tsquery: &compiled.General.TSQuery, FieldClauses: compiled.FieldsJSON(), RequiredAssets: tt.assets, SortOrder: "newest", PageLimit: 24})
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != tt.want {
				t.Fatalf("got %d rows, want %d", len(rows), tt.want)
			}
		})
	}
	exec("UPDATE context_windows SET stale=true WHERE id=$1", window.ID)
	rows, err := q.ListVideosPaginated(ctx, &db.ListVideosPaginatedParams{Uploader: &uploader, RequiredAssets: []string{"context"}, SortOrder: "newest", PageLimit: 24})
	if err != nil || len(rows) != 0 {
		t.Fatalf("stale context matched: %d, %v", len(rows), err)
	}
}
