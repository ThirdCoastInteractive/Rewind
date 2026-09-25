package runtimecfg

import (
	"encoding/json"
	"strings"
	"time"

	"thirdcoast.systems/rewind/internal/db"
)

// ConsumerStatus distinguishes receipt of configuration from completion of existing jobs.
// Acknowledgement applies to future admission/checkpoints; running jobs retain snapshots.
// LiveConsumers drops stopped containers and heartbeats that have gone silent.
func LiveConsumers(services []*db.RuntimeSettingsConsumer, now time.Time) []*db.RuntimeSettingsConsumer {
	out := make([]*db.RuntimeSettingsConsumer, 0, len(services))
	for _, service := range services {
		if service == nil || service.StoppedAt.Valid {
			continue
		}
		if strings.Contains(service.Service, "/") {
			continue
		}
		if !service.UpdatedAt.Valid || now.Sub(service.UpdatedAt.Time) > 3*time.Minute {
			continue
		}
		out = append(out, service)
	}
	return out
}

func ConsumerStatus(service *db.RuntimeSettingsConsumer, effective Snapshot, now time.Time) string {
	if service == nil {
		return "Offline or awaiting reconciliation"
	}
	if service.StoppedAt.Valid {
		return "Stopped gracefully"
	}
	if !service.UpdatedAt.Valid || now.Sub(service.UpdatedAt.Time) > 90*time.Second {
		return "Offline or awaiting reconciliation"
	}
	var applied Snapshot
	if json.Unmarshal(service.Snapshot, &applied) != nil {
		return "Invalid acknowledgement"
	}
	for _, definition := range Registry {
		if !consumes(strings.SplitN(service.Service, "/", 2)[0], definition.Key, definition.Owner) {
			continue
		}
		want, _ := json.Marshal(effective[definition.Key])
		got, _ := json.Marshal(applied[definition.Key])
		if string(want) != string(got) {
			return "Waiting for configuration"
		}
	}
	return "Applied for subsequent work"
}

func consumes(service, key, owner string) bool {
	if service == owner {
		return true
	}
	switch service {
	case "web":
		return strings.HasPrefix(key, "agent.") || strings.HasPrefix(key, "exports.")
	case "ml":
		return strings.HasPrefix(key, "ml.") || strings.HasPrefix(key, "whisper.") || strings.HasPrefix(key, "vision.") || strings.HasPrefix(key, "diarize.")
	case "downloader":
		return strings.HasPrefix(key, "downloads.")
	case "ingest":
		return strings.HasPrefix(key, "seek.") || key == "processing.ingest_workers" || key == "processing.asset_workers"
	case "encoder":
		return strings.HasPrefix(key, "exports.") || key == "processing.encoder_workers"
	}
	return false
}
