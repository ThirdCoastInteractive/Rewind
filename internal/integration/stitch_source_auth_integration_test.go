//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"
	"thirdcoast.systems/rewind/internal/stitch"
	"thirdcoast.systems/rewind/pkg/plugin"
)

type integrationScopeAuth struct{ tenant string }

func (a *integrationScopeAuth) WorkspaceForUser(context.Context, string) (string, error) {
	return a.tenant, nil
}
func (*integrationScopeAuth) Current(*http.Request) (*plugin.Actor, error)      { return nil, nil }
func (*integrationScopeAuth) Login(http.ResponseWriter, *http.Request) error    { return nil }
func (*integrationScopeAuth) Logout(http.ResponseWriter, *http.Request) error   { return nil }
func (*integrationScopeAuth) Register(http.ResponseWriter, *http.Request) error { return nil }
func (*integrationScopeAuth) LoginPath() string                                 { return "/login" }
func (*integrationScopeAuth) Mount(*echo.Echo)                                  {}

type integrationScopeLive struct{}

func (*integrationScopeLive) CreateInput(context.Context, *plugin.Actor, string) (*plugin.LiveInput, error) {
	return nil, nil
}
func (*integrationScopeLive) GetInput(context.Context, *plugin.Actor, string) (*plugin.LiveInput, error) {
	return nil, nil
}
func (*integrationScopeLive) ListInputs(context.Context, *plugin.Actor) ([]*plugin.LiveInput, error) {
	return nil, nil
}
func (*integrationScopeLive) DeleteInput(context.Context, *plugin.Actor, string) error { return nil }
func (*integrationScopeLive) AddOutput(context.Context, *plugin.Actor, string, string, string) (*plugin.LiveOutput, error) {
	return nil, nil
}
func (*integrationScopeLive) RemoveOutput(context.Context, *plugin.Actor, string, string) error {
	return nil
}
func (*integrationScopeLive) EnableOutput(context.Context, *plugin.Actor, string, string, bool) error {
	return nil
}
func (*integrationScopeLive) Mount(*echo.Echo) {}

func enableIntegrationLive(t *testing.T, tenant pgtype.UUID) {
	t.Helper()
	plugin.Reset()
	plugin.Use(plugin.Set{Authn: &integrationScopeAuth{tenant: tenant.String()}, Live: &integrationScopeLive{}})
	t.Cleanup(plugin.Reset)
}

func TestStitchStoreRejectsForeignVideoSource(t *testing.T) {
	d, owner, project := openStitchTest(t)
	ctx := context.Background()
	tenant := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	foreign := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	video := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	if _, err := d.Exec(ctx, `INSERT INTO videos(id,src,archived_by,title,duration_seconds,tenant_id) VALUES($1,$2,$3,'foreign',10,$4)`, video, "fixture-"+video.String(), owner, foreign); err != nil {
		t.Fatal(err)
	}
	enableIntegrationLive(t, tenant)
	s := stitch.NewStore(d)
	if _, err := s.Enable(ctx, owner, project); err != nil {
		t.Fatal(err)
	}
	_, err := s.Commit(ctx, owner, project, 0, "foreign-video", stitch.Actor{Kind: "user", ID: owner.String()}, "insert", []stitch.Operation{{Type: "insert_segment", Segment: &stitch.Segment{ID: "foreign", Type: "video", VideoID: video.String(), DurationUS: 1_000_000}}})
	if err == nil {
		t.Fatal("foreign video source was accepted")
	}
}

