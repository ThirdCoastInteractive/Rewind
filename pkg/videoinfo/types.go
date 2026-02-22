package videoinfo

// InfoPair is a simple label/value pair for data-driven info displays.
type InfoPair struct {
	Label string
	Value string
}

// InfoLinkPair is a label + URL + display text triple for link rows.
type InfoLinkPair struct {
	Label   string
	URL     string
	Display string
}

// StreamColumn holds the data needed to render one column in the streams table.
type StreamColumn struct {
	Index      int
	CodecType  string // "video", "audio", "subtitle"
	IsDefault  bool
	Language   string
	Title      string
	Properties map[string]string // label → value
}
