package encode

import (
	"thirdcoast.systems/rewind/internal/stitch"
	"thirdcoast.systems/rewind/pkg/ffmpeg"
	"thirdcoast.systems/rewind/pkg/utils/crops"
)

func encoderLayout(layout *stitch.TeaserLayout) *ffmpeg.Layout {
	if layout == nil {
		return nil
	}
	out := make([]ffmpeg.LayoutCrop, len(layout.Crops))
	for i, crop := range layout.Crops {
		out[i] = ffmpeg.LayoutCrop{X: crop.X, Y: crop.Y, Width: crop.Width, Height: crop.Height}
	}
	return &ffmpeg.Layout{Mode: layout.Mode, Crops: out}
}

func encoderCrops(in []stitch.CameraCrop) crops.CropArray {
	if len(in) == 0 {
		return nil
	}
	out := make(crops.CropArray, len(in))
	for i, c := range in {
		out[i] = crops.Crop{ID: c.ID, Name: c.Name, AspectRatio: c.AspectRatio, X: c.X, Y: c.Y, Width: c.Width, Height: c.Height}
	}
	return out
}

func encoderShots(in []stitch.CameraShot) crops.ShotList {
	if len(in) == 0 {
		return nil
	}
	out := make(crops.ShotList, len(in))
	for i, s := range in {
		shot := crops.Shot{CropID: s.CropID, Start: s.Start, End: s.End}
		if s.TransitionOut != nil {
			tr := crops.ShotTransition{Type: s.TransitionOut.Type, Duration: s.TransitionOut.Duration}
			shot.TransitionOut = &tr
		}
		out[i] = shot
	}
	return out
}

// inputRelativeShots converts clip-relative shot times to the already-trimmed
// stitch input (0 = segment -ss / SourceIn). Shots before the in-point or
// past duration are dropped; transitions are clamped below shot duration.
func inputRelativeShots(shots crops.ShotList, clipStartUS int64, sourceInSec, durationSec float64) crops.ShotList {
	if len(shots) == 0 || durationSec <= 0 {
		return nil
	}
	clipStartSec := float64(clipStartUS) / 1e6
	if clipStartSec == 0 {
		clipStartSec = sourceInSec
	}
	offset := sourceInSec - clipStartSec
	out := make(crops.ShotList, 0, len(shots))
	for _, shot := range shots {
		relStart := shot.Start - offset
		relEnd := shot.End - offset
		if relStart < 0 {
			relStart = 0
		}
		if relEnd > durationSec {
			relEnd = durationSec
		}
		if relEnd <= relStart {
			continue
		}
		s := crops.Shot{CropID: shot.CropID, Start: relStart, End: relEnd}
		if shot.TransitionOut != nil && shot.TransitionOut.Duration > 0 {
			tr := *shot.TransitionOut
			shotDur := relEnd - relStart
			if tr.Duration >= shotDur {
				tr.Duration = shotDur / 2
			}
			if tr.Duration > 0 {
				s.TransitionOut = &tr
			}
		}
		out = append(out, s)
	}
	return out
}

// encoderMulticam returns crops/shots for ffmpeg.Segment. Multicam wins over
// teaser Layout: when any visible shot remains, layout is cleared.
func encoderMulticam(raw stitchSegmentJSON, sourceInSec, durationSec float64) (crops.CropArray, crops.ShotList, *ffmpeg.Layout) {
	layout := encoderLayout(raw.Layout)
	if len(raw.Shots) == 0 || len(raw.Crops) == 0 {
		return nil, nil, layout
	}
	shots := inputRelativeShots(raw.Shots, raw.ClipStartUS, sourceInSec, durationSec)
	if len(shots) == 0 {
		return nil, nil, layout
	}
	return append(crops.CropArray(nil), raw.Crops...), shots, nil
}
