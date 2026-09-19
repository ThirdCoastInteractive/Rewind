package templates

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestAdminExportsSplitsClipAndStitchQueues(t *testing.T) {
	var out bytes.Buffer
	if err := AdminExports("tester", "stitch", 0, 102, &AdminExportStats{ReadyCount: 80, QueuedCount: 2}, "", "").Render(context.Background(), &out); err != nil {
		t.Fatal(err)
	}
	html := out.String()
	for _, want := range []string{"EXPORTS", "kind=clips", "kind=stitch", "Clips (0)", "Stitch (102)", "tab-btn-active"} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(html, "CLIP EXPORTS") {
		t.Fatal("old clip-only heading still present")
	}
}
