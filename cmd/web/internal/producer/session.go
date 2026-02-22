package producer

// Session represents a producer session with connected viewers.
type Session struct {
	Code string
	subs map[chan []byte]struct{}
}
