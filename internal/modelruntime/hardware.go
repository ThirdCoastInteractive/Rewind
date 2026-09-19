package modelruntime

import (
	"context"
	"encoding/csv"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Device describes memory visible to the ML worker, not a promise of backend support.
type Device struct {
	Name   string `json:"name"`
	Vendor string `json:"vendor"`
	Total  uint64 `json:"total"`
	Free   uint64 `json:"free"`
}

// Hardware is a point-in-time inventory from the inference container.
type Hardware struct {
	RemoteOllama bool      `json:"remote_ollama"`
	CPUs         int       `json:"cpus"`
	RAM          uint64    `json:"ram"`
	Available    uint64    `json:"available"`
	GPUs         []Device  `json:"gpus"`
	Observed     time.Time `json:"observed"`
}

func readBytes(path string) uint64 {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	n, _ := strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64)
	return n
}

func nvidiaDevices(raw string) []Device {
	rows, err := csv.NewReader(strings.NewReader(raw)).ReadAll()
	if err != nil {
		return nil
	}
	var devices []Device
	for _, row := range rows {
		if len(row) != 3 {
			continue
		}
		total, err := strconv.ParseUint(strings.TrimSpace(row[1]), 10, 64)
		if err != nil {
			continue
		}
		free, err := strconv.ParseUint(strings.TrimSpace(row[2]), 10, 64)
		if err != nil {
			continue
		}
		devices = append(devices, Device{strings.TrimSpace(row[0]), "NVIDIA", total << 20, free << 20})
	}
	return devices
}

func detectHardware(ctx context.Context) Hardware {
	endpoint := os.Getenv("OLLAMA_HOST")
	if !strings.Contains(endpoint, "://") {
		endpoint = "http://" + endpoint
	}
	u, _ := url.Parse(endpoint)
	remote := u != nil && u.Hostname() != "" && u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost" && u.Hostname() != "0.0.0.0" && u.Hostname() != "::1"
	h := Hardware{RemoteOllama: remote, CPUs: runtime.NumCPU(), Observed: time.Now().UTC(), GPUs: []Device{}}
	if data, err := os.ReadFile("/proc/meminfo"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			n, _ := strconv.ParseUint(fields[1], 10, 64)
			switch fields[0] {
			case "MemTotal:":
				h.RAM = n << 10
			case "MemAvailable:":
				h.Available = n << 10
			}
		}
	}
	// Account for the container's memory ceiling rather than advertising host RAM.
	limit := readBytes("/sys/fs/cgroup/memory.max")
	used := readBytes("/sys/fs/cgroup/memory.current")
	if limit == 0 {
		limit = readBytes("/sys/fs/cgroup/memory/memory.limit_in_bytes")
		used = readBytes("/sys/fs/cgroup/memory/memory.usage_in_bytes")
	}
	if limit > 0 && limit < h.RAM {
		h.RAM = limit
		free := uint64(0)
		if used < limit {
			free = limit - used
		}
		if free < h.Available {
			h.Available = free
		}
	}
	probe, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if raw, err := exec.CommandContext(probe, "nvidia-smi", "--query-gpu=name,memory.total,memory.free", "--format=csv,noheader,nounits").Output(); err == nil {
		h.GPUs = append(h.GPUs, nvidiaDevices(string(raw))...)
	}
	cards, _ := filepath.Glob("/sys/class/drm/card[0-9]*/device/vendor")
	for _, card := range cards {
		raw, _ := os.ReadFile(card)
		vendor := strings.TrimSpace(string(raw))
		name := map[string]string{"0x1002": "AMD", "0x8086": "Intel"}[vendor]
		if name == "" {
			continue
		}
		dir := filepath.Dir(card)
		total := readBytes(filepath.Join(dir, "mem_info_vram_total"))
		used := readBytes(filepath.Join(dir, "mem_info_vram_used"))
		free := uint64(0)
		if total > used {
			free = total - used
		}
		h.GPUs = append(h.GPUs, Device{Name: name + " " + filepath.Base(filepath.Dir(dir)), Vendor: name, Total: total, Free: free})
	}
	return h
}
