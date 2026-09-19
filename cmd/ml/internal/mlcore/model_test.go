package mlcore

import (
	"errors"
	"os"
	"testing"
)

func TestMissingModelWaiting(t *testing.T) {
	orig := Stat
	t.Cleanup(func() { Stat = orig })
	Stat = func(string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	if got := ModelCheck("/models/whisper/ggml-small.bin"); got != StatusWaitingModel {
		t.Fatalf("got %s", got)
	}
	if FilePresent("/models/whisper/ggml-small.bin") {
		t.Fatal("missing file reported present")
	}
}

func TestPresentModelOK(t *testing.T) {
	orig := Stat
	t.Cleanup(func() { Stat = orig })
	Stat = func(string) (os.FileInfo, error) {
		return fakeSize(2048), nil
	}
	if got := ModelCheck("/models/whisper/ggml-small.bin"); got != StatusOK {
		t.Fatalf("got %s", got)
	}
}

type sizeInfo struct {
	os.FileInfo
	n int64
}

func (s sizeInfo) Size() int64 { return s.n }

func fakeSize(n int64) os.FileInfo { return sizeInfo{n: n} }

func TestWaitingModelError(t *testing.T) {
	err := errors.Join(ErrWaitingModel, errors.New("stat failed"))
	if !errors.Is(err, ErrWaitingModel) {
		t.Fatal("expected errors.Is waiting_model")
	}
}


