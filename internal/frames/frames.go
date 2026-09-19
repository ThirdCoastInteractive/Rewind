// Package frames resolves timestamped archive images without changing media assets.
package frames

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	_ "image/png"
	"math"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"thirdcoast.systems/rewind/pkg/plugin"
	"time"

	"github.com/google/uuid"
)

// Asset is the authorized archive record to inspect; paths never come from tool arguments.
type Asset struct {
	ID, Directory, File string
	Duration            float64
}

// Frame includes provenance as well as bytes for transports that can deliver images.
type Frame struct {
	Requested    float64    `json:"requested_timestamp"`
	Sample       float64    `json:"sample_timestamp"`
	Actual       *float64   `json:"actual_timestamp"`
	Interval     [2]float64 `json:"source_interval"`
	Width        int        `json:"width"`
	Height       int        `json:"height"`
	Source       string     `json:"source"`
	Coverage     string     `json:"spatial_coverage"`
	Reference    string     `json:"frame_ref"`
	WebPath      string     `json:"web_path"`
	ContentIndex int        `json:"content_index,omitempty"`
	Error        string     `json:"error,omitempty"`
	JPEG         []byte     `json:"-"`
}

// Service bounds decoding concurrency and caches disposable frame results in memory.
type Service struct {
	slots   chan struct{}
	mu      sync.Mutex
	cache   map[string]cached
	flights map[string]chan struct{}
	size    int
}
type cached struct {
	frame Frame
	at    time.Time
}

// New creates a service with two decoder slots and a one-GiB, 24-hour cache.
func New() *Service {
	return &Service{slots: make(chan struct{}, 2), cache: map[string]cached{}, flights: map[string]chan struct{}{}}
}

// Default is shared by HTTP and MCP requests in the web process.
var Default = New()

// ErrMediaUnavailable means no archived media file exists; inspection never downloads it.
var ErrMediaUnavailable = errors.New("media_unavailable")

// Resolve finds an existing media file and asset directory beneath the configured archive.
func Resolve(id, storedPath string, duration float64) (Asset, error) {
	if _, err := uuid.Parse(id); err != nil {
		return Asset{}, fmt.Errorf("invalid video UUID")
	}
	root, ok := plugin.LocalRoot()
	if !ok {
		root = os.Getenv("DOWNLOADS_DIR")
		if root == "" {
			root = "/downloads"
		}
	}
	dir := filepath.Join(root, id)
	if p, found := plugin.LocalMaster(id); found {
		return Asset{ID: id, Directory: filepath.Dir(p), File: p, Duration: duration}, nil
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		if st, e := os.Stat(filepath.Join("/download", id)); e == nil && st.IsDir() {
			dir = filepath.Join("/download", id)
		}
	}
	a := Asset{ID: id, Directory: dir, Duration: duration}
	if storedPath != "" {
		if st, e := os.Stat(storedPath); e == nil && st.Mode().IsRegular() {
			a.File = storedPath
			a.Directory = filepath.Dir(storedPath)
			return a, nil
		}
	}
	for _, ext := range []string{".mp4", ".webm", ".mkv", ".mov", ".avi"} {
		p := filepath.Join(dir, id+".video"+ext)
		if st, e := os.Stat(p); e == nil && st.Mode().IsRegular() {
			a.File = p
			break
		}
	}
	return a, nil
}

// Fingerprint changes when the source file or seek manifest is replaced.
func Fingerprint(a Asset) string {
	h := sha256.New()
	fmt.Fprint(h, a.ID)
	paths := []string{a.File}
	_ = filepath.WalkDir(filepath.Join(a.Directory, "seek"), func(path string, entry os.DirEntry, err error) error {
		if err == nil && !entry.IsDir() && entry.Type()&os.ModeSymlink == 0 {
			paths = append(paths, path)
		}
		return nil
	})
	for _, p := range paths {
		if st, e := os.Stat(p); e == nil {
			fmt.Fprintf(h, "|%s:%d:%d", p, st.Size(), st.ModTime().UnixNano())
		}
	}
	return hex.EncodeToString(h.Sum(nil))[:24]
}

