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
	// HasNegation is true when the query contains a -word or -"phrase" token.
	HasNegation bool
	// HasPositive is true when at least one searchable token is not negated.
	// A negative-only query is intentionally not executed: applying it cue by cue
	// would turn every non-matching transcript cue into a false-positive hit.
	HasPositive bool
	tokens      []token
}

// Compile turns a search box value into tsquery + fallback strings.
func Compile(q string) Result {
	q = strings.TrimSpace(q)
	if q == "" {
		return Result{}
	}
	out := Result{Raw: q, WebSearch: q, Trigram: q}
	tokens := splitTokens(q)
	out.tokens = tokens
	if len(tokens) == 0 {
		return out
	}
	parts := make([]string, 0, len(tokens))
	for i, tok := range tokens {
		out.HasNegation = out.HasNegation || tok.negated
		part := ""
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
			part = "(" + strings.Join(quoted, " <-> ") + ")"
		} else {
			lex := sanitizeLexeme(tok.text)
			if lex == "" {
				continue
			}
			if i == len(tokens)-1 {
				part = lex + ":*"
			} else {
				part = lex
			}
		}
		if tok.negated {
			part = "!(" + part + ")"
		} else {
			out.HasPositive = true
		}
		parts = append(parts, part)
	}
	if len(parts) > 0 && out.HasPositive {
		out.TSQuery = strings.Join(parts, " & ")
	}
	return out
}

// Match reports whether text satisfies the same word, phrase, punctuation,
// and final-prefix rules used by the PostgreSQL query compiler.
func (r Result) Match(text string) bool {
	if len(r.tokens) == 0 || !r.HasPositive {
		return false
	}
	words := textLexemes(text)
	if len(words) == 0 {
		return false
	}
	for tokenIndex, tok := range r.tokens {
		matched := false
		if tok.phrase {
			phrase := textLexemes(tok.text)
			matched = len(phrase) > 0 && containsPhrase(words, phrase)
		} else {
			lex := sanitizeLexeme(tok.text)
			if lex == "" {
				continue
			}
			prefix := tokenIndex == len(r.tokens)-1
			for _, word := range words {
				if (!prefix && word == lex) || (prefix && strings.HasPrefix(word, lex)) {
					matched = true
					break
				}
			}
		}
		if tok.negated == matched {
			return false
		}
	}
	return true
}

func textLexemes(s string) []string {
	var words []string
	var b strings.Builder
	flush := func() {
		if b.Len() > 0 {
			words = append(words, strings.ToLower(b.String()))
			b.Reset()
		}
	}
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '\'' {
			b.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return words
}

func containsPhrase(words, phrase []string) bool {
	if len(phrase) > len(words) {
		return false
	}
	for i := 0; i <= len(words)-len(phrase); i++ {
		matched := true
		for j := range phrase {
			if words[i+j] != phrase[j] {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

type token struct {
	text    string
	phrase  bool
	negated bool
}

func splitTokens(q string) []token {
	runes := []rune(q)
	out := make([]token, 0)
	for i := 0; i < len(runes); {
		for i < len(runes) && unicode.IsSpace(runes[i]) {
			i++
		}
		if i >= len(runes) {
			break
		}
		negated := false
		if runes[i] == '-' && i+1 < len(runes) && !unicode.IsSpace(runes[i+1]) {
			negated = true
			i++
		}
		phrase := i < len(runes) && runes[i] == '"'
		if phrase {
			i++
		}
		start := i
		if phrase {
			for i < len(runes) && runes[i] != '"' {
				i++
			}
		} else {
			for i < len(runes) && !unicode.IsSpace(runes[i]) {
				i++
			}
		}
		text := strings.TrimSpace(string(runes[start:i]))
		if phrase && i < len(runes) && runes[i] == '"' {
			i++
		}
		if text != "" {
			out = append(out, token{text: text, phrase: phrase, negated: negated})
		}
	}
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
