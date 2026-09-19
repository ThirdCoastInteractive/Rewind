package frames

import (
	"bytes"
	"context"
	"fmt"
	"image/jpeg"
	"math"
	"mime/multipart"
	"os/exec"
	"strconv"
	"time"
)

// Sample decodes a bounded sequence once, preserving full-frame geometry and PTS.
func (s *Service) Sample(ctx context.Context, a Asset, start, interval float64, count int) ([]Frame, error) {
	if a.File == "" {
		return nil, ErrMediaUnavailable
	}
	if !finite(start) || !finite(interval) || start < 0 || interval <= 0 || count < 1 || count > 32 {
		return nil, fmt.Errorf("invalid sample range")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	filter := fmt.Sprintf("select='gte(t,%.9f+selected_n*%.9f)',scale='if(gte(iw,ih),min(1280,iw),-2)':'if(gte(iw,ih),-2,min(1280,ih))',showinfo", start, interval)
	cmd := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-nostdin", "-loglevel", "info", "-ss", fmt.Sprintf("%.9f", math.Max(0, start-2)), "-copyts", "-start_at_zero", "-i", a.File, "-map", "0:v:0", "-vf", filter, "-frames:v", strconv.Itoa(count), "-vsync", "0", "-f", "mpjpeg", "-boundary_tag", "rewind-frame", "-vcodec", "mjpeg", "-q:v", "3", "pipe:1")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	pipe, e := cmd.StdoutPipe()
	if e != nil {
		return nil, e
	}
	if e = cmd.Start(); e != nil {
		return nil, e
	}
	// Bound each decoder to one image: jpeg.Decode can read beyond JPEG EOI.
	reader := multipart.NewReader(pipe, "rewind-frame")
	out := []Frame{}
	for i := 0; i < count; i++ {
		part, e := reader.NextPart()
		if e != nil {
			cancel()
			_ = cmd.Wait()
			return nil, fmt.Errorf("sample %d boundary: %w", i, e)
		}
		img, e := jpeg.Decode(part)
		if e != nil {
			cancel()
			_ = cmd.Wait()
			return nil, fmt.Errorf("sample %d decode: %w", i, e)
		}
		var b bytes.Buffer
		if e = jpeg.Encode(&b, img, &jpeg.Options{Quality: 90}); e != nil {
			cancel()
			_ = cmd.Wait()
			return nil, e
		}
		out = append(out, Frame{Requested: start + float64(i)*interval, Width: img.Bounds().Dx(), Height: img.Bounds().Dy(), JPEG: b.Bytes(), Source: "video_frame", Coverage: "full_frame"})
	}
	if e = cmd.Wait(); e != nil {
		return nil, fmt.Errorf("sample decode: %w: %.1000s", e, stderr.String())
	}
	matches := ptsPattern.FindAllStringSubmatch(stderr.String(), -1)
	if len(matches) < len(out) {
		return nil, fmt.Errorf("missing sample timestamps")
	}
	fingerprint := Fingerprint(a)
	for i := range out {
		actual, e := strconv.ParseFloat(matches[i][1], 64)
		if e != nil || !finite(actual) || actual+0.001 < out[i].Requested {
			return nil, fmt.Errorf("invalid decoded sample timestamp")
		}
		out[i].Actual = &actual
		out[i].Sample = actual
		out[i].Interval = [2]float64{actual, actual}
		out[i].Reference = fmt.Sprintf("rewind://video/%s/frame?t=%.6f&quality=detail&asset=%s", a.ID, actual, fingerprint)
		out[i].WebPath = fmt.Sprintf("/videos/%s?t=%.3f", a.ID, actual)
	}
	return out, nil
}