// ParseReference validates a Rewind frame URI without trusting filesystem paths in it.
func ParseReference(ref string) (string, float64, string, string, error) {
	u, e := url.Parse(ref)
	if e != nil || u.Scheme != "rewind" || u.Host != "video" {
		return "", 0, "", "", fmt.Errorf("invalid frame reference")
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 2 || parts[1] != "frame" {
		return "", 0, "", "", fmt.Errorf("invalid frame reference")
	}
	if _, e = uuid.Parse(parts[0]); e != nil {
		return "", 0, "", "", e
	}
	t, e := strconv.ParseFloat(u.Query().Get("t"), 64)
	if e != nil || !finite(t) || t < 0 {
		return "", 0, "", "", fmt.Errorf("invalid frame timestamp")
	}
	q := u.Query().Get("quality")
	if q != "preview" && q != "detail" {
		return "", 0, "", "", fmt.Errorf("invalid frame quality")
	}
	return parts[0], t, q, u.Query().Get("asset"), nil
}
func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

// Get returns individual frames, preserving request order and per-frame failures.
func (s *Service) Get(ctx context.Context, a Asset, times []float64, quality string, width int) ([]Frame, error) {
	if len(times) == 0 || len(times) > 32 {
		return nil, fmt.Errorf("request 1–32 frames")
	}
	if quality == "" {
		quality = "detail"
	}
	if quality != "detail" && quality != "preview" {
		return nil, fmt.Errorf("quality must be preview or detail")
	}
	if width == 0 {
		width = 960
	}
	if width < 32 || width > 1920 {
		return nil, fmt.Errorf("max_width must be 32–1920")
	}
	for _, t := range times {
		if !finite(t) || t < 0 || (a.Duration > 0 && t >= a.Duration) {
			return nil, fmt.Errorf("timestamp outside video duration")
		}
	}
	var cues []tile
	var err error
	if quality == "preview" {
		cues, err = readTiles(a)
		if err != nil {
			return nil, err
		}
	}
	decoded := map[string]image.Image{}
	out := make([]Frame, 0, len(times))
	fp := Fingerprint(a)
	for _, t := range times {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		key := fmt.Sprintf("%s/%s/%.6f/%d", fp, quality, t, width)
		f, e := s.cached(ctx, key, func() (Frame, error) {
			if quality == "preview" {
				return preview(t, cues, decoded)
			}
			return s.detail(ctx, a, t, width)
		})
		f.Requested = t
		f.WebPath = fmt.Sprintf("/videos/%s?t=%.3f", a.ID, t)
		f.Reference = fmt.Sprintf("rewind://video/%s/frame?t=%.6f&quality=%s&asset=%s", a.ID, t, quality, fp)
		if e != nil {
			f.Error = e.Error()
		}
		out = append(out, f)
	}
	return out, nil
}
func (s *Service) cached(ctx context.Context, key string, fn func() (Frame, error)) (Frame, error) {
	for {
		s.mu.Lock()
		if c, ok := s.cache[key]; ok && time.Since(c.at) < 24*time.Hour {
			s.mu.Unlock()
			return c.frame, nil
		}
		if ch, ok := s.flights[key]; ok {
			s.mu.Unlock()
			select {
			case <-ctx.Done():
				return Frame{}, ctx.Err()
			case <-ch:
				continue
			}
		}
		ch := make(chan struct{})
		s.flights[key] = ch
		s.mu.Unlock()
		f, e := fn()
		s.mu.Lock()
		if e == nil {
			if old, ok := s.cache[key]; ok {
				s.size -= len(old.frame.JPEG)
			}
			s.cache[key] = cached{f, time.Now()}
			s.size += len(f.JPEG)
			for k, c := range s.cache {
				if s.size <= 1<<30 && time.Since(c.at) < 24*time.Hour {
					continue
				}
				delete(s.cache, k)
				s.size -= len(c.frame.JPEG)
			}
		}
		delete(s.flights, key)
		close(ch)
		s.mu.Unlock()
		return f, e
	}
}

type level struct {
	Name     string  `json:"name"`
	Interval float64 `json:"interval_seconds"`
	Width    int     `json:"thumb_width"`
	Height   int     `json:"thumb_height"`
	VTT      string  `json:"vtt_path"`
}
type tile struct {
	start, end float64
	path       string
	rect       image.Rectangle
}

func inside(root, rel string) (string, error) {
	if filepath.IsAbs(rel) || strings.Contains(rel, ":") {
		return "", fmt.Errorf("invalid asset path")
	}
	base, e := filepath.Abs(root)
	if e != nil {
		return "", e
	}
	p := filepath.Join(base, filepath.FromSlash(rel))
	r, e := filepath.Rel(base, p)
	if e != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("asset path escapes video directory")
	}
	// Inspect only descendants of the trusted archive root. Resolving all ancestors
	// needlessly requires access to parent directories on sandboxed Windows hosts.
	current := base
	for _, component := range strings.Split(r, string(filepath.Separator)) {
		current = filepath.Join(current, component)
		st, err := os.Lstat(current)
		if err != nil {
			return "", err
		}
		if st.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("symlinked seek assets are not supported")
		}
	}
	return p, nil
}
func seconds(s string) (float64, error) {
	p := strings.Split(s, ":")
	if len(p) != 3 {
		return 0, fmt.Errorf("invalid VTT timestamp")
	}
	h, e := strconv.ParseFloat(p[0], 64)
	if e != nil {
		return 0, e
	}
	m, e := strconv.ParseFloat(p[1], 64)
	if e != nil {
		return 0, e
	}
	v, e := strconv.ParseFloat(p[2], 64)
	if e != nil || !finite(v) || h < 0 || m < 0 || m >= 60 || v < 0 || v >= 60 {
		return 0, fmt.Errorf("invalid VTT timestamp")
	}
	return h*3600 + m*60 + v, nil
}
func levelScore(l level) int {
	score := 0
	if l.Interval > 0 && l.Interval <= 1.01 {
		score += 100
	}
	if l.Width == 160 && l.Height == 90 {
		score += 50
	}
	if l.Name == "fine" {
		score += 10
	}
	return score
}

