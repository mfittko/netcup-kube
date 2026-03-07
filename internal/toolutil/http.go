// Package toolutil provides shared helpers for backend-agnostic data tools.
package toolutil

import (
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const defaultUserAgent = "Mozilla/5.0 (OpenClaw; fxempire-rates)"

// HTTPGetJSON performs an HTTP GET request and returns the raw response body.
// It applies a per-request timeout (timeoutMs milliseconds), sets a default
// User-Agent header, and merges any extra headers supplied by the caller.
func HTTPGetJSON(url string, timeoutMs int, headers map[string]string) ([]byte, error) {
	client := &http.Client{Timeout: time.Duration(timeoutMs) * time.Millisecond}

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request for %s: %w", url, err)
	}

	req.Header.Set("User-Agent", defaultUserAgent)
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: unexpected status %d", url, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response from %s: %w", url, err)
	}
	return body, nil
}

// FmtNum formats a floating-point number with exactly the given number of
// decimal places.  Negative zero is normalised to zero.
func FmtNum(v float64, decimals int) string {
	if decimals < 0 {
		decimals = 0
	}
	if v == 0 {
		v = math.Copysign(0, 1) // normalise -0.0 to +0.0
	}
	format := fmt.Sprintf("%%.%df", decimals)
	return fmt.Sprintf(format, math.Round(v*math.Pow10(decimals))/math.Pow10(decimals))
}

// FmtPct formats a floating-point percentage value with exactly two decimal
// places and appends a "%" suffix.  This matches the JS fmtPct helper which
// uses Number.toFixed(2).
func FmtPct(v float64) string {
	return FmtNum(v, 2) + "%"
}

// FmtNumUS formats a number the way JavaScript's
// n.toLocaleString('en-US', { maximumFractionDigits: 3 }) does:
//   - up to 3 decimal places (trailing zeros stripped)
//   - US-style thousand separators (commas)
//
// This matches the JS fmtNum helper used by fxempire_rates.mjs.
func FmtNumUS(v float64) string {
	// Round to at most 3 decimal places, then format as fixed-3.
	rounded := math.Round(v*1000) / 1000
	s := strconv.FormatFloat(rounded, 'f', 3, 64)

	// Trim trailing zeros after the decimal point.
	if strings.Contains(s, ".") {
		s = strings.TrimRight(s, "0")
		s = strings.TrimRight(s, ".")
	}

	// Split into integer and optional decimal part.
	dotIdx := strings.IndexByte(s, '.')
	intPart := s
	decPart := ""
	if dotIdx >= 0 {
		intPart = s[:dotIdx]
		decPart = s[dotIdx:] // includes the leading "."
	}

	// Strip sign for digit grouping.
	neg := strings.HasPrefix(intPart, "-")
	digits := intPart
	if neg {
		digits = intPart[1:]
	}

	// Insert comma separators every 3 digits from the right.
	var b strings.Builder
	for i, c := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}

	result := b.String()
	if neg {
		result = "-" + result
	}
	return result + decPart
}