func TestStitchStoreAllowsSameTenantTeammateClip(t *testing.T) {
	d, owner, project := openStitchTest(t)
	ctx := context.Background()
	tenant := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	teammate := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	video := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	clip := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	if _, err := d.Exec(ctx, `INSERT INTO users(id,user_name,email,password,enabled) VALUES($1,$2,$3,'fixture',true)`, teammate, teammate.String(), teammate.String()+"@test"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(ctx, `INSERT INTO videos(id,src,archived_by,title,duration_seconds,tenant_id) VALUES($1,$2,$3,'shared',10,$4)`, video, "fixture-"+video.String(), owner, tenant); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(ctx, `INSERT INTO clips(id,video_id,start_ts,end_ts,duration,created_by,title) VALUES($1,$2,0,2,2,$3,'teammate')`, clip, video, teammate); err != nil {
		t.Fatal(err)
	}
	enableIntegrationLive(t, tenant)
	s := stitch.NewStore(d)
	if _, err := s.Enable(ctx, owner, project); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Commit(ctx, owner, project, 0, "teammate-clip", stitch.Actor{Kind: "user", ID: owner.String()}, "insert", []stitch.Operation{{Type: "insert_segment", Segment: &stitch.Segment{ID: "shared", Type: "clip", ClipID: clip.String(), SourceInUS: 0, DurationUS: 1_000_000}}}); err != nil {
		t.Fatalf("same-tenant teammate clip rejected: %v", err)
	}
}

func TestStitchStoreRejectsNestedExportAfterSourceMovesTenant(t *testing.T) {
	d, owner, project := openStitchTest(t)
	ctx := context.Background()
	tenant := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	foreign := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	video := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	job := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	if _, err := d.Exec(ctx, `INSERT INTO videos(id,src,archived_by,title,duration_seconds,tenant_id) VALUES($1,$2,$3,'nested',10,$4)`, video, "fixture-"+video.String(), owner, tenant); err != nil {
		t.Fatal(err)
	}
	snapshot, err := json.Marshal(stitch.RenderSnapshot{Document: stitch.Document{Segments: []stitch.Segment{{ID: "nested-source", Type: "video", VideoID: video.String(), DurationUS: 1_000_000}}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(ctx, `INSERT INTO stitch_jobs(id,created_by,title,segments,document_snapshot,project_id) VALUES($1,$2,'nested','[]',$3,$4)`, job, owner, snapshot, project); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(ctx, `UPDATE videos SET tenant_id=$1 WHERE id=$2`, foreign, video); err != nil {
		t.Fatal(err)
	}
	enableIntegrationLive(t, tenant)
	s := stitch.NewStore(d)
	if _, err := s.Enable(ctx, owner, project); err != nil {
		t.Fatal(err)
	}
	_, err = s.Commit(ctx, owner, project, 0, "nested-moved", stitch.Actor{Kind: "user", ID: owner.String()}, "insert", []stitch.Operation{{Type: "insert_segment", Segment: &stitch.Segment{ID: "nested", Type: "video", ExportJobID: job.String(), DurationUS: 1_000_000}}})
	if err == nil {
		t.Fatal("nested export with moved source was accepted")
	}
}

func TestStitchStoreRejectsLegacyClipNestedExportAfterSourceMovesTenant(t *testing.T) {
	d, owner, project := openStitchTest(t)
	ctx := context.Background()
	tenant := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	foreign := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	video := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	clip := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	job := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	if _, err := d.Exec(ctx, `INSERT INTO videos(id,src,archived_by,title,duration_seconds,tenant_id) VALUES($1,$2,$3,'legacy-nested',10,$4)`, video, "fixture-"+video.String(), owner, tenant); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(ctx, `INSERT INTO clips(id,video_id,start_ts,end_ts,duration,created_by,title) VALUES($1,$2,0,2,2,$3,'legacy clip')`, clip, video, owner); err != nil {
		t.Fatal(err)
	}
	legacy, err := json.Marshal([]map[string]any{{"id": "legacy-segment", "type": "clip", "clip_id": clip.String(), "legacy_metadata": map[string]any{"video_id": video.String(), "start_us": 0, "duration_us": 1000000, "end_ts": 2}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(ctx, `INSERT INTO stitch_jobs(id,created_by,title,segments) VALUES($1,$2,'legacy nested',$3)`, job, owner, legacy); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(ctx, `UPDATE videos SET tenant_id=$1 WHERE id=$2`, foreign, video); err != nil {
		t.Fatal(err)
	}
	enableIntegrationLive(t, tenant)
	s := stitch.NewStore(d)
	if _, err := s.Enable(ctx, owner, project); err != nil {
		t.Fatal(err)
	}
	_, err = s.Commit(ctx, owner, project, 0, "legacy-nested-moved", stitch.Actor{Kind: "user", ID: owner.String()}, "insert", []stitch.Operation{{Type: "insert_segment", Segment: &stitch.Segment{ID: "nested", Type: "video", ExportJobID: job.String(), DurationUS: 1_000_000}}})
	if err == nil {
		t.Fatal("legacy clip-only nested export with moved source was accepted")
	}
}