func readTiles(a Asset) ([]tile, error) {
	root := filepath.Join(a.Directory, "seek")
	raw, e := os.ReadFile(filepath.Join(root, "seek.json"))
	if e != nil {
		return nil, fmt.Errorf("waiting_assets: %w", e)
	}
	var manifest struct {
		Format string  `json:"format"`
		Levels []level `json:"levels"`
	}
	if e = json.Unmarshal(raw, &manifest); e != nil {
		return nil, e
	}
	if manifest.Format != "rewind-seek-v1" {
		return nil, fmt.Errorf("unsupported seek manifest")
	}
	sort.SliceStable(manifest.Levels, func(i, j int) bool {
		a, b := manifest.Levels[i], manifest.Levels[j]
		as, bs := levelScore(a), levelScore(b)
		if as != bs {
			return as > bs
		}
		if a.Width*a.Height != b.Width*b.Height {
			return a.Width*a.Height > b.Width*b.Height
		}
		return a.Interval < b.Interval
	})
	for _, l := range manifest.Levels {
		p, e := inside(root, l.VTT)
		if e != nil {
			return nil, fmt.Errorf("seek VTT path: %w", e)
		}
		raw, e := os.ReadFile(p)
		if e != nil {
			continue
		}
		var out []tile
		scan := bufio.NewScanner(bytes.NewReader(raw))
		for scan.Scan() {
			line := scan.Text()
			if !strings.Contains(line, " --> ") {
				continue
			}
			p := strings.Split(line, " --> ")
			start, e := seconds(strings.TrimSpace(p[0]))
			if e != nil {
				return nil, e
			}
			end, e := seconds(strings.Fields(p[1])[0])
			if e != nil || end <= start {
				return nil, fmt.Errorf("invalid VTT interval")
			}
			if !scan.Scan() {
				return nil, fmt.Errorf("missing VTT image")
			}
			ref := strings.Split(strings.TrimSpace(scan.Text()), "#xywh=")
			if len(ref) != 2 {
				return nil, fmt.Errorf("invalid VTT image rectangle")
			}
			path, e := inside(root, filepath.Join(filepath.Dir(l.VTT), ref[0]))
			if e != nil {
				return nil, e
			}
			var x, y, w, h int
			if _, e = fmt.Sscanf(ref[1], "%d,%d,%d,%d", &x, &y, &w, &h); e != nil || x < 0 || y < 0 || w <= 0 || h <= 0 {
				return nil, fmt.Errorf("invalid VTT rectangle")
			}
			out = append(out, tile{start, end, path, image.Rect(x, y, x+w, y+h)})
		}
		if e = scan.Err(); e != nil {
			return nil, e
		}
		if len(out) > 0 {
			return out, nil
		}
	}
	return nil, fmt.Errorf("waiting_assets: no usable seek level")
}
func preview(t float64, cues []tile, decoded map[string]image.Image) (Frame, error) {
	i := sort.Search(len(cues), func(i int) bool { return cues[i].end > t })
	if i == len(cues) || t < cues[i].start {
		return Frame{}, fmt.Errorf("no sprite covers timestamp")
	}
	c := cues[i]
	img := decoded[c.path]
	if img == nil {
		f, e := os.Open(c.path)
		if e != nil {
			return Frame{}, e
		}
		defer f.Close()
		cfg, _, e := image.DecodeConfig(f)
		if e != nil || cfg.Width*cfg.Height > 64_000_000 {
			return Frame{}, fmt.Errorf("invalid or oversized sprite")
		}
		if _, e = f.Seek(0, 0); e != nil {
			return Frame{}, e
		}
		img, _, e = image.Decode(f)
		if e != nil {
			return Frame{}, e
		}
		decoded[c.path] = img
	}
	if !c.rect.In(img.Bounds()) {
		return Frame{}, fmt.Errorf("sprite rectangle outside image")
	}
	dst := image.NewRGBA(image.Rect(0, 0, c.rect.Dx(), c.rect.Dy()))
	draw.Draw(dst, dst.Bounds(), img, c.rect.Min, draw.Src)
	var b bytes.Buffer
	if e := jpeg.Encode(&b, dst, &jpeg.Options{Quality: 90}); e != nil {
		return Frame{}, e
	}
	return Frame{Sample: c.start, Interval: [2]float64{c.start, c.end}, Width: c.rect.Dx(), Height: c.rect.Dy(), Source: "seek_sprite", Coverage: "center_crop", JPEG: b.Bytes()}, nil
}

