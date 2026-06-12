package filters

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// ---------------------------------------------------------------------------
// Frequency Response Graph — SVG path computation
// ---------------------------------------------------------------------------

// FreqResponsePoint is an (x, y) coordinate for the SVG graph.
type FreqResponsePoint struct {
	X float64
	Y float64
}

// FreqResponseCurve holds the SVG path and axis info for a frequency response graph.
type FreqResponseCurve struct {
	// PathD is the SVG <path d="…"> attribute value.
	PathD string
	// Width and Height of the SVG viewbox.
	Width  float64
	Height float64
	// DBMin and DBMax define the vertical range.
	DBMin float64
	DBMax float64
}

// freqToX converts a frequency (Hz) to an x coordinate on a log scale.
func freqToX(freq, width float64) float64 {
	return math.Log10(freq/20) / math.Log10(20000.0/20.0) * width
}

// dbToY converts a dB value to a y coordinate.
func dbToY(db, height, dbMin, dbMax float64) float64 {
	return height - (db-dbMin)/(dbMax-dbMin)*height
}

// ComputePeakingResponse computes the frequency response of a peaking EQ filter.
// freq0 = center frequency (Hz), width = bandwidth (Hz), gain = boost/cut (dB).
func ComputePeakingResponse(freq0, width, gain, svgW, svgH float64) FreqResponseCurve {
	const (
		dbMin    = -15.0
		dbMax    = 15.0
		nPoints  = 60
		fMin     = 20.0
		fMax     = 20000.0
		logMin   = 1.301029995663981 // log10(20)
		logMax   = 4.301029995663981 // log10(20000)
		logRange = logMax - logMin
	)

	// Q derived from bandwidth: Q ≈ freq0 / width
	Q := freq0 / math.Max(width, 1)

	var sb strings.Builder
	for i := 0; i <= nPoints; i++ {
		// Log-spaced frequency
		logF := logMin + float64(i)/float64(nPoints)*logRange
		f := math.Pow(10, logF)

		// Peaking EQ magnitude response (analog approximation):
		// H(f) = gain * (BW^2) / ((f - f0)^2 + BW^2) in dB domain
		// Using the standard parametric EQ response:
		ratio := f / freq0
		denom := (ratio - 1/ratio)
		response := gain / (1 + Q*Q*denom*denom)

		x := freqToX(f, svgW)
		y := dbToY(response, svgH, dbMin, dbMax)

		if i == 0 {
			sb.WriteString(fmt.Sprintf("M%.1f %.1f", x, y))
		} else {
			sb.WriteString(fmt.Sprintf(" L%.1f %.1f", x, y))
		}
	}

	return FreqResponseCurve{
		PathD:  sb.String(),
		Width:  svgW,
		Height: svgH,
		DBMin:  dbMin,
		DBMax:  dbMax,
	}
}

// ComputeShelfResponse computes frequency response for a low-shelf or high-shelf filter.
// shelfType is "low" or "high". freq0 = shelf frequency, gain = dB.
func ComputeShelfResponse(shelfType string, freq0, gain, svgW, svgH float64) FreqResponseCurve {
	const (
		dbMin    = -15.0
		dbMax    = 15.0
		nPoints  = 60
		logMin   = 1.301029995663981
		logMax   = 4.301029995663981
		logRange = logMax - logMin
	)

	var sb strings.Builder
	for i := 0; i <= nPoints; i++ {
		logF := logMin + float64(i)/float64(nPoints)*logRange
		f := math.Pow(10, logF)

		var response float64
		ratio := f / freq0
		if shelfType == "low" {
			// Low shelf: full gain below freq0, rolls off above
			response = gain / (1 + ratio*ratio)
		} else {
			// High shelf: full gain above freq0, rolls off below
			invRatio := freq0 / f
			response = gain / (1 + invRatio*invRatio)
		}

		x := freqToX(f, svgW)
		y := dbToY(response, svgH, dbMin, dbMax)

		if i == 0 {
			sb.WriteString(fmt.Sprintf("M%.1f %.1f", x, y))
		} else {
			sb.WriteString(fmt.Sprintf(" L%.1f %.1f", x, y))
		}
	}

	return FreqResponseCurve{
		PathD:  sb.String(),
		Width:  svgW,
		Height: svgH,
		DBMin:  dbMin,
		DBMax:  dbMax,
	}
}

// ComputePassResponse computes frequency response for a highpass or lowpass filter.
// passType is "high" or "low". freq0 = cutoff frequency (Hz).
func ComputePassResponse(passType string, freq0, svgW, svgH float64) FreqResponseCurve {
	const (
		dbMin    = -30.0
		dbMax    = 3.0
		nPoints  = 60
		logMin   = 1.301029995663981
		logMax   = 4.301029995663981
		logRange = logMax - logMin
	)

	var sb strings.Builder
	for i := 0; i <= nPoints; i++ {
		logF := logMin + float64(i)/float64(nPoints)*logRange
		f := math.Pow(10, logF)

		var mag float64
		ratio := f / freq0
		if passType == "high" {
			// 2nd-order Butterworth highpass: H = ratio^2 / sqrt(1 + ratio^4)
			r2 := ratio * ratio
			mag = r2 / math.Sqrt(1+r2*r2)
		} else {
			// 2nd-order Butterworth lowpass: H = 1 / sqrt(1 + ratio^4)
			r2 := ratio * ratio
			mag = 1.0 / math.Sqrt(1+r2*r2)
		}

		// Convert magnitude to dB, clamp to range
		db := 20 * math.Log10(math.Max(mag, 0.001))
		if db < dbMin {
			db = dbMin
		}
		if db > dbMax {
			db = dbMax
		}

		x := freqToX(f, svgW)
		y := dbToY(db, svgH, dbMin, dbMax)

		if i == 0 {
			sb.WriteString(fmt.Sprintf("M%.1f %.1f", x, y))
		} else {
			sb.WriteString(fmt.Sprintf(" L%.1f %.1f", x, y))
		}
	}

	return FreqResponseCurve{
		PathD:  sb.String(),
		Width:  svgW,
		Height: svgH,
		DBMin:  dbMin,
		DBMax:  dbMax,
	}
}

