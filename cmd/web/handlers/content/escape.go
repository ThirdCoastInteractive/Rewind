package content

import "encoding/json"

func escapeJSSingleQuote(s string) string {
	b, _ := json.Marshal(s)
	if len(b) >= 2 {
		s = string(b[1 : len(b)-1])
	}
	result := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\'' {
			result = append(result, '\\', '\'')
		} else {
			result = append(result, s[i])
		}
	}
	return string(result)
}
