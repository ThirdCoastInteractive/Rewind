// Package plugin is Rewind's process-wide extension point.
//
// Public Rewind registers builtins (local users, disk storage, local ML).
// Live is unset unless a private binary registers it. Call Use before
// rewindapp.Run to swap in org auth, R2, and Cloudflare Stream.
package plugin

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
)

// Action names for Authz.Allow. Keep this list tiny.
const (
	ActionVideoRead  = "video.read"
	ActionVideoWrite = "video.write"
	ActionLiveManage = "live.manage"
	ActionAdmin      = "admin"
)

var (
	ErrNotAuthenticated = errors.New("not authenticated")
	ErrNotSupported     = errors.New("not supported")
	ErrNotFound         = errors.New("not found")
)

// Actor is the authenticated caller. TenantID is empty in the OSS builtin.
type Actor struct {
	UserID   string
	TenantID string
	Name     string
	Roles    []string
}

func (a *Actor) HasRole(role string) bool {
	if a == nil {
		return false
	}
	for _, r := range a.Roles {
		if r == role {
			return true
		}
	}
	return false
}

// Authn is login, logout, and the current session.
type Authn interface {
	Current(r *http.Request) (*Actor, error)
	Login(w http.ResponseWriter, r *http.Request) error
	Logout(w http.ResponseWriter, r *http.Request) error
	Register(w http.ResponseWriter, r *http.Request) error
	LoginPath() string
	Mount(e *echo.Echo)
}

// Authz is a single Allow method. No policy language.
type Authz interface {
	Allow(ctx context.Context, actor *Actor, action, resource string) bool
}

// Reader is a blob body that supports range serving.
type Reader interface {
	io.ReadCloser
	io.Seeker
}

// Stat is blob metadata.
type Stat struct {
	Size    int64
	ModTime time.Time
}

// Blob is object storage. Keys are slash paths, not OS paths.
// OSS layout: "{videoID}/{filename}". Private plugins may prefix a tenant.
type Blob interface {
	Open(ctx context.Context, key string) (Reader, Stat, error)
	Create(ctx context.Context, key string) (io.WriteCloser, error)
	Remove(ctx context.Context, key string) error
	List(ctx context.Context, prefix string) ([]string, error)
	// PublicURL is empty when the app should stream via Open.
	PublicURL(ctx context.Context, key string, ttl time.Duration) (string, error)
	// LocalPath is a real filesystem path when the blob is already a local file.
	// R2 returns ("", false).
	LocalPath(key string) (string, bool)
}

// Job is one unit of ML work. Kinds match the OSS queue names.
type Job struct {
	VideoID        string
	Kind           string
	Priority       int32
	TranscriptHash string
	ModelDigest    string
	PromptVersion  string
	RequestKey     string // transcribe range reuse key; empty is full-video
	RangeStart     *float64
	RangeEnd       *float64
}

const (
	KindTranscribe = "transcribe"
	KindEmbed      = "visual_index"
	KindClassify   = "comment_classify"
	KindContext    = "context_windows"
	KindSpeechTone = "speech_tone"
	KindRefine     = "refine_boundaries"
)

// ML is the only enqueue path for transcription, embedding, and classification.
// OSS LocalML writes ml_jobs; a private plugin may call a remote worker instead.
type ML interface {
	Enqueue(ctx context.Context, job Job) (jobID string, err error)
}

// LiveInput is a restream ingest endpoint shown to the creator.
type LiveInput struct {
	ID        string
	Name      string
	IngestURL string
	StreamKey string
	Status    string
}

// LiveOutput is one RTMP/SRT destination on a live input.
type LiveOutput struct {
	ID      string
	URL     string
	Enabled bool
}

// Live is restream + record. Absent (nil) on OSS Rewind. Private plugin is CF Stream.
type Live interface {
	CreateInput(ctx context.Context, actor *Actor, name string) (*LiveInput, error)
	GetInput(ctx context.Context, actor *Actor, id string) (*LiveInput, error)
	ListInputs(ctx context.Context, actor *Actor) ([]*LiveInput, error)
	DeleteInput(ctx context.Context, actor *Actor, id string) error
	AddOutput(ctx context.Context, actor *Actor, inputID, url, streamKey string) (*LiveOutput, error)
	RemoveOutput(ctx context.Context, actor *Actor, inputID, outputID string) error
	EnableOutput(ctx context.Context, actor *Actor, inputID, outputID string, on bool) error
	Mount(e *echo.Echo)
}

// Set is the bundle installed for this process.
type Set struct {
	Authn Authn
	Authz Authz
	Blob  Blob
	ML    ML
	Live  Live
}

var (
	mu      sync.RWMutex
	current Set
)

// Use merges non-nil fields into the process plugin set.
func Use(s Set) {
	mu.Lock()
	defer mu.Unlock()
	if s.Authn != nil {
		current.Authn = s.Authn
	}
	if s.Authz != nil {
		current.Authz = s.Authz
	}
	if s.Blob != nil {
		current.Blob = s.Blob
	}
	if s.ML != nil {
		current.ML = s.ML
	}
	if s.Live != nil {
		current.Live = s.Live
	}
}

// Current returns a copy of the process plugin set.
func Current() Set {
	mu.RLock()
	defer mu.RUnlock()
	return current
}

func Auth() Authn {
	mu.RLock()
	defer mu.RUnlock()
	return current.Authn
}

func Guards() Authz {
	mu.RLock()
	defer mu.RUnlock()
	return current.Authz
}

func Blobs() Blob {
	mu.RLock()
	defer mu.RUnlock()
	return current.Blob
}

func Jobs() ML {
	mu.RLock()
	defer mu.RUnlock()
	return current.ML
}

func LiveIngest() Live {
	mu.RLock()
	defer mu.RUnlock()
	return current.Live
}

// VideoKey is the blob key for a file in a video's directory.
func VideoKey(videoID, name string) string {
	return videoID + "/" + name
}

// Enqueue is the process-wide ML door. Callers must not insert ml_jobs themselves.
func Enqueue(ctx context.Context, job Job) error {
	_, err := EnqueueID(ctx, job)
	return err
}

// EnqueueID is Enqueue and returns the durable job id.
func EnqueueID(ctx context.Context, job Job) (string, error) {
	m := Jobs()
	if m == nil {
		return "", errors.New("ml plugin not registered")
	}
	if strings.TrimSpace(job.VideoID) == "" {
		return "", errors.New("ml job video_id required")
	}
	if strings.TrimSpace(job.Kind) == "" {
		return "", errors.New("ml job kind required")
	}
	if job.Priority == 0 {
		job.Priority = 160
	}
	return m.Enqueue(ctx, job)
}

// Transcribe queues speech-to-text for a video.
func Transcribe(ctx context.Context, videoID string) error {
	return Enqueue(ctx, Job{VideoID: videoID, Kind: KindTranscribe, Priority: 160})
}

// Embed queues visual indexing.
func Embed(ctx context.Context, videoID string) error {
	return Enqueue(ctx, Job{VideoID: videoID, Kind: KindEmbed, Priority: 500})
}

// Classify queues comment classification.
func Classify(ctx context.Context, videoID string) error {
	return Enqueue(ctx, Job{VideoID: videoID, Kind: KindClassify})
}