var ptsPattern = regexp.MustCompile(`pts_time:([\-0-9.e+]+)`)

func (s *Service) detail(ctx context.Context, a Asset, t float64, width int) (Frame, error) {
	if a.File == "" {
		return Frame{}, ErrMediaUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	case <-ctx.Done():
		return Frame{}, ctx.Err()
	}
	// -copyts retains source PTS; -start_at_zero maps a nonzero container origin to player time.
	filter := fmt.Sprintf("select='gte(t,%.9f)',scale='min(%d,iw)':-2,showinfo", t, width)
	args := []string{"-hide_banner", "-nostdin", "-loglevel", "info", "-ss", fmt.Sprintf("%.9f", math.Max(0, t-2)), "-copyts", "-start_at_zero", "-i", a.File, "-map", "0:v:0", "-vf", filter, "-frames:v", "1", "-vsync", "0", "-f", "image2pipe", "-vcodec", "mjpeg", "-q:v", "3", "pipe:1"}
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	raw, e := cmd.Output()
	if e != nil {
		if ctx.Err() != nil {
			return Frame{}, ctx.Err()
		}
		return Frame{}, fmt.Errorf("frame decode: %w: %.1200s", e, stderr.String())
	}
	cfg, _, e := image.DecodeConfig(bytes.NewReader(raw))
	if e != nil {
		return Frame{}, fmt.Errorf("frame decode returned no image: %w", e)
	}
	matches := ptsPattern.FindStringSubmatch(stderr.String())
	if len(matches) != 2 {
		return Frame{}, fmt.Errorf("decoder did not report frame timestamp")
	}
	actual, e := strconv.ParseFloat(matches[1], 64)
	if e != nil || actual+0.001 < t {
		return Frame{}, fmt.Errorf("decoder returned invalid timestamp")
	}
	return Frame{Sample: actual, Actual: &actual, Interval: [2]float64{actual, actual}, Width: cfg.Width, Height: cfg.Height, Source: "video_frame", Coverage: "full_frame", JPEG: raw}, nil
}

