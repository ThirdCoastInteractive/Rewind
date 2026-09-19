package mlcore

import "errors"

func IsWaiting(err error) bool {
	return errors.Is(err, ErrWaitingModel)
}

// ErrSuperseded means this job should not run: the transcript or generation
// it was queued for is no longer current. It is not a success and not a failure.
var ErrSuperseded = errors.New("job superseded")
