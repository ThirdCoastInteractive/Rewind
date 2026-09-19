package osint

import (
	"hash/fnv"
	"strings"
	"unicode"
)

// Simhash64 returns a 64-bit Charikar simhash of s (caller should NormalizeText first).
func Simhash64(s string) uint64 {
	var weights [64]int
	for _, tok := range tokenize(s) {
		h := fnv64(tok)
		for i := 0; i < 64; i++ {
			if h&(1<<uint(i)) != 0 {
				weights[i]++
			} else {
				weights[i]--
			}
		}
	}
	var out uint64
	for i := 0; i < 64; i++ {
		if weights[i] > 0 {
			out |= 1 << uint(i)
		}
	}
	return out
}

// HammingDistance returns the number of differing bits between two 64-bit hashes.
func HammingDistance(a, b uint64) int {
	x := a ^ b
	n := 0
	for x != 0 {
		n++
		x &= x - 1
	}
	return n
}

func tokenize(s string) []string {
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsPunct(r)
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if f != "" {
			out = append(out, f)
		}
	}
	if len(out) == 0 && s != "" {
		// Fall back to overlapping char 3-grams so short strings still hash.
		runes := []rune(s)
		if len(runes) < 3 {
			return []string{s}
		}
		for i := 0; i+3 <= len(runes); i++ {
			out = append(out, string(runes[i:i+3]))
		}
	}
	return out
}

func fnv64(s string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))
	return h.Sum64()
}
