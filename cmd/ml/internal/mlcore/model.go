package mlcore

import (
	"errors"
	"os"
)

// ErrWaitingModel means a required local model file or Ollama tag is absent.
// The worker records job status waiting_model and must not download weights.
var ErrWaitingModel = errors.New("waiting_model")

const (
	StatusOK           = "ok"
	StatusWaitingModel = "waiting_model"
)

// Stat is os.Stat, overridable in tests (fake missing files).
var Stat = os.Stat

// FilePresent reports whether path exists and is non-empty. Never downloads.
func FilePresent(path string) bool {
	if path == "" {
		return false
	}
	st, err := Stat(path)
	return err == nil && st.Size() > 0
}

// ModelCheck returns StatusWaitingModel when the file is missing.
func ModelCheck(path string) string {
	if FilePresent(path) {
		return StatusOK
	}
	return StatusWaitingModel
}
