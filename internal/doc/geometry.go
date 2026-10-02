package doc

import "math"

// Layer geometry — the same model the renderer (web/src/render/geometry.ts)
// uses, which in turn follows Fabric.js so documents from those days stay
// put: (x, y) is the box's top-left corner and the box turns about it; the
// box counts half the stroke on each side (a stroke of 1 when none is set,
// except images and groups); a slanted text's box is the box around the
// slanted shape; the content is drawn centred in the box.

// Mat is an affine transform [a b c d e f]: x' = a·x + c·y + e, y' = b·x + d·y + f.
type Mat [6]float64

func mul(m, n Mat) Mat {
	return Mat{
		m[0]*n[0] + m[2]*n[1],
		m[1]*n[0] + m[3]*n[1],
		m[0]*n[2] + m[2]*n[3],
		m[1]*n[2] + m[3]*n[3],
		m[0]*n[4] + m[2]*n[5] + m[4],
		m[1]*n[4] + m[3]*n[5] + m[5],
	}
}

func (m Mat) apply(x, y float64) (float64, float64) {
	return m[0]*x + m[2]*y + m[4], m[1]*x + m[3]*y + m[5]
}

func rot(deg float64) Mat {
	r := deg * math.Pi / 180
	c, s := math.Cos(r), math.Sin(r)
	return Mat{c, s, -s, c, 0, 0}
}

// Frame is how a layer's content is sized and deformed.
type Frame struct {
	CW, CH         float64 // content size in its own units
	ScaleX, ScaleY float64
	SkewX          float64 // Fabric's skewX, degrees (negative leans right)
	FlipX, FlipY   bool
	StrokeWidth    float64
}

func (f Frame) dims() Mat {
	sx, sy := f.ScaleX, f.ScaleY
	if f.FlipX {
		sx = -sx
	}
	if f.FlipY {
		sy = -sy
	}
	m := Mat{sx, 0, 0, sy, 0, 0}
	if f.SkewX != 0 {
		m = mul(m, Mat{1, 0, math.Tan(f.SkewX * math.Pi / 180), 1, 0, 0})
	}
	return m
}

// BoxSize is the layer's box on the board before rotation.
func (f Frame) BoxSize() (float64, float64) {
	dx, dy := f.CW+f.StrokeWidth, f.CH+f.StrokeWidth
	if f.SkewX == 0 {
		return dx * f.ScaleX, dy * f.ScaleY
	}
	m := f.dims()
	xs := make([]float64, 0, 4)
	ys := make([]float64, 0, 4)
	for _, p := range [][2]float64{{-dx / 2, -dy / 2}, {dx / 2, -dy / 2}, {-dx / 2, dy / 2}, {dx / 2, dy / 2}} {
		x, y := m.apply(p[0], p[1])
		xs = append(xs, x)
		ys = append(ys, y)
	}
	return spread(xs), spread(ys)
}

func spread(v []float64) float64 {
	lo, hi := v[0], v[0]
	for _, x := range v {
		lo, hi = math.Min(lo, x), math.Max(hi, x)
	}
	return hi - lo
}

// Centre of the layer's box on the board.
func Centre(l *Layer, f Frame) (float64, float64) {
	w, h := f.BoxSize()
	ox, oy := rot(l.Rotation).apply(w/2, h/2)
	return l.X + ox, l.Y + oy
}

// PlaceCentre moves a layer so its box is centred on (cx, cy).
func PlaceCentre(l *Layer, f Frame, cx, cy float64) {
	w, h := f.BoxSize()
	ox, oy := rot(l.Rotation).apply(w/2, h/2)
	l.X, l.Y = cx-ox, cy-oy
}

// ContentMatrix maps content units (centred) to the board.
func ContentMatrix(l *Layer, f Frame) Mat {
	cx, cy := Centre(l, f)
	return mul(mul(Mat{1, 0, 0, 1, cx, cy}, rot(l.Rotation)), f.dims())
}

// Rect is an axis-aligned box.
type Rect struct{ Left, Top, Width, Height float64 }

func (r Rect) Right() float64  { return r.Left + r.Width }
func (r Rect) Bottom() float64 { return r.Top + r.Height }

// Corners are the layer box's corners on the board: tl, tr, br, bl.
func Corners(l *Layer, f Frame) [4][2]float64 {
	cx, cy := Centre(l, f)
	w, h := f.BoxSize()
	m := mul(Mat{1, 0, 0, 1, cx, cy}, rot(l.Rotation))
	var out [4][2]float64
	for i, p := range [][2]float64{{-w / 2, -h / 2}, {w / 2, -h / 2}, {w / 2, h / 2}, {-w / 2, h / 2}} {
		x, y := m.apply(p[0], p[1])
		out[i] = [2]float64{x, y}
	}
	return out
}

