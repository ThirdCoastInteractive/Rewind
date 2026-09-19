package sfu

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"thirdcoast.systems/rewind/internal/config"
)

func TestNewServerConstructs(t *testing.T) {
	srv, err := NewServer(nil, &config.Config{})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	if srv == nil {
		t.Fatal("NewServer returned nil")
	}
	if srv.Handler() == nil {
		t.Fatal("Handler returned nil")
	}
}

func TestStartShutsDownOnCancel(t *testing.T) {
	t.Setenv("SFU_LISTEN_HOST", "127.0.0.1")

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- Start(ctx, nil, &config.Config{SFUPort: port})
	}()

	url := fmt.Sprintf("http://127.0.0.1:%d/healthz", port)
	client := &http.Client{Timeout: 200 * time.Millisecond}
	deadline := time.Now().Add(3 * time.Second)
	ready := false
	for time.Now().Before(deadline) {
		resp, getErr := client.Get(url)
		if getErr == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				ready = true
				break
			}
		}
		select {
		case err := <-errCh:
			t.Fatalf("Start exited before ready: %v", err)
		case <-time.After(20 * time.Millisecond):
		}
	}
	if !ready {
		cancel()
		t.Fatal("SFU did not become ready")
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Start: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return after cancel")
	}
}
