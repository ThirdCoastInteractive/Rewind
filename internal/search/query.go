// Package search builds PostgreSQL tsquery strings from human search boxes.
//
// Callers pass the result to SQL as text and cast with `::tsquery`. Never
// concatenate unsanitized user input into to_tsquery.
package search

import (
	"strings"
	"unicode"
)

// Result is a compiled library query.
type Result struct {
	// Raw is the trimmed original string. Empty means "no search".
	Raw string
	// TSQuery is a `simple`-dictionary tsquery with prefix matching on the
	// last unquoted token. Empty if Raw produced no lexemes.
	TSQuery string
	// WebSearch is the original string for websearch_to_tsquery('simple', …),
	// which understands quoted phrases, OR, and -negation.
	WebSearch string
	// Trigram is the raw string for pg_trgm / ILIKE fallback.
	Trigram string
}

// Compile turns a search box value into tsquery + fallback strings.
func Compile(q string) Result {
	q = strings.TrimSpace(q)
	if q == "" {
		return Result{}
	}
	out := Result{Raw: q, WebSearch: q, Trigram: q}
	tokens := splitTokens(q)
	if len(tokens) == 0 {
		return out
	}
	parts := make([]string, 0, len(tokens))
	for i, tok := range tokens {
		if tok.phrase {
			quoted := make([]string, 0)
			for _, w := range strings.Fields(tok.text) {
				w = sanitizeLexeme(w)
				if w != "" {
					quoted = append(quoted, w)
				}
			}
			if len(quoted) == 0 {
				continue
			}
			parts = append(parts, "("+strings.Join(quoted, " <-> ")+")")
			continue
		}
		lex := sanitizeLexeme(tok.text)
		if lex == "" {
			continue
		}
		if i == len(tokens)-1 {
			parts = append(parts, lex+":*")
		} else {
			parts = append(parts, lex)
		}
	}
	if len(parts) > 0 {
		out.TSQuery = strings.Join(parts, " & ")
	}
	return out
}

type token struct {
	text   string
	phrase bool
}

func splitTokens(q string) []token {
	var out []token
	var buf strings.Builder
	inQuote := false
	flush := func(phrase bool) {
		s := strings.TrimSpace(buf.String())
		buf.Reset()
		if s != "" {
			out = append(out, token{text: s, phrase: phrase})
		}
	}
	for _, r := range q {
		switch {
		case r == '"':
			if inQuote {
				flush(true)
				inQuote = false
			} else {
				flush(false)
				inQuote = true
			}
		case !inQuote && unicode.IsSpace(r):
			flush(false)
		default:
			buf.WriteRune(r)
		}
	}
	flush(inQuote)
	return out
}

// sanitizeLexeme keeps letters, digits, and apostrophes; tsquery operators
// and wildcards are stripped so user input cannot inject syntax.
func sanitizeLexeme(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '\'':
			b.WriteRune(r)
		case r == '-' || r == '_':
			// Drop separators; "mr-beast" → "mrbeast" still prefix-matches poorly,
			// so split-style callers should pass pre-split tokens. Inside a token
			// we keep nothing.
		}
	}
	out := b.String()
	switch out {
	case "and", "or", "not":
		return ""
	}
	return out
}
