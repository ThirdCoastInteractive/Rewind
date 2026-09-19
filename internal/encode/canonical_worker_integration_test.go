//go:build integration

package encode

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"thirdcoast.systems/rewind/internal/application"
	"thirdcoast.systems/rewind/internal/config"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/stitch"
)

func TestCanonicalWorkerDisposableQueueAndFrame(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	p, err := application.OpenDBPoolWithRetry(ctx, config.Config{DatabaseDSN: "postgres://rewind_test:disposable-test-only@127.0.0.1:15439/rewind_test?sslmode=disable", DatabaseRetries: 1})
	if err != nil {
		t.Fatal(err)
	}
	d := &db.DatabaseConnection{Pool: p}
	if err := d.Migrate(ctx); err != nil {
		p.Close()
		t.Fatal(err)
	}
	owner := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	project := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	video := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	if _, err := p.Exec(ctx, "INSERT INTO users(id,user_name,email,password,enabled) VALUES($1,$2,$3,'fixture',true)", owner, owner.String(), owner.String()+"@worker-test"); err != nil {
		p.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = p.Exec(context.Background(), "DELETE FROM users WHERE id=$1", owner); p.Close() })
	if _, err := p.Exec(ctx, "INSERT INTO videos(id,src,archived_by,title) VALUES($1,$2,$3,'worker fixture')", video, "worker-fixture-"+video.String(), owner); err != nil {
		t.Fatal(err)
	}
	downloads, exports := t.TempDir(), t.TempDir()
	videoDir := filepath.Join(downloads, video.String())
	if err := os.MkdirAll(videoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(videoDir, video.String()+".video.mp4")
	ffmpeg := workerTool(t, "ffmpeg")
	workerRun(t, ffmpeg, "-hide_banner", "-loglevel", "error", "-y", "-f", "lavfi", "-i", "color=c=black:s=320x180:r=24:d=4", "-c:v", "libx264", "-pix_fmt", "yuv420p", input)
	workerProbe := workerTool(t, "ffprobe")
	t.Logf("source streams: %s", workerRun(t, workerProbe, "-v", "error", "-show_entries", "stream=codec_type,width,height", "-of", "json", input))
	doc := stitch.Document{Version: stitch.CurrentVersion, Title: "worker fixture", FPS: 24, Width: 160, Height: 90, Segments: []stitch.Segment{{ID: "video", Type: "video", VideoID: video.String(), StartUS: 0, SourceInUS: 0, DurationUS: 4_000_000}}, Overlays: []stitch.Overlay{{ID: "box", Kind: "rectangle", X: 10, Y: 10, Width: 20, Height: 20, Color: "#ff0000", Background: "#ff0000", Opacity: 1, StartUS: 0, EndUS: 4_000_000, Visible: true, Z: 1}}}
	docJSON, _ := json.Marshal(doc)
	if _, err := p.Exec(ctx, "INSERT INTO stitch_projects(id,created_by,title,segments,global_filters,document,document_version,revision,editor_enabled) VALUES($1,$2,$3,'[]','[]',$4,$5,0,true)", project, owner, doc.Title, docJSON, stitch.CurrentVersion); err != nil {
		t.Fatal(err)
	}
	// Clear only this fixture owner's stale queue so parallel disposable fixtures
	// cannot interfere with one another.
	if _, err := p.Exec(ctx, "DELETE FROM stitch_jobs WHERE created_by=$1 AND status='queued'", owner); err != nil {
		t.Fatal(err)
	}
	store := stitch.NewStore(d)
	opts := stitch.RenderOptions{Format: "mp4", Quality: "high", CaptionMode: "none", Scope: "range", StartUS: 1_000_000, EndUS: 2_500_000}
	job, err := store.QueueExport(ctx, owner, project, 0, "worker-export", opts)
	if err != nil {
		t.Fatal(err)
	}
	workerID := "worker-test"
	claimed, err := d.Queries(ctx).FindAndLockPendingStitchJob(ctx, &workerID)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.ID != job.ID {
		t.Fatalf("claimed unexpected job %s (wanted %s)", uuidString(claimed.ID), uuidString(job.ID))
	}
	probe := workerTool(t, "ffprobe")
	if err := processStitch(ctx, d.Queries(ctx), exports, downloads, claimed); err != nil {
		t.Fatal(err)
	}
	preProbe := workerRun(t, probe, "-v", "error", "-show_entries", "stream=codec_type", "-of", "csv=p=0", filepath.Join(exports, "stitch", uuidString(job.ID)+".mp4"))
	t.Logf("post-worker streams: %s", preProbe)
	got, err := store.GetRenderJob(ctx, owner, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "ready" || got.MIME != "video/mp4" || got.FilePath == "" {
		t.Fatalf("job=%+v", got)
	}
	if got.Progress != 100 {
		t.Fatalf("progress=%d", got.Progress)
	}
	raw := workerRun(t, probe, "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=width,height", "-of", "json", got.FilePath)
	duration := workerRun(t, probe, "-v", "error", "-show_entries", "format=duration", "-of", "default=nw=1:nk=1", got.FilePath)
	if !strings.Contains(raw, "160") || !strings.Contains(raw, "90") || !strings.Contains(duration, "1.") {
		t.Fatalf("probe=%s", raw)
	}
	frame, err := store.QueueFrame(ctx, owner, project, 0, "worker-frame", stitch.RenderOptions{Format: "mp4", Quality: "high", CaptionMode: "none", FrameTimeUS: 1_000_000})
	if err != nil {
		t.Fatal(err)
	}
	workerID = "worker-frame"
	claimed, err = d.Queries(ctx).FindAndLockPendingStitchJob(ctx, &workerID)
	if err != nil {
		t.Fatal(err)
	}
	if err := processStitch(ctx, d.Queries(ctx), exports, downloads, claimed); err != nil {
		t.Fatal(err)
	}
	frameJob, err := store.GetRenderJob(ctx, owner, frame.ID)
	if err != nil {
		t.Fatal(err)
	}
	if frameJob.Status != "ready" || frameJob.MIME != "image/png" || !strings.HasSuffix(frameJob.FilePath, ".png") {
		t.Fatalf("frame=%+v", frameJob)
	}
	f, err := os.Open(frameJob.FilePath)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(f)
	_ = f.Close()
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, _ := img.At(15, 15).RGBA()
	if r < 40000 || g > 12000 || b > 12000 {
		t.Fatalf("frame rectangle pixel=%d,%d,%d", r, g, b)
	}
}

// TestCanonicalWorkerCutsTransitionGapAndCaption exercises the complete
// persisted snapshot -> worker path. The sources and database rows are
// disposable fixtures; no archive rows are removed.
func TestCanonicalWorkerCutsTransitionGapAndCaption(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	p, err := application.OpenDBPoolWithRetry(ctx, config.Config{DatabaseDSN: "postgres://rewind_test:disposable-test-only@127.0.0.1:15439/rewind_test?sslmode=disable", DatabaseRetries: 1})
	if err != nil {
		t.Fatal(err)
	}
	dbconn := &db.DatabaseConnection{Pool: p}
	if err := dbconn.Migrate(ctx); err != nil {
		p.Close()
		t.Fatal(err)
	}
	owner, project := pgtype.UUID{Bytes: uuid.New(), Valid: true}, pgtype.UUID{Bytes: uuid.New(), Valid: true}
	red, blue := pgtype.UUID{Bytes: uuid.New(), Valid: true}, pgtype.UUID{Bytes: uuid.New(), Valid: true}
	if _, err := p.Exec(ctx, "INSERT INTO users(id,user_name,email,password,enabled) VALUES($1,$2,$3,'fixture',true)", owner, owner.String(), owner.String()+"@cut-test"); err != nil {
		p.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = p.Exec(context.Background(), "DELETE FROM users WHERE id=$1", owner); p.Close() })
	for _, v := range []struct {
		id         pgtype.UUID
		src, color string
	}{{red, "cut-red-" + red.String(), "red"}, {blue, "cut-blue-" + blue.String(), "blue"}} {
		if _, err := p.Exec(ctx, "INSERT INTO videos(id,src,archived_by,title) VALUES($1,$2,$3,$4)", v.id, v.src, owner, v.color); err != nil {
			t.Fatal(err)
		}
	}
	downloads, exports := t.TempDir(), t.TempDir()
	ffmpeg := workerTool(t, "ffmpeg")
	for _, v := range []struct {
		id    pgtype.UUID
		color string
	}{{red, "red"}, {blue, "blue"}} {
		dir := filepath.Join(downloads, v.id.String())
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, v.id.String()+".video.mp4")
		workerRun(t, ffmpeg, "-hide_banner", "-loglevel", "error", "-y", "-f", "lavfi", "-i", "color=c="+v.color+":s=320x180:r=24:d=4", "-c:v", "libx264", "-pix_fmt", "yuv420p", path)
	}
	doc := stitch.Document{Version: stitch.CurrentVersion, Title: "cuts", FPS: 24, Width: 160, Height: 90,
		Segments: []stitch.Segment{
			{ID: "red", Type: "video", VideoID: red.String(), StartUS: 0, SourceInUS: 0, DurationUS: 2_000_000},
			{ID: "blue", Type: "video", VideoID: blue.String(), StartUS: 1_500_000, SourceInUS: 0, DurationUS: 2_000_000, Transition: &stitch.Transition{Kind: "fade", DurationUS: 500_000}},
			{ID: "gap", Type: "gap", StartUS: 3_500_000, DurationUS: 1_000_000},
		},
		Captions: []stitch.Caption{{ID: "cue", SegmentID: "blue", Text: "BLUE", StartUS: 2_000_000, EndUS: 2_900_000, Alignment: "valid", Style: stitch.CaptionStyle{WordHighlight: true, Color: "#ffffff", HighlightColor: "#00ff00", FontSize: 24, X: 80, Y: 80}, Words: []stitch.Word{{Text: "BLUE", StartUS: 2_200_000, EndUS: 2_600_000}}}},
	}
	if err := stitch.Validate(doc); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(doc)
	if _, err := p.Exec(ctx, "INSERT INTO stitch_projects(id,created_by,title,segments,global_filters,document,document_version,revision,editor_enabled) VALUES($1,$2,$3,'[]','[]',$4,$5,0,true)", project, owner, doc.Title, b, stitch.CurrentVersion); err != nil {
		t.Fatal(err)
	}
	store := stitch.NewStore(dbconn)
	job, err := store.QueueExport(ctx, owner, project, 0, "cut-transition-gap", stitch.RenderOptions{Format: "mp4", Quality: "high", CaptionMode: "burn", Scope: "all", WordHighlight: true})
	if err != nil {
		t.Fatal(err)
	}
	workerID := "cut-transition-gap-worker"
	claimed, err := dbconn.Queries(ctx).FindAndLockPendingStitchJob(ctx, &workerID)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.ID != job.ID {
		t.Fatalf("claimed %s, wanted %s", uuidString(claimed.ID), uuidString(job.ID))
	}
	if err := processStitch(ctx, dbconn.Queries(ctx), exports, downloads, claimed); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetRenderJob(ctx, owner, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "ready" {
		t.Fatalf("job=%+v", got)
	}
	probe := workerTool(t, "ffprobe")
	var meta struct {
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
		Streams []struct {
			CodecType string `json:"codec_type"`
		} `json:"streams"`
	}
	probeJSON := workerRun(t, probe, "-v", "error", "-show_entries", "format=duration:stream=codec_type", "-of", "json", got.FilePath)
	if err := json.Unmarshal([]byte(probeJSON), &meta); err != nil {
		t.Fatal(err)
	}
	dur, err := strconv.ParseFloat(meta.Format.Duration, 64)
	if err != nil {
		t.Fatal(err)
	}
	if len(meta.Streams) == 0 || math.Abs(dur-4.5) > 1.0/24.0 {
		t.Fatalf("duration/streams=%s", probeJSON)
	}
	// Verify the solid portions and the explicit black gap. The fade sample
	// must be neither pure red nor pure blue.
	samples := []struct {
		at   float64
		want string
	}{{0.5, "red"}, {1.75, "fade"}, {3.0, "blue"}, {4.0, "black"}}
	for _, sample := range samples {
		pngPath := filepath.Join(t.TempDir(), fmt.Sprintf("sample-%v.png", sample.at))
		workerRun(t, ffmpeg, "-hide_banner", "-loglevel", "error", "-y", "-ss", fmt.Sprintf("%.3f", sample.at), "-i", got.FilePath, "-frames:v", "1", pngPath)
		f, err := os.Open(pngPath)
		if err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(f)
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
		r, g, b, _ := img.At(80, 45).RGBA()
		switch sample.want {
		case "red":
			if r < 40000 || g > 12000 || b > 12000 {
				t.Fatalf("at %.2fs pixel=%d,%d,%d", sample.at, r, g, b)
			}
		case "black":
			if r > 12000 || g > 12000 || b > 12000 {
				t.Fatalf("gap at %.2fs pixel=%d,%d,%d", sample.at, r, g, b)
			}
		case "fade":
			if r < 18000 || b < 18000 || g > 18000 {
				t.Fatalf("fade at %.2fs pixel=%d,%d,%d", sample.at, r, g, b)
			}
		case "blue":
			if b < 40000 || r > 16000 || g > 16000 {
				t.Fatalf("blue at %.2fs pixel=%d,%d,%d", sample.at, r, g, b)
			}
		}
	}
	// During the valid word interval the caption compositor must produce a
	// green highlighted word. During the preceding pause the ordinary white
	// caption remains visible without the highlight.
	sampleCaption := func(at string) image.Image {
		pngPath := filepath.Join(t.TempDir(), "caption-"+at+".png")
		workerRun(t, ffmpeg, "-hide_banner", "-loglevel", "error", "-y", "-ss", at, "-i", got.FilePath, "-frames:v", "1", pngPath)
		f, err := os.Open(pngPath)
		if err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(f)
		_ = f.Close()
		if err != nil {
			t.Fatal(err)
		}
		return img
	}
	paused := sampleCaption("2.10")
	active := sampleCaption("2.35")
	var pausedWhite, activeGreen bool
	for y := active.Bounds().Dy() / 2; y < active.Bounds().Dy(); y++ {
		for x := 0; x < active.Bounds().Dx(); x++ {
			r, g, b, _ := active.At(x, y).RGBA()
			if g > r+12000 && g > b+12000 {
				activeGreen = true
			}
			pr, pg, pb, _ := paused.At(x, y).RGBA()
			if pr > 45000 && pg > 45000 && pb > 45000 {
				pausedWhite = true
			}
		}
	}
	if !activeGreen {
		t.Fatal("active word did not produce green highlight")
	}
	if !pausedWhite {
		t.Fatal("caption was not visible during word pause")
	}
}

func workerTool(t *testing.T, name string) string {
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	p := filepath.Join(`C:\bin`, name+".exe")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("missing %s: %v", name, err)
	}
	return p
}
func workerRun(t *testing.T, path string, args ...string) string {
	out, err := exec.Command(path, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("%s: %v\n%s", path, err, out)
	}
	return string(out)
}
