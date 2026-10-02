package doc

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// The style a layer keeps follows one rule: defaults are left out (left
// alignment, normal style, no letter spacing …), and a text layer's outline is
// one "colour:width" value while a shape keeps colour and width apart. That is
// how documents have always been written, so old and new read the same.

// propKeys is every key updateLayer understands; anything else is a typo, and
// a typo is an error rather than a silent no-op.
var propKeys = []string{
	"x", "y", "rotation", "opacity", "visible", "locked", "name",
	"color", "fill", "fontFamily", "fontSize", "fontWeight", "fontStyle", "textAlign",
	"lineHeight", "charSpacing", "padding", "textBackgroundColor", "underline",
	"linethrough", "paintFirst", "shadow", "stroke", "strokeWidth", "cornerRadius",
	"flipX", "flipY", "filters", "width", "height", "skew", "stretch", "warp", "warpAmount", "warpBias",
}

// textStyleKeys is what addText's and setTextStyle's style bag may hold.
var textStyleKeys = []string{
	"fontFamily", "fontSize", "fontWeight", "fontStyle", "fill", "color", "textAlign",
	"lineHeight", "charSpacing", "shadow", "textBackgroundColor", "stroke", "strokeWidth",
	"paintFirst", "underline", "linethrough", "padding", "skew", "stretch", "warp", "warpAmount", "warpBias",
}

// Warps are the envelope shapes text can take (稿定's 变形).
var Warps = []string{"trapezoid"}

func checkKeys(props map[string]any, allowed []string) error {
	ok := map[string]bool{}
	for _, k := range allowed {
		ok[k] = true
	}
	var bad []string
	for k := range props {
		if !ok[k] {
			bad = append(bad, k)
		}
	}
	if len(bad) == 0 {
		return nil
	}
	sort.Strings(bad)
	return errf(`unknown style key "%s" — expected one of %s`, bad[0], strings.Join(allowed, ", "))
}

