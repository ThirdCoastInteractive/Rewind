package shownote

import (
	"fmt"
	"sort"
	"strings"

	"thirdcoast.systems/rewind/internal/db"
)

// LegacyMarkdown converts a legacy show-note block forest depth-first while
// preserving source order, notes, missing references, and duration overrides.
func LegacyMarkdown(blocks []*db.ShowNoteBlock, clips map[string]*db.Clip) string {
	children := make(map[string][]*db.ShowNoteBlock)
	for _, block := range blocks {
		parent := ""
		if block.ParentID.Valid {
			parent = block.ParentID.String()
		}
		children[parent] = append(children[parent], block)
	}
	for key := range children {
		sort.SliceStable(children[key], func(i, j int) bool {
			if children[key][i].Position == children[key][j].Position {
				return children[key][i].CreatedAt.Time.Before(children[key][j].CreatedAt.Time)
			}
			return children[key][i].Position < children[key][j].Position
		})
	}

	var out strings.Builder
	var walk func(parent string, depth int)
	walk = func(parent string, depth int) {
		for _, block := range children[parent] {
			writeLegacyBlock(&out, block, clips, depth)
			nextDepth := depth
			if block.BlockType == "section" {
				nextDepth++
			}
			walk(block.ID.String(), nextDepth)
		}
	}
	walk("", 1)
	return strings.TrimSpace(out.String()) + "\n"
}

func writeLegacyBlock(out *strings.Builder, block *db.ShowNoteBlock, clips map[string]*db.Clip, depth int) {
	title := strings.TrimSpace(block.Title)
	if title == "" {
		title = "Untitled"
	}
	switch block.BlockType {
	case "section":
		level := depth
		if level < 1 {
			level = 1
		}
		if level > 6 {
			level = 6
		}
		fmt.Fprintf(out, "%s %s\n", strings.Repeat("#", level), title)
	case "video":
		if block.VideoID.Valid {
			fmt.Fprintf(out, "- [%s](rewind://video/%s)\n", title, block.VideoID.String())
		} else {
			fmt.Fprintf(out, "- [Missing video: %s](rewind://missing/%s)\n", title, block.ID.String())
		}
		writeLegacyContext(out, block)
	case "clip":
		if block.ClipID.Valid {
			fmt.Fprintf(out, "- [%s](rewind://clip/%s)", title, block.ClipID.String())
			if clip := clips[block.ClipID.String()]; clip != nil {
				fmt.Fprintf(out, " @ %s–%s", FormatTimestamp(clip.StartTs), FormatTimestamp(clip.EndTs))
			}
			out.WriteString("\n")
		} else {
			fmt.Fprintf(out, "- [Missing clip: %s](rewind://missing/%s)\n", title, block.ID.String())
		}
		writeLegacyContext(out, block)
	case "break":
		fmt.Fprintf(out, "> Break — %s\n", title)
		writeLegacyContext(out, block)
	default:
		fmt.Fprintf(out, "<!-- Unrecognized legacy block %s: %s -->\n", block.BlockType, title)
	}
	out.WriteString("\n")
}

func writeLegacyContext(out *strings.Builder, block *db.ShowNoteBlock) {
	for _, line := range strings.Split(strings.TrimSpace(block.Notes), "\n") {
		if strings.TrimSpace(line) != "" {
			fmt.Fprintf(out, "  %s\n", strings.TrimSpace(line))
		}
	}
	if block.DurationOverride != nil {
		fmt.Fprintf(out, "  Duration override: %d seconds.\n", *block.DurationOverride)
	}
}
