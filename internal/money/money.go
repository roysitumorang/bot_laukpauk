// Package money formats and parses Indonesian Rupiah amounts.
//
// All amounts in the bot are whole Rupiah stored as int64.
package money

import (
	"encoding/json"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// Rupiah formats n as "Rp 12.500" (dot thousands separator).
func Rupiah(n int64) string {
	return "Rp " + Thousands(n)
}

// Thousands formats n with dot thousands separators: 1234567 -> "1.234.567".
func Thousands(n int64) string {
	neg := n < 0
	if neg {
		n = -n
	}
	s := strconv.FormatInt(n, 10)
	var b strings.Builder
	pre := len(s) % 3
	if pre > 0 {
		b.WriteString(s[:pre])
	}
	for i := pre; i < len(s); i += 3 {
		if b.Len() > 0 {
			b.WriteByte('.')
		}
		b.WriteString(s[i : i+3])
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

// ToInt64 converts loosely-typed JSON/Sheets values (float64, json.Number,
// strings such as "12.000" or "Rp 12,000", ints) to whole Rupiah.
// Unparseable values become 0.
func ToInt64(v any) int64 {
	switch x := v.(type) {
	case nil:
		return 0
	case int:
		return int64(x)
	case int64:
		return x
	case int32:
		return int64(x)
	case float64:
		return int64(math.Round(x))
	case float32:
		return int64(math.Round(float64(x)))
	case json.Number:
		if i, err := x.Int64(); err == nil {
			return i
		}
		if f, err := x.Float64(); err == nil {
			return int64(math.Round(f))
		}
		return 0
	case string:
		return ParseString(x)
	default:
		return 0
	}
}

var thousandsDots = regexp.MustCompile(`^\d{1,3}(\.\d{3})+$`)

// ParseString parses a human-entered amount. Dot-grouped thousands such as
// "12.000" are 12000; a plain decimal such as "12000.0" is parsed as a float;
// otherwise "Rp", spaces, dots and commas are stripped ("Rp 12,000" -> 12000).
func ParseString(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	if !thousandsDots.MatchString(s) {
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return int64(math.Round(f))
		}
	}
	if f, err := strconv.ParseFloat(strings.ReplaceAll(s, ".", ""), 64); err == nil && thousandsDots.MatchString(s) {
		return int64(math.Round(f))
	}
	r := strings.NewReplacer("Rp", "", "rp", "", " ", "", ".", "", ",", "")
	if i, err := strconv.ParseInt(r.Replace(s), 10, 64); err == nil {
		return i
	}
	return 0
}