// applyProps writes a property bag onto a layer.
func applyProps(l *Layer, props map[string]any) error {
	if err := checkKeys(props, propKeys); err != nil {
		return err
	}
	isText := l.Type == "text"
	isLine := l.Type == "shape" && l.Shape != nil && l.Shape.Kind == "line"
	if v, ok := props["x"]; ok {
		l.X = round(num(v, l.X))
	}
	if v, ok := props["y"]; ok {
		l.Y = round(num(v, l.Y))
	}
	if v, ok := props["rotation"]; ok {
		l.Rotation = round(num(v, l.Rotation))
	}
	if v, ok := props["opacity"]; ok {
		l.Opacity = clamp(num(v, l.Opacity), 0, 1)
	}
	if v, ok := props["visible"]; ok {
		l.Visible = boolean(v, l.Visible)
	}
	if v, ok := props["locked"]; ok {
		l.Locked = boolean(v, l.Locked)
	}
	for _, k := range []string{"color", "fill"} {
		if v, ok := props[k]; ok {
			c := colour(v, str(l.S("fill")))
			if isLine {
				l.SetS("stroke", c)
			} else {
				l.SetS("fill", c)
			}
		}
	}
	if v, ok := props["fontFamily"]; ok {
		if f := strings.TrimSpace(str(v)); f != "" {
			l.SetS("fontFamily", f)
		}
	}
	if v, ok := props["fontSize"]; ok {
		l.SetS("fontSize", math.Max(1, num(v, num(l.S("fontSize"), 40))))
	}
	if v, ok := props["fontWeight"]; ok {
		l.SetS("fontWeight", v)
	}
	if v, ok := props["fontStyle"]; ok {
		if pick(v, []string{"normal", "italic"}, "normal") == "italic" {
			l.SetS("fontStyle", "italic")
		} else {
			l.SetS("fontStyle", nil)
		}
	}
	if v, ok := props["textAlign"]; ok {
		a := pick(v, []string{"left", "center", "right"}, "left")
		l.SetS("textAlign", nilIf(a, "left"))
	}
	if v, ok := props["lineHeight"]; ok {
		l.SetS("lineHeight", num(v, num(l.S("lineHeight"), 1.16)))
	}
	if v, ok := props["charSpacing"]; ok {
		l.SetS("charSpacing", nilIfZero(num(v, 0)))
	}
	if v, ok := props["padding"]; ok {
		l.SetS("padding", nilIfZero(math.Max(0, num(v, 0))))
	}
	if v, ok := props["textBackgroundColor"]; ok {
		s := strings.TrimSpace(str(v))
		if s == "" || s == "transparent" || s == "none" {
			l.SetS("textBackgroundColor", nil)
		} else {
			l.SetS("textBackgroundColor", s)
		}
	}
	for _, k := range []string{"underline", "linethrough", "paintFirst"} {
		if v, ok := props[k]; ok {
			if boolean(v, false) {
				l.SetS(k, true)
			} else {
				l.SetS(k, nil)
			}
		}
	}
	if v, ok := props["shadow"]; ok {
		s := strings.TrimSpace(str(v))
		if s == "" || s == "none" {
			l.SetS("shadow", nil)
		} else {
			l.SetS("shadow", normShadow(s))
		}
	}

	// Outline: one "colour:width" on text, colour and width apart on shapes.
	if v, ok := props["stroke"]; ok {
		c, w, hasW := splitStroke(strings.TrimSpace(str(v)))
		if c == "" {
			l.SetS("stroke", nil)
			if !isText {
				l.SetS("strokeWidth", nil)
			}
		} else if isText {
			if !hasW {
				_, w, _ = splitStroke(str(l.S("stroke")))
				if w <= 0 {
					w = 1
				}
			}
			l.SetS("stroke", fmt.Sprintf("%s:%s", c, fnum(w)))
		} else {
			l.SetS("stroke", c)
			if hasW {
				l.SetS("strokeWidth", w)
			}
		}
	}
	if v, ok := props["strokeWidth"]; ok {
		w := math.Max(0, num(v, 1))
		if isText {
			if c, _, _ := splitStroke(str(l.S("stroke"))); c != "" {
				if w > 0 {
					l.SetS("stroke", fmt.Sprintf("%s:%s", c, fnum(w)))
				} else {
					l.SetS("stroke", nil)
				}
			}
		} else {
			l.SetS("strokeWidth", w)
		}
	}
	if v, ok := props["cornerRadius"]; ok {
		l.SetS("cornerRadius", nilIfZero(math.Max(0, round(num(v, 0)))))
	}

	if isText {
		if v, ok := props["skew"]; ok {
			l.SetS("skew", nilIfZero(clamp(num(v, 0), -45, 45)))
		}
		if v, ok := props["stretch"]; ok {
			// The width on the page stays; the glyphs get wider or narrower.
			s := clamp(num(v, 1), 0.3, 3)
			if s == 1 {
				l.SetS("stretch", nil)
			} else {
				l.SetS("stretch", s)
			}
		}
		if v, ok := props["warp"]; ok {
			key := strings.TrimSpace(str(v))
			if key == "" || key == "none" {
				l.SetS("warp", nil)
				l.SetS("warpAmount", nil)
				l.SetS("warpBias", nil)
			} else {
				known := false
				for _, w := range Warps {
					known = known || w == key
				}
				if !known {
					return errf(`unknown warp "%s" — one of none, %s`, key, strings.Join(Warps, ", "))
				}
				l.SetS("warp", key)
				// A shape picked with no strength yet would do nothing visible.
				if num(l.S("warpAmount"), 0) == 0 && !has(props, "warpAmount") {
					l.SetS("warpAmount", 50.0)
				}
			}
		}
		if v, ok := props["warpAmount"]; ok && l.S("warp") != nil {
			l.SetS("warpAmount", clamp(round(num(v, 0)), -100, 100))
		}
		if v, ok := props["warpBias"]; ok && l.S("warp") != nil {
			l.SetS("warpBias", nilIfZero(clamp(round(num(v, 0)), -100, 100)))
		}
	}
	if l.Type == "image" {
		for _, k := range []string{"flipX", "flipY"} {
			if v, ok := props[k]; ok {
				l.SetS(k, boolean(v, boolean(l.S(k), false)))
			}
		}
		if v, ok := props["filters"]; ok {
			setFilters(l, v)
		}
	}

	// Size last: it may depend on the font size just set.
	if v, ok := props["width"]; ok {
		w := round(num(v, l.Width))
		switch l.Type {
		case "text":
			l.Width = math.Max(8, w)
		case "shape":
			l.Width = math.Max(1, w)
		default:
			l.Width = math.Max(1, w)
		}
	}
	if v, ok := props["height"]; ok && !isText {
		l.Height = math.Max(1, round(num(v, l.Height)))
	}
	return nil
}

// setFilters replaces an image's filters (zero values are dropped).
func setFilters(l *Layer, v any) {
	m, _ := v.(map[string]any)
	out := map[string]any{}
	for k, x := range m {
		if n := num(x, 0); n != 0 {
			out[k] = n
		}
	}
	if len(out) == 0 {
		l.SetS("filters", nil)
	} else {
		l.SetS("filters", out)
	}
}

// normShadow writes a shadow the way it is stored: "colour:blur:x:y".
func normShadow(s string) string {
	parts := strings.Split(s, ":")
	c := parts[0]
	if c == "" {
		c = "rgba(0,0,0,0.6)"
	}
	// An rgba() colour carries no colons, so the numbers are the tail.
	at := func(i int, d float64) float64 {
		if i < len(parts) {
			return num(parts[i], d)
		}
		return d
	}
	return fmt.Sprintf("%s:%s:%s:%s", c, fnum(at(1, 12)), fnum(at(2, 0)), fnum(at(3, 0)))
}

func fnum(v float64) string {
	return strings.TrimSuffix(strings.TrimRight(fmt.Sprintf("%.3f", v), "0"), ".")
}

func nilIf(v, def string) any {
	if v == def {
		return nil
	}
	return v
}

func nilIfZero(v float64) any {
	if v == 0 {
		return nil
	}
	return v
}

// textStyle builds a new text layer's style from defaults plus the keys given.
func textStyle(given map[string]any, defaults map[string]any) (map[string]any, error) {
	if err := checkKeys(given, textStyleKeys); err != nil {
		return nil, err
	}
	l := &Layer{Type: "text", Style: map[string]any{}}
	for k, v := range defaults {
		l.Style[k] = v
	}
	props := map[string]any{}
	for k, v := range given {
		props[k] = v
	}
	if err := applyProps(l, props); err != nil {
		return nil, err
	}
	return l.Style, nil
}
