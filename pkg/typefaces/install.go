package typefaces

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

type fontDetail struct {
	ID       string `json:"id"`
	Family   string `json:"family"`
	Category string `json:"category"`
	Variants map[string]map[string]map[string]struct {
		URL map[string]string `json:"url"`
	} `json:"variants"`
}

var installWeights = []int{400, 700, 600, 500, 800, 300}

// Install downloads latin TTF files for a Google family into FONTS_DIR.
func Install(ctx context.Context, family string) (Face, error) {
	cat, err := findCatalogID(ctx, family)
	if err != nil {
		return Face{}, err
	}
	detail, err := fetchDetail(ctx, cat.ID)
	if err != nil {
		return Face{}, err
	}
	dir := filepath.Join(googleDir(), cat.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Face{}, err
	}
	meta := familyMeta{ID: cat.ID, Family: detail.Family, Category: cat.Category}
	if meta.Family == "" {
		meta.Family = cat.Family
	}
	seen := map[int]bool{}
	for _, w := range installWeights {
		if seen[w] {
			continue
		}
		url := ttfURL(detail, w)
		if url == "" {
			continue
		}
		name := fmt.Sprintf("latin-%d-normal.ttf", w)
		dest := filepath.Join(dir, name)
		if err := downloadFile(ctx, url, dest); err != nil {
			if w == 400 || w == 700 {
				return Face{}, fmt.Errorf("%s weight %d: %w", meta.Family, w, err)
			}
			continue
		}
		meta.Files = append(meta.Files, familyFile{Weight: w, Style: "normal", Path: name})
		seen[w] = true
		if len(seen) >= 2 {
			break
		}
	}
	if len(meta.Files) == 0 {
		return Face{}, fmt.Errorf("no TTF files for %s", meta.Family)
	}
	if err := writeFamilyMeta(dir, meta); err != nil {
		return Face{}, err
	}
	return loadFamily(dir)
}

// Uninstall removes a downloaded family. Bundled faces cannot be removed.
func Uninstall(id string) error {
	id = slug(id)
	if id == "tomorrow" || id == "unifraktur-cook" {
		return fmt.Errorf("cannot remove bundled font")
	}
	dir := filepath.Join(googleDir(), id)
	if _, err := os.Stat(dir); err != nil {
		return fmt.Errorf("font %s is not installed", id)
	}
	return os.RemoveAll(dir)
}

func fetchDetail(ctx context.Context, id string) (fontDetail, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fontsourceListURL+"/"+id, nil)
	if err != nil {
		return fontDetail{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Rewind/fonts")
	res, err := httpClient.Do(req)
	if err != nil {
		return fontDetail{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 1024))
		return fontDetail{}, fmt.Errorf("font %s: HTTP %d %s", id, res.StatusCode, b)
	}
	var d fontDetail
	if err := json.NewDecoder(res.Body).Decode(&d); err != nil {
		return fontDetail{}, err
	}
	return d, nil
}

func ttfURL(d fontDetail, weight int) string {
	byStyle, ok := d.Variants[fmt.Sprintf("%d", weight)]
	if !ok {
		return ""
	}
	bySubset, ok := byStyle["normal"]
	if !ok {
		return ""
	}
	for _, subset := range []string{"latin", "latin-ext"} {
		if u, ok := bySubset[subset]; ok && u.URL["ttf"] != "" {
			return u.URL["ttf"]
		}
	}
	for _, u := range bySubset {
		if u.URL["ttf"] != "" {
			return u.URL["ttf"]
		}
	}
	return ""
}

func downloadFile(ctx context.Context, url, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Rewind/fonts")
	res, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", res.StatusCode)
	}
	tmp := dest + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	_, err = io.Copy(f, io.LimitReader(res.Body, 8<<20))
	closeErr := f.Close()
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	if st, err := os.Stat(tmp); err != nil || st.Size() < 1024 {
		_ = os.Remove(tmp)
		return fmt.Errorf("truncated font download")
	}
	return os.Rename(tmp, dest)
}
