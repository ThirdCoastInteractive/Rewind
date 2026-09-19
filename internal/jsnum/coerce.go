package jsnum

import (
	"encoding/json"
	"strconv"
	"strings"
)

var coerceKeys = []string{"start", "end", "start_ts", "end_ts", "duration", "font_size", "gain_db", "context_seconds", "revision"}

// CoerceObject rewrites DataStar-style empty/quoted numerics on a segment map
// so encoding/json can unmarshal them into float64/int fields.
func CoerceObject(m map[string]any) {
	if m == nil {
		return
	}
	for _, key := range coerceKeys {
		switch v := m[key].(type) {
		case nil:
			delete(m, key)
		case string:
			s := strings.TrimSpace(v)
			if s == "" {
				delete(m, key)
				continue
			}
			n, err := strconv.ParseFloat(s, 64)
			if err != nil {
				delete(m, key)
				continue
			}
			m[key] = coercedNumber(key, n)
		case json.Number:
			n, err := v.Float64()
			if err != nil {
				delete(m, key)
				continue
			}
			m[key] = coercedNumber(key, n)
		}
	}
	switch t := m["transition"].(type) {
	case string:
		if strings.TrimSpace(t) == "" {
			delete(m, "transition")
		}
	case map[string]any:
		CoerceObject(t)
	}
	for _, v := range m {
		switch child := v.(type) {
		case map[string]any:
			CoerceObject(child)
		case []any:
			CoerceList(child)
		}
	}
}

func coercedNumber(key string, n float64) any {
	if key == "font_size" || key == "revision" {
		return int(n)
	}
	return n
}

// CoerceList walks tool-call argument arrays (segments, timestamps).
func CoerceList(items []any) {
	for _, item := range items {
		switch child := item.(type) {
		case map[string]any:
			CoerceObject(child)
		case []any:
			CoerceList(child)
		}
	}
}

// CoerceArgs rewrites quoted numeric tool arguments in place.
func CoerceArgs(args map[string]any) map[string]any {
	CoerceObject(args)
	return args
}

// CoerceJSONArray walks a JSON array of objects and applies CoerceObject.
// On parse failure it returns the original bytes.
func CoerceJSONArray(raw []byte) []byte {
	if len(raw) == 0 || string(raw) == "null" {
		return []byte("[]")
	}
	var maps []map[string]any
	if err := json.Unmarshal(raw, &maps); err != nil {
		return raw
	}
	for i := range maps {
		CoerceObject(maps[i])
	}
	b, err := json.Marshal(maps)
	if err != nil {
		return raw
	}
	return b
}
