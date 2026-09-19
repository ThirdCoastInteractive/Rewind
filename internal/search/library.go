package search

import (
	"encoding/json"
	"strings"
	"unicode"
)

// FieldClause restricts a phrase to one source in the archive.
type FieldClause struct {
	Field string `json:"field"`
	Text  string `json:"text"`
}

// LibraryQuery separates ordinary search text from field-specific phrases.
type LibraryQuery struct {
	General Result
	Fields  []FieldClause
}

// FieldsJSON returns the parameterized SQL representation of the field clauses.
func (q LibraryQuery) FieldsJSON() []byte {
	b, _ := json.Marshal(q.Fields)
	if len(q.Fields) == 0 {
		return []byte("[]")
	}
	return b
}

// ParseLibrary recognizes scopes outside quotes. An unquoted scope consumes
// text until the next scope; a quoted scope consumes only its quoted phrase.
func ParseLibrary(raw string) LibraryQuery {
	var out LibraryQuery
	var general []string
	r := []rune(raw)
	for i := 0; i < len(r); {
		if unicode.IsSpace(r[i]) {
			i++
			continue
		}
		start := i
		field, width := libraryScope(r[i:])
		if field != "" {
			i += width
		}
		if i < len(r) && r[i] == '"' {
			i++
			valueStart := i
			for i < len(r) && r[i] != '"' {
				i++
			}
			value := string(r[valueStart:i])
			if i < len(r) {
				i++
			}
			if field != "" {
				out.Fields = append(out.Fields, FieldClause{field, strings.TrimSpace(value)})
			} else {
				general = append(general, string(r[start:i]))
			}
			continue
		}
		valueStart := i
		for i < len(r) {
			if unicode.IsSpace(r[i]) {
				if field == "" {
					break
				}
				j := i
				for j < len(r) && unicode.IsSpace(r[j]) {
					j++
				}
				if next, _ := libraryScope(r[j:]); next != "" {
					break
				}
			}
			i++
		}
		if field != "" {
			out.Fields = append(out.Fields, FieldClause{field, strings.TrimSpace(string(r[valueStart:i]))})
		} else {
			general = append(general, string(r[start:i]))
		}
	}
	out.General = Compile(strings.Join(general, " "))
	return out
}

func libraryScope(r []rune) (string, int) {
	s := strings.ToLower(string(r))
	for _, field := range []string{"title", "url", "context", "transcript", "description", "uploader"} {
		prefix := "in" + field + ":"
		if strings.HasPrefix(s, prefix) {
			return field, len(prefix)
		}
	}
	return "", 0
}
