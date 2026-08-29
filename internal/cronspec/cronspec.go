// Package cronspec parses the cron schedules used by channel watching. It
// wraps robfig/cron's standard 5-field parser (plus @hourly/@daily/@weekly and
// @every descriptors) and enforces a minimum scan interval so a typo like
// "* * * * *" can't hammer a video site every minute.
package cronspec

import (
	"fmt"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

// MinInterval is the tightest allowed spacing between two scheduled runs.
const MinInterval = 10 * time.Minute

func parse(spec string) (cron.Schedule, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, fmt.Errorf("cron schedule is required")
	}
	sched, err := cron.ParseStandard(spec)
	if err != nil {
		return nil, fmt.Errorf("invalid cron schedule %q: %w", spec, err)
	}
	return sched, nil
}

// Validate checks that spec is a parseable cron schedule whose runs are at
// least MinInterval apart.
func Validate(spec string) error {
	sched, err := parse(spec)
	if err != nil {
		return err
	}

	// Probe consecutive occurrences: cron gaps vary across the calendar (e.g.
	// "0 0 * * *" vs "59 23 * * *" wrap differently), so check a handful.
	at := time.Now()
	for i := 0; i < 4; i++ {
		next := sched.Next(at)
		after := sched.Next(next)
		if after.Sub(next) < MinInterval {
			return fmt.Errorf("schedule %q runs more often than every %s", strings.TrimSpace(spec), MinInterval)
		}
		at = next
	}
	return nil
}

// Next returns the first occurrence of spec strictly after the given time.
func Next(spec string, after time.Time) (time.Time, error) {
	sched, err := parse(spec)
	if err != nil {
		return time.Time{}, err
	}
	return sched.Next(after), nil
}
