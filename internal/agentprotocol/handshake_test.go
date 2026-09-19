//go:build agenthandshake

package agentprotocol

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestInstalledProviderHandshake(t *testing.T) {
	for _, provider := range []string{"GROK", "CODEX"} {
		t.Run(provider, func(t *testing.T) {
			binary := os.Getenv("REWIND_" + provider + "_BIN")
			if binary == "" {
				t.Skip("explicit provider executable required")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			args := []string{"app-server"}
			if provider == "GROK" {
				args = []string{"--no-auto-update", "agent", "stdio"}
			}
			cmd := exec.CommandContext(ctx, binary, args...)
			home := t.TempDir()
			cmd.Dir = home
			cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "SystemRoot=" + os.Getenv("SystemRoot"), "TEMP=" + home, "TMP=" + home, "HOME=" + home, "USERPROFILE=" + home, "CODEX_HOME=" + filepath.Join(home, "codex"), "GROK_HOME=" + filepath.Join(home, "grok")}
			_ = os.MkdirAll(filepath.Join(home, "codex"), 0700)
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			stdin, err := cmd.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			cmd.Stderr = io.Discard
			if err = cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { stdin.Close(); cancel(); _ = cmd.Wait() }()
			p := NewPeer(ctx, stdout, stdin, nil)
			if provider == "GROK" {
				a := ACP{Peer: p}
				if err = a.Initialize(ctx); err != nil {
					t.Fatal(err)
				}
				t.Logf("ACP v1 HTTP MCP=%t session load=%t", a.HTTPMCP, a.LoadSession)
			} else {
				c := Codex{Peer: p}
				if err = c.Initialize(ctx); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
