package encode

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"thirdcoast.systems/rewind/internal/stitch"
)

func writeSnapshotFonts(fonts []stitch.FontReference) (string, error) {
	dir, err := os.MkdirTemp("", "rewind-stitch-fonts-")
	if err != nil {
		return "", err
	}
	for i, font := range fonts {
		if len(font.Data) == 0 {
			os.RemoveAll(dir)
			return "", fmt.Errorf("font %q has no captured bytes", font.Family)
		}
		name := strings.Map(func(r rune) rune {
			if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
				return r
			}
			return '_'
		}, font.Family)
		if name == "" {
			name = fmt.Sprintf("font-%d", i)
		}
		if err := os.WriteFile(filepath.Join(dir, name+".ttf"), font.Data, 0o600); err != nil {
			os.RemoveAll(dir)
			return "", err
		}
	}
	return dir, nil
}
