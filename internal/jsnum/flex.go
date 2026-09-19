package jsnum

import (
	"encoding/json"
	"strconv"
	"strings"
)

// F is a float64 that accepts JSON numbers, numeric strings, "", or null.
type F float64

func (f *F) UnmarshalJSON(b []byte) error {
	n, err := ParseFloat(b)
	if err != nil {
		return err
	}
	*f = F(n)
	return nil
}

func (f F) MarshalJSON() ([]byte, error) {
	return json.Marshal(float64(f))
}

// I is an int that accepts JSON numbers, numeric strings, "", or null.
type I int

func (i *I) UnmarshalJSON(b []byte) error {
	n, err := ParseFloat(b)
	if err != nil {
		return err
	}
	*i = I(n)
	return nil
}

func (i I) MarshalJSON() ([]byte, error) {
	return json.Marshal(int(i))
}

// ParseFloat reads a JSON number, a quoted number, empty string, or null as a float64.
func ParseFloat(b []byte) (float64, error) {
	s := strings.TrimSpace(string(b))
	if s == "" || s == "null" || s == `""` {
		return 0, nil
	}
	if strings.HasPrefix(s, `"`) {
		var str string
		if err := json.Unmarshal(b, &str); err != nil {
			return 0, err
		}
		str = strings.TrimSpace(str)
		if str == "" {
			return 0, nil
		}
		return strconv.ParseFloat(str, 64)
	}
	var n float64
	if err := json.Unmarshal(b, &n); err != nil {
		return 0, err
	}
	return n, nil
}
