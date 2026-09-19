package templates

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestModelPickerHardwareAndActions(t *testing.T) {
	raw := []byte(`{"hardware":{"cpus":8,"ram":17179869184,"available":8589934592,"gpus":[{"name":"GPU One","free":6442450944,"total":8589934592}]},"ollama":{"models":[{"name":"small:latest","size":2147483648}]},"details":{"small:latest":{"capabilities":["completion","tools"]}},"whisper":[{"name":"tiny","installed":false}],"assignments":{"agent.model":"small:latest"}}`)
	var out bytes.Buffer
	if err := ModelInventory(raw, nil, "").Render(context.Background(), &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"GPU One", "Use for assistant", "Use for context", "currently free GPU memory", "tiny", "Assistant"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q", want)
		}
	}
}

func TestRemoteHardwareDoesNotPromiseLocalFit(t *testing.T) {
	raw := []byte(`{"hardware":{"remote_ollama":true,"available":999999999999},"ollama":{"models":[{"name":"small","size":100}]}}`)
	if got := modelMemoryHint(raw, inventoryRow{Name: "small", Runtime: "Ollama"}); !strings.Contains(got, "Remote Ollama") {
		t.Fatal(got)
	}
}
