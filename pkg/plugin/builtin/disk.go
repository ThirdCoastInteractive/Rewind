package builtin

import (
	"context"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"thirdcoast.systems/rewind/pkg/plugin"
)

// Disk is the OSS Blob: files under Root, keys mapped 1:1.
type Disk struct {
	Root string
}

func NewDisk(root string) *Disk {
	if strings.TrimSpace(root) == "" {
		root = "/downloads"
	}
	return &Disk{Root: root}
}

func (d *Disk) resolve(key string) (string, error) {
	clean := path.Clean("/" + strings.ReplaceAll(key, "\\", "/"))
	rel := strings.TrimPrefix(clean, "/")
	if rel == "" || rel == "." || strings.HasPrefix(rel, "../") || strings.Contains(rel, "/../") {
		return "", plugin.ErrNotFound
	}
	return filepath.Join(d.Root, filepath.FromSlash(rel)), nil
}

func (d *Disk) Open(_ context.Context, key string) (plugin.Reader, plugin.Stat, error) {
	p, err := d.resolve(key)
	if err != nil {
		return nil, plugin.Stat{}, err
	}
	f, err := os.Open(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, plugin.Stat{}, plugin.ErrNotFound
		}
		return nil, plugin.Stat{}, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, plugin.Stat{}, err
	}
	return f, plugin.Stat{Size: st.Size(), ModTime: st.ModTime()}, nil
}

func (d *Disk) Create(_ context.Context, key string) (io.WriteCloser, error) {
	p, err := d.resolve(key)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return nil, err
	}
	return os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
}

func (d *Disk) Remove(_ context.Context, key string) error {
	p, err := d.resolve(key)
	if err != nil {
		return err
	}
	err = os.Remove(p)
	if os.IsNotExist(err) {
		return plugin.ErrNotFound
	}
	return err
}

func (d *Disk) List(_ context.Context, prefix string) ([]string, error) {
	p, err := d.resolve(strings.TrimSuffix(prefix, "/") + "/x")
	if err != nil {
		return nil, err
	}
	dir := filepath.Dir(p)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	base := strings.TrimSuffix(prefix, "/")
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if base == "" {
			out = append(out, e.Name())
		} else {
			out = append(out, base+"/"+e.Name())
		}
	}
	return out, nil
}

func (d *Disk) PublicURL(context.Context, string, time.Duration) (string, error) {
	return "", nil
}

func (d *Disk) LocalPath(key string) (string, bool) {
	p, err := d.resolve(key)
	if err != nil {
		return "", false
	}
	return p, true
}
