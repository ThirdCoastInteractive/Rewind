package sponsorblock

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"thirdcoast.systems/rewind/internal/db"
)

const defaultBaseURL = "https://sponsor.ajay.app"

type Client struct {
	baseURL string
	http    *http.Client
}

func NewClient(baseURL string) *Client {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	baseURL = strings.TrimRight(baseURL, "/")

	return &Client{
		baseURL: baseURL,
		http: &http.Client{
			Timeout: 8 * time.Second,
		},
	}
}

type SkipSegment struct {
	Segment       []float64 `json:"segment"`
	UUID          string    `json:"UUID"`
	Category      string    `json:"category"`
	VideoDuration float64   `json:"videoDuration"`
	ActionType    string    `json:"actionType"`
	Locked        int       `json:"locked"`
	Votes         int       `json:"votes"`
	Description   string    `json:"description"`
}

type SkipSegmentsParams struct {
	VideoID     string
	Service     string
	Categories  []string
	ActionTypes []string
}

func (c *Client) GetSkipSegments(ctx context.Context, params SkipSegmentsParams) ([]SkipSegment, error) {
	videoID := strings.TrimSpace(params.VideoID)
	if videoID == "" {
		return nil, fmt.Errorf("videoID is required")
	}

	service := strings.TrimSpace(params.Service)
	if service == "" {
		service = "YouTube"
	}

	u, err := url.Parse(c.baseURL + "/api/skipSegments")
	if err != nil {
		return nil, err
	}

	q := u.Query()
	q.Set("videoID", videoID)
	q.Set("service", service)

	if len(params.Categories) > 0 {
		b, err := json.Marshal(params.Categories)
		if err != nil {
			return nil, err
		}
		q.Set("categories", string(b))
	}

	if len(params.ActionTypes) > 0 {
		b, err := json.Marshal(params.ActionTypes)
		if err != nil {
			return nil, err
		}
		q.Set("actionTypes", string(b))
	}

	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return []SkipSegment{}, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 16*1024))
		return nil, fmt.Errorf("sponsorblock: unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var out []SkipSegment
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}

	return out, nil
}

// SegmentToMarker converts a sponsorblock segment to a db.Marker.
// Returns a virtual marker (not persisted in DB) with data from the segment.
func SegmentToMarker(videoID pgtype.UUID, seg SkipSegment) *db.Marker {
	// Convert sponsorblock UUID to marker ID
	var markerID pgtype.UUID
	_ = markerID.Scan(seg.UUID)

	// Start timestamp and duration
	var timestamp float64
	var duration *float64

	if len(seg.Segment) >= 2 {
		start := seg.Segment[0]
		end := seg.Segment[1]

		timestamp = start

		// Calculate duration
		dur := end - start
		if dur > 0 {
			duration = &dur
		}
	} else if len(seg.Segment) == 1 {
		// Fallback: single timestamp (shouldn't happen with SponsorBlock)
		timestamp = seg.Segment[0]
		// duration remains nil
	}

	// Color based on category
	color := CategoryColor(seg.Category)

	// Title from category and action type
	title := FormatSegmentTitle(seg.Category, seg.ActionType)

	// Description
	description := seg.Description
	if description == "" {
		description = seg.Category
	}

	// Determine marker type based on action
	markerType := db.MarkerTypePoint
	if seg.ActionType == "chapter" || seg.Category == "chapter" {
		markerType = db.MarkerTypeChapter
	}

	return &db.Marker{
		ID:          markerID,
		VideoID:     videoID,
		Timestamp:   timestamp,
		Duration:    duration,
		Title:       title,
		Description: description,
		Color:       color,
		MarkerType:  markerType,
		CreatedAt:   pgtype.Timestamptz{}, // Virtual marker
		CreatedBy:   pgtype.UUID{},        // Virtual marker
	}
}

// CategoryColor returns the color hex code for a sponsorblock category
func CategoryColor(category string) string {
	switch category {
	case "sponsor":
		return "#00d400"
	case "intro":
		return "#00ffff"
	case "outro":
		return "#0202ed"
	case "selfpromo":
		return "#ffff00"
	case "interaction":
		return "#cc00ff"
	case "music_offtopic":
		return "#ff9900"
	case "preview":
		return "#008fd6"
	case "chapter":
		return "#a67c52"
	default:
		return "#ffffff"
	}
}

// FormatSegmentTitle creates a human-readable title for a segment
func FormatSegmentTitle(category, actionType string) string {
	prefix := ""
	switch actionType {
	case "skip":
		prefix = "[Skip] "
	case "mute":
		prefix = "[Mute] "
	case "chapter":
		prefix = ""
	}

	name := strings.ReplaceAll(category, "_", " ")
	name = cases.Title(language.AmericanEnglish).String(name)
	return prefix + name
}
