package main

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
)

const (
	defaultRuntimeDir     = "/runtime"
	defaultOllamaVersion  = "0.33.2"
	visionLockFile        = "/opt/vision/requirements.lock"
	textclsLockFile       = "/opt/textcls/requirements.txt"
	alignmentLockFile     = "/opt/alignment/requirements.lock"
	diarizeLockFile       = "/opt/diarize/requirements.txt"
	diarizeTransformers   = "https://github.com/huggingface/transformers/archive/8f080cb5aa480f794283c6e8618f917cc9a6506d.tar.gz"
	diarizeInstallTimeout = 60 * time.Minute
	onnxruntimeCPU        = "onnxruntime==1.20.1"
	onnxruntimeGPU        = "onnxruntime-gpu==1.20.2"
	ollamaDownloadTimeout = 30 * time.Minute
	visionDownloadTimeout = 30 * time.Minute
)

func ensureRuntime(ctx context.Context) {
	dir := envOr("RUNTIME_DIR", defaultRuntimeDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		slog.Warn("runtime dir", "dir", dir, "error", err)
		return
	}
	if err := ensureOllama(ctx, dir); err != nil {
		slog.Warn("ollama runtime", "error", err)
	}
	if err := ensureVision(ctx, dir); err != nil {
		slog.Warn("vision runtime", "error", err)
	}
	if err := ensureTextcls(ctx, dir); err != nil {
		slog.Warn("textcls runtime", "error", err)
	}
	bin := filepath.Join(dir, "bin")
	venvBin := filepath.Join(dir, "vision", "bin")
	textclsBin := filepath.Join(dir, "textcls", "bin")
	os.Setenv("PATH", bin+string(os.PathListSeparator)+venvBin+string(os.PathListSeparator)+textclsBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	if lib := filepath.Join(dir, "lib", "ollama"); dirExists(lib) {
		os.Setenv("LD_LIBRARY_PATH", lib+string(os.PathListSeparator)+os.Getenv("LD_LIBRARY_PATH"))
	}
}

func ensureAlignment(ctx context.Context, dir string) error {
	raw, e := os.ReadFile(envOr("ALIGNMENT_LOCK", alignmentLockFile))
	if e != nil {
		return e
	}
	stamp := "alignment=" + fmt.Sprintf("%x", sha256.Sum256(raw))
	venv := filepath.Join(dir, "alignment")
	py := filepath.Join(venv, "bin", "python")
	if stampOK(dir, "alignment", stamp) {
		if _, e = os.Stat(py); e == nil {
			return nil
		}
	}
	python, e := exec.LookPath("python3")
	if e != nil {
		return e
	}
	if e = runCmd(ctx, python, "-m", "venv", venv); e != nil {
		return e
	}
	if e = runCmd(ctx, filepath.Join(venv, "bin", "pip"), "install", "--no-cache-dir", "-r", envOr("ALIGNMENT_LOCK", alignmentLockFile)); e != nil {
		return e
	}
	return writeStamp(dir, "alignment", stamp)
}

func ollamaArchiveURL(version, goos, arch, extra string) string {
	name := fmt.Sprintf("ollama-%s-%s%s.tar.zst", goos, arch, extra)
	return "https://github.com/ollama/ollama/releases/download/v" + version + "/" + name
}

func gpuRuntime() bool {
	runtimeName := strings.ToLower(envOr("ML_RUNTIME", ""))
	device := strings.ToLower(envOr("WHISPER_DEVICE", envOr("VISION_DEVICE", "cpu")))
	return strings.Contains(runtimeName, "cuda") || strings.Contains(runtimeName, "rocm") ||
		device == "cuda" || device == "rocm"
}

func ensureOllama(ctx context.Context, dir string) error {
	version := envOr("OLLAMA_VERSION", defaultOllamaVersion)
	extra := ""
	if strings.Contains(strings.ToLower(envOr("ML_RUNTIME", "")), "rocm") {
		extra = "-rocm"
	}
	stamp := "ollama=" + version + extra
	bin := filepath.Join(dir, "bin", "ollama")
	if stampOK(dir, "ollama", stamp) {
		if _, err := os.Stat(bin); err == nil {
			return nil
		}
	}
	url := ollamaArchiveURL(version, runtime.GOOS, runtime.GOARCH, extra)
	slog.Info("downloading ollama runtime", "url", url, "dest", dir)
	ctx, cancel := context.WithTimeout(ctx, ollamaDownloadTimeout)
	defer cancel()
	tmp := filepath.Join(dir, ".ollama-extract")
	_ = os.RemoveAll(tmp)
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if err := downloadExtractZst(ctx, url, tmp); err != nil {
		return err
	}
	stripOllamaExtras(filepath.Join(tmp, "lib", "ollama"))
	srcBin := filepath.Join(tmp, "bin", "ollama")
	if _, err := os.Stat(srcBin); err != nil {
		return fmt.Errorf("ollama binary missing after extract: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
		return err
	}
	_ = os.Remove(bin)
	if err := os.Rename(srcBin, bin); err != nil {
		return err
	}
	_ = os.Chmod(bin, 0o755)
	if srcLib := filepath.Join(tmp, "lib", "ollama"); dirExists(srcLib) {
		dstLib := filepath.Join(dir, "lib", "ollama")
		_ = os.RemoveAll(dstLib)
		if err := os.MkdirAll(filepath.Dir(dstLib), 0o755); err != nil {
			return err
		}
		if err := os.Rename(srcLib, dstLib); err != nil {
			return err
		}
	}
	return writeStamp(dir, "ollama", stamp)
}

func stripOllamaExtras(lib string) {
	_ = os.RemoveAll(filepath.Join(lib, "mlx_cuda_v13"))
	_ = os.RemoveAll(filepath.Join(lib, "vulkan"))
	if !gpuRuntime() {
		_ = os.RemoveAll(filepath.Join(lib, "cuda_v12"))
		_ = os.RemoveAll(filepath.Join(lib, "cuda_v13"))
		_ = os.RemoveAll(filepath.Join(lib, "rocm"))
		return
	}
	switch strings.ToLower(envOr("OLLAMA_LLM_LIBRARY", "")) {
	case "cuda_v12":
		_ = os.RemoveAll(filepath.Join(lib, "cuda_v13"))
	case "cuda_v13":
		_ = os.RemoveAll(filepath.Join(lib, "cuda_v12"))
	}
}

func skipOllamaExtract(name string) bool {
	n := strings.ToLower(filepath.ToSlash(name))
	if strings.Contains(n, "/mlx_") || strings.Contains(n, "/vulkan") {
		return true
	}
	if !gpuRuntime() && (strings.Contains(n, "/cuda_v") || strings.Contains(n, "/rocm")) {
		return true
	}
	switch strings.ToLower(envOr("OLLAMA_LLM_LIBRARY", "")) {
	case "cuda_v12":
		return strings.Contains(n, "/cuda_v13")
	case "cuda_v13":
		return strings.Contains(n, "/cuda_v12")
	}
	return false
}

func ensureVision(ctx context.Context, dir string) error {
	lock := envOr("VISION_LOCK", visionLockFile)
	raw, err := os.ReadFile(lock)
	if err != nil {
		return fmt.Errorf("vision lockfile: %w", err)
	}
	sum := fmt.Sprintf("%x", sha256.Sum256(raw))
	device := strings.ToLower(envOr("VISION_DEVICE", "cpu"))
	wheel := onnxruntimeCPU
	if device == "cuda" {
		wheel = onnxruntimeGPU
	}
	stamp := "vision=" + sum + " " + wheel
	venv := filepath.Join(dir, "vision")
	venvPython := filepath.Join(venv, "bin", "python")
	if stampOK(dir, "vision", stamp) {
		if _, err := os.Stat(venvPython); err == nil {
			return nil
		}
	}
	py, err := exec.LookPath("python3")
	if err != nil {
		py, err = exec.LookPath("python")
	}
	if err != nil {
		return fmt.Errorf("python3 not on PATH")
	}
	slog.Info("installing vision python runtime", "venv", venv, "wheel", wheel)
	ctx, cancel := context.WithTimeout(ctx, visionDownloadTimeout)
	defer cancel()
	_ = os.RemoveAll(venv)
	if err := runCmd(ctx, py, "-m", "venv", venv); err != nil {
		return err
	}
	pip := filepath.Join(venv, "bin", "pip")
	if err := runCmd(ctx, pip, "install", "--no-cache-dir", "-r", lock, wheel); err != nil {
		return err
	}
	return writeStamp(dir, "vision", stamp)
}

func ensureTextcls(ctx context.Context, dir string) error {
	lock := envOr("TEXTCLS_LOCK", textclsLockFile)
	raw, err := os.ReadFile(lock)
	if err != nil {
		return fmt.Errorf("textcls lockfile: %w", err)
	}
	sum := fmt.Sprintf("%x", sha256.Sum256(raw))
	device := strings.ToLower(envOr("TEXTCLS_DEVICE", envOr("VISION_DEVICE", "cpu")))
	wheel := onnxruntimeCPU
	if device == "cuda" {
		wheel = onnxruntimeGPU
	}
	stamp := "textcls=" + sum + " " + wheel
	venv := filepath.Join(dir, "textcls")
	venvPython := filepath.Join(venv, "bin", "python")
	if stampOK(dir, "textcls", stamp) {
		if _, err := os.Stat(venvPython); err == nil {
			return nil
		}
	}
	py, err := exec.LookPath("python3")
	if err != nil {
		py, err = exec.LookPath("python")
	}
	if err != nil {
		return fmt.Errorf("python3 not on PATH")
	}
	slog.Info("installing textcls python runtime", "venv", venv, "wheel", wheel)
	ctx, cancel := context.WithTimeout(ctx, visionDownloadTimeout)
	defer cancel()
	_ = os.RemoveAll(venv)
	if err := runCmd(ctx, py, "-m", "venv", venv); err != nil {
		return err
	}
	pip := filepath.Join(venv, "bin", "pip")
	if err := runCmd(ctx, pip, "install", "--no-cache-dir", "-r", lock, wheel); err != nil {
		return err
	}
	return writeStamp(dir, "textcls", stamp)
}

func ensureDiarize(ctx context.Context, dir, device string) error {
	lock := envOr("DIARIZE_LOCK", diarizeLockFile)
	raw, err := os.ReadFile(lock)
	if err != nil {
		return fmt.Errorf("diarize lockfile: %w", err)
	}
	device = strings.ToLower(strings.TrimSpace(device))
	if device != "cuda" {
		device = "cpu"
	}
	stamp := fmt.Sprintf("diarize=%x device=%s transformers=%s", sha256.Sum256(raw), device, diarizeTransformers)
	venv := filepath.Join(dir, "diarize")
	venvPython := filepath.Join(venv, "bin", "python")
	if stampOK(dir, "diarize", stamp) {
		if _, err := os.Stat(venvPython); err == nil {
			return nil
		}
	}
	py, err := exec.LookPath("python3")
	if err != nil {
		py, err = exec.LookPath("python")
	}
	if err != nil {
		return fmt.Errorf("python3 not on PATH")
	}
	slog.Info("installing diarize python runtime", "venv", venv, "device", device)
	ctx, cancel := context.WithTimeout(ctx, diarizeInstallTimeout)
	defer cancel()
	_ = os.RemoveAll(venv)
	if err := runCmd(ctx, py, "-m", "venv", venv); err != nil {
		return err
	}
	pip := filepath.Join(venv, "bin", "pip")
	if err := runCmd(ctx, pip, "install", "--no-cache-dir", "-r", lock); err != nil {
		return err
	}
	torchArgs := []string{"install", "--no-cache-dir", "torch"}
	if device == "cpu" {
		torchArgs = append(torchArgs, "--index-url", "https://download.pytorch.org/whl/cpu")
	}
	if err := runCmd(ctx, pip, torchArgs...); err != nil {
		return err
	}
	if err := runCmd(ctx, pip, "install", "--no-cache-dir", diarizeTransformers); err != nil {
		return err
	}
	return writeStamp(dir, "diarize", stamp)
}

func stampOK(dir, name, want string) bool {
	raw, err := os.ReadFile(filepath.Join(dir, name+".stamp"))
	return err == nil && strings.TrimSpace(string(raw)) == want
}

func writeStamp(dir, name, token string) error {
	return os.WriteFile(filepath.Join(dir, name+".stamp"), []byte(token+"\n"), 0o644)
}

func downloadExtractZst(ctx context.Context, url, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "rewind-ml")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: HTTP %s", url, resp.Status)
	}
	body := io.Reader(resp.Body)
	if resp.ContentLength > 0 {
		slog.Info("ollama download", "bytes", resp.ContentLength)
		body = &progressReader{r: resp.Body, total: resp.ContentLength}
	}
	zr, err := zstd.NewReader(body)
	if err != nil {
		return err
	}
	defer zr.Close()
	tr := tar.NewReader(zr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if skipOllamaExtract(hdr.Name) {
			if hdr.Typeflag == tar.TypeReg {
				if _, err := io.Copy(io.Discard, tr); err != nil {
					return err
				}
			}
			continue
		}
		if err := writeTarFile(dest, hdr, tr); err != nil {
			return err
		}
	}
}