// ContactSheet draws timestamped, numbered frames into a four-column JPEG.
func ContactSheet(frames []Frame, detail bool) ([]byte, []Frame, error) {
	w, h := 160, 90
	if detail {
		w, h = 320, 180
	}
	seen := map[string]bool{}
	cells := []Frame{}
	for _, f := range frames {
		if f.Error != "" {
			cells = append(cells, f)
			continue
		}
		key := fmt.Sprintf("%s/%.6f", f.Source, f.Sample)
		if !seen[key] {
			cells = append(cells, f)
			seen[key] = true
		}
	}
	if len(cells) == 0 {
		return nil, nil, fmt.Errorf("no contact sheet frames")
	}
	dst := image.NewRGBA(image.Rect(0, 0, w*4, ((len(cells)+3)/4)*(h+20)))
	draw.Draw(dst, dst.Bounds(), image.NewUniform(color.Black), image.Point{}, draw.Src)
	for i, f := range cells {
		x, y := (i%4)*w, (i/4)*(h+20)
		if f.Error == "" {
			img, _, e := image.Decode(bytes.NewReader(f.JPEG))
			if e != nil {
				return nil, nil, e
			}
			b := img.Bounds()
			scale := math.Min(float64(w)/float64(b.Dx()), float64(h)/float64(b.Dy()))
			dw, dh := int(float64(b.Dx())*scale), int(float64(b.Dy())*scale)
			for yy := 0; yy < dh; yy++ {
				for xx := 0; xx < dw; xx++ {
					dst.Set(x+(w-dw)/2+xx, y+(h-dh)/2+yy, img.At(b.Min.X+xx*b.Dx()/dw, b.Min.Y+yy*b.Dy()/dh))
				}
			}
		}
		label(dst, x+4, y+h+3, fmt.Sprintf("%d %.3f", i+1, f.Sample))
		cells[i].JPEG = nil
		cells[i].ContentIndex = 1
	}
	var b bytes.Buffer
	e := jpeg.Encode(&b, dst, &jpeg.Options{Quality: 85})
	return b.Bytes(), cells, e
}

var digits = map[rune]string{'0': "111101101101111", '1': "010110010010111", '2': "111001111100111", '3': "111001111001111", '4': "101101111001001", '5': "111100111001111", '6': "111100111101111", '7': "111001010010010", '8': "111101111101111", '9': "111101111001111", '.': "000000000000010"}

func label(dst *image.RGBA, x, y int, text string) {
	for _, r := range text {
		for i, v := range digits[r] {
			if v == '1' {
				draw.Draw(dst, image.Rect(x+i%3*2, y+i/3*2, x+i%3*2+2, y+i/3*2+2), image.NewUniform(color.White), image.Point{}, draw.Src)
			}
		}
		x += 8
	}
}

// MediaFingerprint identifies the archived media independently of disposable seek assets.
func MediaFingerprint(a Asset) string {
	h := sha256.New()
	fmt.Fprint(h, a.ID)
	if st, err := os.Stat(a.File); err == nil {
		fmt.Fprintf(h, "|%d:%d", st.Size(), st.ModTime().UnixNano())
	}
	return hex.EncodeToString(h.Sum(nil))[:24]
}
