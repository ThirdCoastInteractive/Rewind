package shownote

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf16"
)

// TextChange describes the single changed span between two Markdown snapshots.
// Offsets use UTF-16 code units so they can be used directly with Y.Text.
type TextChange struct {
	StartUTF16 int
	EndUTF16   int
	Before     string
	After      string
	StartLine  int
	StartCol   int
	EndLine    int
	EndCol     int
}

var hunkPattern = regexp.MustCompile(`^@@ -([0-9]+)(?:,([0-9]+))? \+([0-9]+)(?:,([0-9]+))? @@`)

// ApplyUnifiedDiff validates and applies a single-document unified diff.
func ApplyUnifiedDiff(original, patch string) (string, error) {
	patchLines := strings.Split(strings.ReplaceAll(patch, "\r\n", "\n"), "\n")
	oldLines := splitDocumentLines(original)
	result := make([]string, 0, len(oldLines))
	oldIndex := 0
	hunks := 0
	fileHeaders := 0

	for i := 0; i < len(patchLines); {
		line := patchLines[i]
		if strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "+++ ") {
			fileHeaders++
			if fileHeaders > 2 {
				return "", fmt.Errorf("patch must target one document")
			}
			i++
			continue
		}
		match := hunkPattern.FindStringSubmatch(line)
		if match == nil {
			if strings.TrimSpace(line) == "" {
				i++
				continue
			}
			return "", fmt.Errorf("invalid unified diff line %d", i+1)
		}
		hunks++
		oldStart, _ := strconv.Atoi(match[1])
		startIndex := oldStart - 1
		if oldStart == 0 {
			startIndex = 0
		}
		if startIndex < oldIndex || startIndex > len(oldLines) {
			return "", fmt.Errorf("hunk %d starts outside the document", hunks)
		}
		result = append(result, oldLines[oldIndex:startIndex]...)
		oldIndex = startIndex
		i++
		oldSeen, newSeen := 0, 0
		for i < len(patchLines) && !strings.HasPrefix(patchLines[i], "@@ ") {
			entry := patchLines[i]
			if strings.HasPrefix(entry, "\\ No newline at end of file") {
				i++
				continue
			}
			if entry == "" && i == len(patchLines)-1 {
				break
			}
			if len(entry) == 0 {
				return "", fmt.Errorf("hunk %d contains an unprefixed line", hunks)
			}
			content := entry[1:]
			switch entry[0] {
			case ' ':
				if oldIndex >= len(oldLines) || oldLines[oldIndex] != content {
					return "", fmt.Errorf("hunk %d context does not match", hunks)
				}
				result = append(result, content)
				oldIndex++
				oldSeen++
				newSeen++
			case '-':
				if oldIndex >= len(oldLines) || oldLines[oldIndex] != content {
					return "", fmt.Errorf("hunk %d deletion does not match", hunks)
				}
				oldIndex++
				oldSeen++
			case '+':
				result = append(result, content)
				newSeen++
			default:
				return "", fmt.Errorf("hunk %d has invalid prefix", hunks)
			}
			i++
		}
		if expected := hunkCount(match[2]); expected != oldSeen {
			return "", fmt.Errorf("hunk %d expected %d old lines, got %d", hunks, expected, oldSeen)
		}
		if expected := hunkCount(match[4]); expected != newSeen {
			return "", fmt.Errorf("hunk %d expected %d new lines, got %d", hunks, expected, newSeen)
		}
	}
	if hunks != 1 {
		return "", fmt.Errorf("patch must contain exactly one document hunk")
	}
	result = append(result, oldLines[oldIndex:]...)
	output := strings.Join(result, "\n")
	if strings.HasSuffix(original, "\n") && !strings.HasSuffix(output, "\n") {
		output += "\n"
	}
	return output, nil
}