func writeTarFile(dest string, hdr *tar.Header, r io.Reader) error {
	name := filepath.Clean(hdr.Name)
	if name == "." || strings.HasPrefix(name, "..") {
		if hdr.Typeflag == tar.TypeReg {
			_, err := io.Copy(io.Discard, r)
			return err
		}
		return nil
	}
	target := filepath.Join(dest, name)
	if !strings.HasPrefix(target, filepath.Clean(dest)+string(os.PathSeparator)) && target != filepath.Clean(dest) {
		return fmt.Errorf("refusing path %s", hdr.Name)
	}
	switch hdr.Typeflag {
	case tar.TypeDir:
		return os.MkdirAll(target, 0o755)
	case tar.TypeSymlink:
		_ = os.Remove(target)
		return os.Symlink(hdr.Linkname, target)
	case tar.TypeReg:
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		f, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(hdr.Mode)|0o644)
		if err != nil {
			return err
		}
		_, err = io.Copy(f, r)
		closeErr := f.Close()
		if err != nil {
			return err
		}
		return closeErr
	default:
		return nil
	}
}

func runCmd(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func dirExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}

type progressReader struct {
	r     io.Reader
	n     int64
	last  int64
	total int64
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.n += int64(n)
	if p.n-p.last >= 100<<20 {
		attrs := []any{"mb", p.n >> 20}
		if p.total > 0 {
			attrs = append(attrs, "of_mb", p.total>>20)
		}
		slog.Info("ollama download", attrs...)
		p.last = p.n
	}
	return n, err
}
