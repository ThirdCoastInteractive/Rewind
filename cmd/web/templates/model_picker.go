package templates

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/dustin/go-humanize"
	"thirdcoast.systems/rewind/internal/modelruntime"
)

func modelAction(m inventoryRow, action string) string {
	return fmt.Sprintf("@post('/api/models/action?runtime=%s&model=%s&action=%s')", strings.ToLower(m.Runtime), url.QueryEscape(m.Name), action)
}

func modelAssignment(m inventoryRow, key string) string {
	return fmt.Sprintf("@post('/api/models/assign?model=%s&key=%s')", url.QueryEscape(m.Name), url.QueryEscape(key))
}

func inventoryHardware(raw []byte) modelruntime.Hardware {
	var in struct{ Hardware modelruntime.Hardware }
	_ = json.Unmarshal(raw, &in)
	return in.Hardware
}

func modelMemoryHint(raw []byte, m inventoryRow) string {
	var in struct {
		Ollama struct {
			Models []struct {
				Name string
				Size uint64
			}
		}
		Running struct {
			Models []struct {
				Name string
				Size uint64
				VRAM uint64 `json:"size_vram"`
			}
		}
	}
	_ = json.Unmarshal(raw, &in)
	if m.Runtime != "Ollama" {
		return "Device is selected in the runtime settings above. CPU is available with a compatible runtime image; Test verifies weight loading on the selected device."
	}
	for _, running := range in.Running.Models {
		if running.Name != m.Name {
			continue
		}
		if running.VRAM == 0 {
			return "Running on CPU · " + humanize.Bytes(running.Size) + " resident memory"
		}
		return fmt.Sprintf("Running · %s in GPU memory / %s total resident memory", humanize.Bytes(running.VRAM), humanize.Bytes(running.Size))
	}
	h := inventoryHardware(raw)
	if h.RemoteOllama {
		return "Remote Ollama: local memory cannot predict fit. Load or test to measure actual allocation."
	}
	for _, model := range in.Ollama.Models {
		if model.Name != m.Name || model.Size == 0 {
			continue
		}
		for _, gpu := range h.GPUs {
			if gpu.Free > model.Size {
				return "Weights fit within currently free GPU memory; context and runtime overhead need additional space. Backend compatibility still needs testing."
			}
		}
		if h.Available > model.Size {
			return "Weights fit in available system RAM. CPU or partial GPU offload may be slower; runtime overhead needs additional space."
		}
		if h.RAM == 0 {
			return "Memory telemetry unavailable. Test to verify compatibility."
		}
		return "Memory pressure: weights exceed currently reported free memory. Unload idle models or choose a smaller model before testing."
	}
	return "Memory requirement unknown."
}

func modelAssignments(raw []byte, name string) string {
	var in struct{ Assignments map[string]any }
	_ = json.Unmarshal(raw, &in)
	var labels []string
	for _, item := range []struct{ key, label string }{{"agent.model", "Assistant"}, {"ml.context_model", "Context"}, {"whisper.model", "Transcription"}, {"vision.clip_model", "Visual search"}, {"textcls.sentiment_model", "Sentiment"}, {"textcls.toxicity_model", "Toxicity"}} {
		if fmt.Sprint(in.Assignments[item.key]) == name {
			labels = append(labels, item.label)
		}
	}
	return strings.Join(labels, " · ")
}

func modelVisionInstallAction(m inventoryRow) string {
	return fmt.Sprintf("@post('/api/models/action?runtime=vision&model=%s&action=install&accept_license=true')", url.QueryEscape(m.Name))
}