// ChangedTextRange returns the smallest old-document span whose replacement
// transforms before into after.
func ChangedTextRange(before, after string) TextChange {
	oldRunes, newRunes := []rune(before), []rune(after)
	prefix := 0
	for prefix < len(oldRunes) && prefix < len(newRunes) && oldRunes[prefix] == newRunes[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(oldRunes)-prefix && suffix < len(newRunes)-prefix && oldRunes[len(oldRunes)-1-suffix] == newRunes[len(newRunes)-1-suffix] {
		suffix++
	}
	oldEnd := len(oldRunes) - suffix
	newEnd := len(newRunes) - suffix
	startLine, startCol := runeLineColumn(oldRunes, prefix)
	endLine, endCol := runeLineColumn(oldRunes, oldEnd)
	return TextChange{
		StartUTF16: len(utf16.Encode(oldRunes[:prefix])),
		EndUTF16:   len(utf16.Encode(oldRunes[:oldEnd])),
		Before:     string(oldRunes[prefix:oldEnd]),
		After:      string(newRunes[prefix:newEnd]),
		StartLine:  startLine,
		StartCol:   startCol,
		EndLine:    endLine,
		EndCol:     endCol,
	}
}

// UTF16Slice returns the text between two Y.Text-compatible offsets.
func UTF16Slice(value string, start, end int) (string, error) {
	if start < 0 || end < start {
		return "", fmt.Errorf("invalid UTF-16 range")
	}
	runes := []rune(value)
	startRune, endRune := -1, -1
	offset := 0
	for index := 0; index <= len(runes); index++ {
		if offset == start && startRune < 0 {
			startRune = index
		}
		if offset == end {
			endRune = index
			break
		}
		if index == len(runes) {
			break
		}
		offset += len(utf16.Encode([]rune{runes[index]}))
	}
	if startRune < 0 || endRune < 0 {
		return "", fmt.Errorf("UTF-16 range is outside the document or splits a surrogate pair")
	}
	return string(runes[startRune:endRune]), nil
}

func splitDocumentLines(value string) []string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.TrimSuffix(value, "\n")
	if value == "" {
		return nil
	}
	return strings.Split(value, "\n")
}

func hunkCount(raw string) int {
	if raw == "" {
		return 1
	}
	value, _ := strconv.Atoi(raw)
	return value
}

func runeLineColumn(runes []rune, offset int) (int, int) {
	line, column := 1, 1
	for _, value := range runes[:offset] {
		if value == '\n' {
			line, column = line+1, 1
		} else {
			column++
		}
	}
	return line, column
}

// MarkdownRange validates a 1-based line/column range and returns its selected
// text and UTF-16 offsets for Yjs relative-position anchors.
func MarkdownRange(markdown string, startLine, startColumn, endLine, endColumn int) (string, int, int, error) {
	lines := strings.Split(strings.ReplaceAll(markdown, "\r\n", "\n"), "\n")
	if startLine < 1 || endLine < startLine || endLine > len(lines) {
		return "", 0, 0, fmt.Errorf("line range is outside the document")
	}
	startByte, startUTF16, err := lineColumnOffset(lines, startLine, startColumn)
	if err != nil {
		return "", 0, 0, err
	}
	endByte, endUTF16, err := lineColumnOffset(lines, endLine, endColumn)
	if err != nil {
		return "", 0, 0, err
	}
	if endByte < startByte {
		return "", 0, 0, fmt.Errorf("range end precedes its start")
	}
	return strings.ReplaceAll(markdown, "\r\n", "\n")[startByte:endByte], startUTF16, endUTF16, nil
}

func lineColumnOffset(lines []string, line, column int) (int, int, error) {
	if column < 1 {
		return 0, 0, fmt.Errorf("column must be 1 or greater")
	}
	byteOffset, utf16Offset := 0, 0
	for i := 0; i < line-1; i++ {
		byteOffset += len(lines[i]) + 1
		utf16Offset += len(utf16.Encode([]rune(lines[i]))) + 1
	}
	runes := []rune(lines[line-1])
	if column-1 > len(runes) {
		return 0, 0, fmt.Errorf("column is outside line %d", line)
	}
	prefix := string(runes[:column-1])
	return byteOffset + len(prefix), utf16Offset + len(utf16.Encode([]rune(prefix))), nil
}
