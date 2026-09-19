package events

import "testing"

func TestSubscriptionsFilterCoalesceAndResync(t *testing.T) {
	var h Hub
	jobs, stopJobs := h.Subscribe("jobs_ui")
	all, stopAll := h.Subscribe()
	defer stopAll()
	h.notify("visual_changed")
	select {
	case <-jobs:
		t.Fatal("unrelated event woke jobs")
	default:
	}
	select {
	case <-all:
	default:
		t.Fatal("unfiltered subscriber missed event")
	}
	for range 100 {
		h.notify("jobs_ui")
	}
	select {
	case <-jobs:
	default:
		t.Fatal("job notification missing")
	}
	select {
	case <-jobs:
		t.Fatal("burst was not coalesced")
	default:
	}
	h.notify("") // Successful reconnect must recover missed commits.
	select {
	case <-jobs:
	default:
		t.Fatal("reconnect did not resync jobs")
	}
	stopJobs()
	h.notify("jobs_ui")
	select {
	case <-jobs:
		t.Fatal("unsubscribed channel received notification")
	default:
	}
}