// Bounds is the axis-aligned box around the layer on the board.
func Bounds(l *Layer, f Frame) Rect {
	c := Corners(l, f)
	lo := [2]float64{c[0][0], c[0][1]}
	hi := lo
	for _, p := range c {
		lo[0], lo[1] = math.Min(lo[0], p[0]), math.Min(lo[1], p[1])
		hi[0], hi[1] = math.Max(hi[0], p[0]), math.Max(hi[1], p[1])
	}
	return Rect{lo[0], lo[1], hi[0] - lo[0], hi[1] - lo[1]}
}

// FrameOf derives a layer's frame from the document (a text layer's height is
// the measured one the document keeps).
func FrameOf(l *Layer) Frame {
	switch l.Type {
	case "text":
		stretch := num(l.S("stretch"), 1)
		if stretch <= 0 {
			stretch = 1
		}
		sw := 1.0
		if s := str(l.S("stroke")); s != "" {
			sw = strokeWidthOf(s, l.S("strokeWidth"))
		}
		return Frame{CW: math.Max(1, l.Width/stretch), CH: math.Max(1, l.Height), ScaleX: stretch, ScaleY: 1, SkewX: -num(l.S("skew"), 0), StrokeWidth: sw}
	case "image":
		nw, nh := l.Width, l.Height
		if l.Image != nil {
			if l.Image.Crop != nil && l.Image.Crop.Width > 0 {
				nw, nh = l.Image.Crop.Width, l.Image.Crop.Height
			} else if l.Image.OriginalWidth > 0 {
				nw, nh = l.Image.OriginalWidth, l.Image.OriginalHeight
			}
		}
		nw, nh = math.Max(nw, 1), math.Max(nh, 1)
		w, h := l.Width, l.Height
		if w <= 0 {
			w = nw
		}
		if h <= 0 {
			h = nh
		}
		return Frame{CW: nw, CH: nh, ScaleX: w / nw, ScaleY: h / nh, FlipX: boolean(l.S("flipX"), false), FlipY: boolean(l.S("flipY"), false)}
	case "group":
		b := MembersBox(l.Children)
		bw, bh := math.Max(b.Width, 1), math.Max(b.Height, 1)
		w, h := l.Width, l.Height
		if w <= 0 {
			w = bw
		}
		if h <= 0 {
			h = bh
		}
		return Frame{CW: bw, CH: bh, ScaleX: w / bw, ScaleY: h / bh}
	}
	sw := 1.0
	if v := l.S("strokeWidth"); v != nil {
		sw = num(v, 0)
	}
	return Frame{CW: math.Max(0.5, l.Width), CH: math.Max(0.5, l.Height), ScaleX: 1, ScaleY: 1, StrokeWidth: sw}
}

// MembersBox is the box around a group's members, in the group's own units.
func MembersBox(children []*Layer) Rect {
	if len(children) == 0 {
		return Rect{}
	}
	r := Bounds(children[0], FrameOf(children[0]))
	left, top, right, bottom := r.Left, r.Top, r.Right(), r.Bottom()
	for _, c := range children[1:] {
		b := Bounds(c, FrameOf(c))
		left, top = math.Min(left, b.Left), math.Min(top, b.Top)
		right, bottom = math.Max(right, b.Right()), math.Max(bottom, b.Bottom())
	}
	return Rect{left, top, right - left, bottom - top}
}

// Union is the box around several boxes.
func Union(rs []Rect) Rect {
	left, top, right, bottom := rs[0].Left, rs[0].Top, rs[0].Right(), rs[0].Bottom()
	for _, r := range rs[1:] {
		left, top = math.Min(left, r.Left), math.Min(top, r.Top)
		right, bottom = math.Max(right, r.Right()), math.Max(bottom, r.Bottom())
	}
	return Rect{left, top, right - left, bottom - top}
}

// strokeWidthOf reads "colour:width" or a separate width.
func strokeWidthOf(stroke string, width any) float64 {
	if w := num(width, 0); w > 0 {
		return w
	}
	if c, w, ok := splitStroke(stroke); ok && c != "" {
		return w
	}
	return 1
}

// splitStroke splits "colour:width" (an rgba() colour has no colon of its own).
func splitStroke(s string) (string, float64, bool) {
	i := lastIndex(s, ':')
	if i > 0 && i > lastIndex(s, ')') {
		if w := num(s[i+1:], math.NaN()); !math.IsNaN(w) {
			return s[:i], w, true
		}
	}
	return s, 1, false
}

func lastIndex(s string, c byte) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == c {
			return i
		}
	}
	return -1
}
