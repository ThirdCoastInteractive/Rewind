package runtimecfg

import (
	"encoding/json"
	"github.com/jackc/pgx/v5/pgtype"
	"testing"
	"thirdcoast.systems/rewind/internal/db"
	"time"
)

func TestLiveConsumersDropsStoppedAndStale(t *testing.T) {
	now := time.Now()
	live := &db.RuntimeSettingsConsumer{Service: "web", UpdatedAt: pgtype.Timestamptz{Time: now, Valid: true}}
	legacy := &db.RuntimeSettingsConsumer{Service: "ml/oldhost", UpdatedAt: pgtype.Timestamptz{Time: now, Valid: true}}
	stale := &db.RuntimeSettingsConsumer{Service: "ingest", UpdatedAt: pgtype.Timestamptz{Time: now.Add(-10 * time.Minute), Valid: true}}
	stopped := &db.RuntimeSettingsConsumer{Service: "encoder", UpdatedAt: pgtype.Timestamptz{Time: now, Valid: true}, StoppedAt: pgtype.Timestamptz{Time: now, Valid: true}}
	got := LiveConsumers([]*db.RuntimeSettingsConsumer{live, legacy, stale, stopped}, now)
	if len(got) != 1 || got[0].Service != "web" {
		t.Fatalf("%d %#v", len(got), got)
	}
}

func TestConsumerStatusDistinguishesPendingAndStale(t *testing.T) {
	now := time.Now()
	values := Defaults()
	raw, _ := json.Marshal(values)
	s := &db.RuntimeSettingsConsumer{Service: "ml", Hostname: "fixture", Snapshot: raw, UpdatedAt: pgtype.Timestamptz{Time: now, Valid: true}}
	if got := ConsumerStatus(s, values, now); got != "Applied for subsequent work" {
		t.Fatal(got)
	}
	values["ml.resident"] = 1
	if got := ConsumerStatus(s, values, now); got != "Waiting for configuration" {
		t.Fatal(got)
	}
	if got := ConsumerStatus(s, values, now.Add(2*time.Minute)); got != "Offline or awaiting reconciliation" {
		t.Fatal(got)
	}
	s.StoppedAt = pgtype.Timestamptz{Time: now, Valid: true}
	if got := ConsumerStatus(s, values, now); got != "Stopped gracefully" {
		t.Fatal(got)
	}
}
