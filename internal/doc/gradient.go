package doc

import (
	"math"
	"strings"
)

var gradientParam = Param{
	Name: "fillGradient", Type: "object",
	Desc: `线性渐变：{type:"linear",angle:180,stops:[{color:"#000000",opacity:0},{color:"#000000",opacity:1}]}；两端颜色与不透明度（默认 1）；角度 0 向上、90 向右、180 向下；null 恢复纯色`,
}

// Gradients are separate from fill so existing solid-colour documents stay valid.
// A new solid fill clears the gradient; an explicit gradient in the same edit wins.
func setFillGradient(l *Layer, value any) error {
	if value == nil {
		l.SetS("fillGradient", nil)
		return nil
	}
	if l.Type != "shape" || (l.Shape != nil && l.Shape.Kind == "line") {
		return errf("fillGradient requires a filled shape")
	}
	g, ok := value.(map[string]any)
	if !ok {
		return errf("fillGradient must be an object or null")
	}
	if err := checkKeys(g, []string{"type", "angle", "stops"}); err != nil {
		return err
	}
	if g["type"] != "linear" {
		return errf(`fillGradient.type must be "linear"`)
	}
	angle := 180.0
	if v, exists := g["angle"]; exists {
		var valid bool
		angle, valid = v.(float64)
		if !valid || math.IsNaN(angle) || math.IsInf(angle, 0) {
			return errf("fillGradient.angle must be a finite number")
		}
	}
	stops, ok := g["stops"].([]any)
	if !ok || len(stops) != 2 {
		return errf("fillGradient.stops requires exactly two colour stops")
	}
	normalized := make([]any, 2)
	for i, raw := range stops {
		stop, ok := raw.(map[string]any)
		if !ok {
			return errf("fillGradient stop %d must be an object", i+1)
		}
		if err := checkKeys(stop, []string{"color", "opacity"}); err != nil {
			return err
		}
		color, ok := stop["color"].(string)
		if !ok || strings.TrimSpace(color) == "" {
			return errf("fillGradient stop %d needs a colour", i+1)
		}
		opacity := 1.0
		if v, exists := stop["opacity"]; exists {
			var valid bool
			opacity, valid = v.(float64)
			if !valid || math.IsNaN(opacity) || math.IsInf(opacity, 0) || opacity < 0 || opacity > 1 {
				return errf("fillGradient stop %d opacity must be between 0 and 1", i+1)
			}
		}
		normalized[i] = map[string]any{"color": strings.TrimSpace(color), "opacity": opacity}
	}
	l.SetS("fillGradient", map[string]any{"type": "linear", "angle": math.Mod(math.Mod(angle, 360)+360, 360), "stops": normalized})
	return nil
}
