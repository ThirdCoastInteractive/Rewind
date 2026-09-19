package ingest

import (
	"context"
	"testing"

	"thirdcoast.systems/rewind/internal/config"
)

func TestStartRejectsNilArgs(t *testing.T) {
	if err := Start(context.Background(), nil, &config.Config{}); err == nil {
		t.Fatal("expected error for nil database connection")
	}
	if err := Start(context.Background(), nil, nil); err == nil {
		t.Fatal("expected error for nil args")
	}
}

func TestDownloadsAndSpoolDirs(t *testing.T) {
	t.Setenv("DOWNLOADS_DIR", "")
	t.Setenv("SPOOL_DIR", "")
	if got := downloadsDir(); got != "/downloads" {
		t.Fatalf("downloadsDir default = %q", got)
	}
	if got := spoolDir(); got != "/spool" {
		t.Fatalf("spoolDir default = %q", got)
	}
	t.Setenv("DOWNLOADS_DIR", "/tmp/rewind-dl")
	t.Setenv("SPOOL_DIR", "/tmp/rewind-spool")
	if got := downloadsDir(); got != "/tmp/rewind-dl" {
		t.Fatalf("downloadsDir = %q", got)
	}
	if got := spoolDir(); got != "/tmp/rewind-spool" {
		t.Fatalf("spoolDir = %q", got)
	}
}
