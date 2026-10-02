package doc

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

var idCounter atomic.Int64

// NewID is short, sortable-ish and collision-resistant: "t_4k2m1ab3".
func NewID(prefix string) string {
	n := idCounter.Add(1)
	var b [3]byte
	_, _ = rand.Read(b[:])
	ts := strconv.FormatInt(time.Now().UnixMilli(), 36)
	if len(ts) > 4 {
		ts = ts[len(ts)-4:]
	}
	return prefix + "_" + ts + strconv.FormatInt(n, 36) + hex.EncodeToString(b[:])[:4]
}

func itoa(n int) string { return strconv.Itoa(n) }

func atoi(s string) (int, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	return n, err == nil
}

// --- coercion: AI sends "x":"100" often enough that tolerating it pays ---

func str(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	}
	return fmt.Sprint(v)
}

func num(v any, def float64) float64 {
	switch t := v.(type) {
	case float64:
		if !math.IsNaN(t) && !math.IsInf(t, 0) {
			return t
		}
	case int:
		return float64(t)
	case string:
		if f, err := strconv.ParseFloat(strings.TrimSpace(t), 64); err == nil {
			return f
		}
	}
	return def
}

func has(cmd map[string]any, key string) bool {
	v, ok := cmd[key]
	return ok && v != nil
}

func boolean(v any, def bool) bool {
	switch t := v.(type) {
	case bool:
		return t
	case float64:
		return t != 0
	case string:
		switch strings.ToLower(strings.TrimSpace(t)) {
		case "true", "1", "yes", "on":
			return true
		case "false", "0", "no", "off":
			return false
		}
	}
	return def
}

func pick(v any, allowed []string, def string) string {
	s := strings.TrimSpace(str(v))
	for _, a := range allowed {
		if a == s {
			return s
		}
	}
	return def
}

func colour(v any, def string) string {
	s := strings.TrimSpace(str(v))
	if s == "" {
		return def
	}
	return s
}

func clamp(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }

func round(v float64) float64 { return math.Round(v) }

// roundTo keeps a few decimals (text heights are fractional, layouts are not).
func roundTo(v float64, places int) float64 {
	k := math.Pow(10, float64(places))
	return math.Round(v*k) / k
}

func jsonMarshal(v any) ([]byte, error)   { return json.Marshal(v) }
func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }
