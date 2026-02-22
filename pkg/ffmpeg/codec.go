package ffmpeg

// Preset bundles combine common option combinations.

// Preset264Fast returns options for fast h264 encoding.
// Uses ultrafast preset with CRF 23 for quick encoding.
func Preset264Fast() []Option {
	return []Option{
		VideoCodec("libx264"),
		CRF(23),
		Preset("ultrafast"),
		PixelFormat("yuv420p"),
	}
}

// Preset264Quality returns options for quality h264 encoding.
// Uses medium preset with CRF 23 for better quality/size ratio.
func Preset264Quality() []Option {
	return []Option{
		VideoCodec("libx264"),
		CRF(23),
		Preset("medium"),
		PixelFormat("yuv420p"),
	}
}

// Preset264VeryFast returns options for h264 with veryfast preset.
// Balance between speed and quality.
func Preset264VeryFast() []Option {
	return []Option{
		VideoCodec("libx264"),
		CRF(28),
		Preset("veryfast"),
		PixelFormat("yuv420p"),
	}
}

// PresetAAC returns options for AAC audio encoding.
func PresetAAC() []Option {
	return []Option{
		AudioCodec("aac"),
		AudioBitrate("192k"),
		AudioChannels(2),
	}
}

// PresetExportHQ returns options for high-quality clip export.
// Uses medium preset with CRF 21 for high-quality output with reasonable file sizes.
// The medium preset enables the full x264 feature set (CABAC, B-frames, multiple
// reference frames, subpixel ME) - essential for producing clean I-frames on complex
// source material. CRF 21 is high quality without bloating file sizes.
func PresetExportHQ() []Option {
	return []Option{
		VideoCodec("libx264"),
		CRF(21),
		Preset("medium"),
		PixelFormat("yuv420p"),
	}
}

// PresetExportAAC returns options for high-quality AAC audio export.
func PresetExportAAC() []Option {
	return []Option{
		AudioCodec("aac"),
		AudioBitrate("192k"),
		AudioChannels(2),
	}
}

// PresetExportWebM returns options for high-quality VP9/Opus WebM export.
// Uses CRF 24 with row-mt for reasonable encode speed.
func PresetExportWebM() []Option {
	return []Option{
		VideoCodec("libvpx-vp9"),
		CRF(24),
		OptionFunc(func(cmd *Command) {
			cmd.postInput = append(cmd.postInput, "-b:v", "0", "-row-mt", "1")
		}),
		PixelFormat("yuv420p"),
	}
}

// PresetExportOpus returns options for Opus audio in WebM container.
func PresetExportOpus() []Option {
	return []Option{
		AudioCodec("libopus"),
		AudioBitrate("128k"),
		AudioChannels(2),
	}
}

// PresetExportGIF returns options for high-quality GIF output.
// Generates a palette per frame for optimal dithering.
func PresetExportGIF() []Option {
	return []Option{
		// GIF palette generation is handled via filter_complex in the encoder.
		// This just sets basic framerate limit.
		OptionFunc(func(cmd *Command) {
			cmd.postInput = append(cmd.postInput, "-r", "15")
		}),
	}
}

// ExportPresetForFormat returns (video codec options, audio options, file extension)
// for the given format string. Returns (h264, aac, ".mp4") as default.
func ExportPresetForFormat(format, quality string) (video []Option, audio []Option, ext string) {
	// Determine CRF override for "max" quality
	switch format {
	case "webm":
		video = PresetExportWebM()
		audio = PresetExportOpus()
		ext = ".webm"
		if quality == "max" {
			// Override CRF to 18 for max quality VP9
			video = append(video, CRF(18))
		}
	case "gif":
		video = PresetExportGIF()
		audio = nil // No audio in GIF
		ext = ".gif"
	default: // "mp4"
		video = PresetExportHQ()
		audio = PresetExportAAC()
		ext = ".mp4"
		if quality == "max" {
			// Override CRF to 17 for max quality h264
			video = append(video, CRF(17), Preset("slow"))
		}
	}
	return
}

// PresetRemux returns options for remuxing (stream copy).
func PresetRemux() []Option {
	return []Option{
		CopyAll,
		MapAll,
	}
}

// Flatten merges multiple option slices into one.
func Flatten(groups ...[]Option) []Option {
	var all []Option
	for _, g := range groups {
		all = append(all, g...)
	}
	return all
}
