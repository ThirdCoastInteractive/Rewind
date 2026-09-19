package osint

import (
	"math"
	"sort"
	"strings"
	"unicode"
)

// Common English function words used as authorship markers.
var functionWords = map[string]struct{}{
	"a": {}, "an": {}, "the": {}, "and": {}, "or": {}, "but": {}, "if": {},
	"in": {}, "on": {}, "at": {}, "to": {}, "for": {}, "of": {}, "with": {},
	"by": {}, "from": {}, "as": {}, "is": {}, "are": {}, "was": {}, "were": {},
	"be": {}, "been": {}, "being": {}, "have": {}, "has": {}, "had": {},
	"do": {}, "does": {}, "did": {}, "will": {}, "would": {}, "could": {},
	"should": {}, "may": {}, "might": {}, "must": {}, "shall": {}, "can": {},
	"this": {}, "that": {}, "these": {}, "those": {}, "it": {}, "its": {},
	"i": {}, "you": {}, "he": {}, "she": {}, "we": {}, "they": {},
	"me": {}, "him": {}, "her": {}, "us": {}, "them": {}, "my": {}, "your": {},
	"his": {}, "our": {}, "their": {}, "not": {}, "no": {}, "so": {}, "than": {},
	"then": {}, "there": {}, "here": {}, "what": {}, "which": {}, "who": {},
	"whom": {}, "when": {}, "where": {}, "why": {}, "how": {}, "all": {},
	"any": {}, "some": {}, "into": {}, "about": {}, "over": {}, "after": {},
	"before": {}, "between": {}, "through": {}, "during": {}, "without": {},
	"under": {}, "again": {}, "further": {}, "once": {}, "also": {}, "just": {},
	"only": {}, "own": {}, "same": {}, "too": {}, "very": {}, "because": {},
}

const topNGram = 32

// StyleFeatures extracts authorship markers from texts. Rates are per-token or
// per-char as noted in the key name; char 3-gram sketch keys are "ng3:<trigram>".
func StyleFeatures(texts []string) map[string]float64 {
	out := map[string]float64{}
	if len(texts) == 0 {
		return out
	}

	var (
		tokens       []string
		funcCount    int
		chars        int
		punct        int
		sentenceLens []float64
		curSentToks  int
		ngrams       = map[string]int{}
	)

	for _, text := range texts {
		norm := NormalizeText(text)
		if norm == "" {
			continue
		}
		chars += len([]rune(norm))
		for _, r := range text {
			if unicode.IsPunct(r) {
				punct++
			}
		}
		for _, tok := range tokenize(norm) {
			tokens = append(tokens, tok)
			curSentToks++
			if _, ok := functionWords[tok]; ok {
				funcCount++
			}
		}
		// Sentence splits from original punctuation.
		parts := splitSentences(text)
		if len(parts) == 0 && curSentToks > 0 {
			sentenceLens = append(sentenceLens, float64(curSentToks))
			curSentToks = 0
		} else {
			for _, p := range parts {
				n := len(tokenize(NormalizeText(p)))
				if n > 0 {
					sentenceLens = append(sentenceLens, float64(n))
				}
			}
			curSentToks = 0
		}
		runes := []rune(norm)
		for i := 0; i+3 <= len(runes); i++ {
			ngrams[string(runes[i:i+3])]++
		}
	}

	nTok := float64(len(tokens))
	if nTok == 0 {
		return out
	}
	out["function_word_rate"] = float64(funcCount) / nTok
	if chars > 0 {
		out["punct_rate"] = float64(punct) / float64(chars)
	}
	if len(sentenceLens) > 0 {
		var sum float64
		for _, v := range sentenceLens {
			sum += v
		}
		out["mean_sentence_len"] = sum / float64(len(sentenceLens))
	}
	uniq := map[string]struct{}{}
	for _, t := range tokens {
		uniq[t] = struct{}{}
	}
	out["type_token_ratio"] = float64(len(uniq)) / nTok

	type kv struct {
		k string
		v int
	}
	ranked := make([]kv, 0, len(ngrams))
	for k, v := range ngrams {
		ranked = append(ranked, kv{k, v})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].v != ranked[j].v {
			return ranked[i].v > ranked[j].v
		}
		return ranked[i].k < ranked[j].k
	})
	if len(ranked) > topNGram {
		ranked = ranked[:topNGram]
	}
	for _, item := range ranked {
		out["ng3:"+item.k] = float64(item.v)
	}
	return out
}

// MergeStyleFeatures blends prior features (weight n) with new features (weight m).
func MergeStyleFeatures(prior map[string]float64, n int, next map[string]float64, m int) map[string]float64 {
	if n <= 0 {
		return cloneFeatures(next)
	}
	if m <= 0 {
		return cloneFeatures(prior)
	}
	out := map[string]float64{}
	keys := map[string]struct{}{}
	for k := range prior {
		keys[k] = struct{}{}
	}
	for k := range next {
		keys[k] = struct{}{}
	}
	tn := float64(n + m)
	for k := range keys {
		out[k] = (prior[k]*float64(n) + next[k]*float64(m)) / tn
	}
	return out
}

// StyleDistance is Euclidean distance over rate features (ignores ng3:* sketch counts).
func StyleDistance(a, b map[string]float64) float64 {
	keys := []string{
		"function_word_rate", "punct_rate", "mean_sentence_len", "type_token_ratio",
	}
	var sum float64
	for _, k := range keys {
		d := a[k] - b[k]
		// mean_sentence_len is on a larger scale; normalize roughly by 20 tokens.
		if k == "mean_sentence_len" {
			d /= 20
		}
		sum += d * d
	}
	return math.Sqrt(sum)
}

// OverlappingFeatureNames returns rate feature names whose absolute difference is below eps.
func OverlappingFeatureNames(a, b map[string]float64, eps float64) []string {
	keys := []string{
		"function_word_rate", "punct_rate", "mean_sentence_len", "type_token_ratio",
	}
	var out []string
	for _, k := range keys {
		d := math.Abs(a[k] - b[k])
		if k == "mean_sentence_len" {
			d /= 20
		}
		if d <= eps {
			out = append(out, k)
		}
	}
	// Shared top trigrams also count as overlap evidence.
	for k := range a {
		if strings.HasPrefix(k, "ng3:") {
			if _, ok := b[k]; ok {
				out = append(out, k)
			}
		}
	}
	sort.Strings(out)
	return out
}

// ConservativeStyleThreshold is a tight distance cutoff for sock suggestions.
const ConservativeStyleThreshold = 0.08

func cloneFeatures(m map[string]float64) map[string]float64 {
	out := make(map[string]float64, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func splitSentences(s string) []string {
	var parts []string
	var b strings.Builder
	flush := func() {
		t := strings.TrimSpace(b.String())
		if t != "" {
			parts = append(parts, t)
		}
		b.Reset()
	}
	for _, r := range s {
		b.WriteRune(r)
		if r == '.' || r == '!' || r == '?' {
			flush()
		}
	}
	flush()
	return parts
}