// FreqGridLines returns the x positions for frequency grid lines at standard values.
func FreqGridLines(svgW float64) []struct {
	X     float64
	Label string
} {
	freqs := []struct {
		Hz    float64
		Label string
	}{
		{50, "50"}, {100, "100"}, {250, "250"}, {500, "500"},
		{1000, "1k"}, {2000, "2k"}, {5000, "5k"}, {10000, "10k"},
	}

	var lines []struct {
		X     float64
		Label string
	}
	for _, f := range freqs {
		lines = append(lines, struct {
			X     float64
			Label string
		}{
			X:     freqToX(f.Hz, svgW),
			Label: f.Label,
		})
	}
	return lines
}

// DBGridLines returns the y positions for dB grid lines.
func DBGridLines(svgH, dbMin, dbMax, step float64) []struct {
	Y     float64
	Label string
} {
	var lines []struct {
		Y     float64
		Label string
	}
	for db := dbMin; db <= dbMax; db += step {
		label := ""
		if db == 0 {
			label = "0"
		} else if math.Mod(db, step*2) == 0 || db == dbMin || db == dbMax {
			label = strconv.FormatFloat(db, 'f', 0, 64)
		}
		lines = append(lines, struct {
			Y     float64
			Label string
		}{
			Y:     dbToY(db, svgH, dbMin, dbMax),
			Label: label,
		})
	}
	return lines
}

// ---------------------------------------------------------------------------
// Dynamics Curve Graph — Compressor transfer function
// ---------------------------------------------------------------------------

// DynamicsCurve holds the SVG path for a compressor transfer function.
type DynamicsCurve struct {
	// PathD is the SVG <path d="…"> attribute for the transfer curve.
	PathD string
	// UnityPathD is the 1:1 diagonal line path.
	UnityPathD string
	// ThresholdX is the x coordinate where the threshold line sits.
	ThresholdX float64
	// Width and Height of the SVG viewbox.
	Width  float64
	Height float64
	// DBMin and DBMax define the axis range (both axes are the same).
	DBMin float64
	DBMax float64
}

// ComputeDynamicsCurve computes the compressor transfer function.
// threshold is in dB (negative), ratio is the compression ratio (e.g. 4 = 4:1).
func ComputeDynamicsCurve(threshold, ratio, svgW, svgH float64) DynamicsCurve {
	const (
		dbMin   = -60.0
		dbMax   = 0.0
		nPoints = 60
	)

	dbRange := dbMax - dbMin

	// Transfer curve
	var sb strings.Builder
	for i := 0; i <= nPoints; i++ {
		inputDB := dbMin + float64(i)/float64(nPoints)*dbRange

		var outputDB float64
		if inputDB <= threshold {
			outputDB = inputDB // Below threshold: unity gain
		} else {
			// Above threshold: compressed
			outputDB = threshold + (inputDB-threshold)/ratio
		}

		// Clamp
		if outputDB < dbMin {
			outputDB = dbMin
		}

		x := (inputDB - dbMin) / dbRange * svgW
		y := svgH - (outputDB-dbMin)/dbRange*svgH

		if i == 0 {
			sb.WriteString(fmt.Sprintf("M%.1f %.1f", x, y))
		} else {
			sb.WriteString(fmt.Sprintf(" L%.1f %.1f", x, y))
		}
	}

	// Unity line (1:1 diagonal)
	unityPath := fmt.Sprintf("M0 %.1f L%.1f 0", svgH, svgW)

	// Threshold x position
	threshX := (threshold - dbMin) / dbRange * svgW

	return DynamicsCurve{
		PathD:      sb.String(),
		UnityPathD: unityPath,
		ThresholdX: threshX,
		Width:      svgW,
		Height:     svgH,
		DBMin:      dbMin,
		DBMax:      dbMax,
	}
}

// ---------------------------------------------------------------------------
// Helper: parse filter params as float64
// ---------------------------------------------------------------------------

// ParamFloat extracts a float64 from a params map, handling both float64 and
// string representations. Returns defaultVal when the key is missing or unparseable.
func ParamFloat(params map[string]interface{}, key string, defaultVal float64) float64 {
	if params == nil {
		return defaultVal
	}
	v, ok := params[key]
	if !ok || v == nil {
		return defaultVal
	}
	switch val := v.(type) {
	case float64:
		return val
	case string:
		f, err := strconv.ParseFloat(val, 64)
		if err != nil {
			return defaultVal
		}
		return f
	default:
		return defaultVal
	}
}
