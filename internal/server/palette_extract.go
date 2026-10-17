package server

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"math"
	"sort"

	"davinci/internal/doc"
)

const maxPaletteBytes = 20 << 20
const maxPalettePixels = 20_000_000

type paletteRGB struct{ r, g, b float64 }
type paletteCluster struct {
	paletteRGB
	weight float64
}

func (c paletteRGB) hex() string {
	return fmt.Sprintf("#%02x%02x%02x", int(math.Round(c.r)), int(math.Round(c.g)), int(math.Round(c.b)))
}
func (c paletteRGB) saturation() float64 {
	hi, lo := max(c.r, c.g, c.b), min(c.r, c.g, c.b)
	if hi == 0 {
		return 0
	}
	return (hi - lo) / hi
}
func (c paletteRGB) luminance() float64 {
	linear := func(x float64) float64 {
		x /= 255
		if x <= .04045 {
			return x / 12.92
		}
		return math.Pow((x+.055)/1.055, 2.4)
	}
	return .2126*linear(c.r) + .7152*linear(c.g) + .0722*linear(c.b)
}
func paletteContrast(a, b paletteRGB) float64 {
	x, y := a.luminance(), b.luminance()
	return (max(x, y) + .05) / (min(x, y) + .05)
}
func paletteDistance(a, b paletteRGB) float64 {
	return math.Pow(a.r-b.r, 2) + math.Pow(a.g-b.g, 2) + math.Pow(a.b-b.b, 2)
}
func paletteMix(a, b paletteRGB, t float64) paletteRGB {
	return paletteRGB{a.r*(1-t) + b.r*t, a.g*(1-t) + b.g*t, a.b*(1-t) + b.b*t}
}

// ExtractPalette decodes bounded raster input, samples at most 128×128 pixels,
// quantizes to a 5-bit histogram, then runs deterministic weighted k-means.
// No files, assets or database rows are created by analysis.
func ExtractPalette(raw []byte) (doc.PaletteColors, error) {
	if len(raw) > maxPaletteBytes {
		return doc.PaletteColors{}, fmt.Errorf("图片不能超过 20 MB")
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return doc.PaletteColors{}, fmt.Errorf("无法解析图片，请使用 PNG、JPEG、GIF 或 WebP")
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > maxPalettePixels {
		return doc.PaletteColors{}, fmt.Errorf("图片不能超过 2000 万像素")
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return doc.PaletteColors{}, fmt.Errorf("图片解码失败：%w", err)
	}
	hist := map[int]*paletteCluster{}
	bounds := img.Bounds()
	nx, ny := min(128, bounds.Dx()), min(128, bounds.Dy())
	for y := 0; y < ny; y++ {
		for x := 0; x < nx; x++ {
			px, py := bounds.Min.X+(2*x+1)*bounds.Dx()/(2*nx), bounds.Min.Y+(2*y+1)*bounds.Dy()/(2*ny)
			c := color.NRGBAModel.Convert(img.At(px, py)).(color.NRGBA)
			if c.A < 16 {
				continue
			}
			key := int(c.R>>3)<<10 | int(c.G>>3)<<5 | int(c.B>>3)
			h := hist[key]
			if h == nil {
				h = &paletteCluster{}
				hist[key] = h
			}
			w := float64(c.A) / 255
			h.r += float64(c.R) * w
			h.g += float64(c.G) * w
			h.b += float64(c.B) * w
			h.weight += w
		}
	}
	if len(hist) == 0 {
		return doc.PaletteColors{}, fmt.Errorf("图片没有可见颜色")
	}
	keys := make([]int, 0, len(hist))
	for k := range hist {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	points := make([]paletteCluster, 0, len(keys))
	for _, k := range keys {
		h := *hist[k]
		h.r /= h.weight
		h.g /= h.weight
		h.b /= h.weight
		points = append(points, h)
	}
	sort.SliceStable(points, func(i, j int) bool { return points[i].weight > points[j].weight })
	centers := []paletteCluster{points[0]}
	for len(centers) < min(8, len(points)) {
		best, score := -1, 0.0
		for i, p := range points {
			d := math.Inf(1)
			for _, c := range centers {
				d = min(d, paletteDistance(p.paletteRGB, c.paletteRGB))
			}
			if s := d * math.Sqrt(p.weight); s > score {
				best, score = i, s
			}
		}
		if best < 0 || score == 0 {
			break
		}
		centers = append(centers, points[best])
	}
	for iter := 0; iter < 16; iter++ {
		sums := make([]paletteCluster, len(centers))
		for _, p := range points {
			at, d := 0, math.Inf(1)
			for i, c := range centers {
				if next := paletteDistance(p.paletteRGB, c.paletteRGB); next < d {
					at, d = i, next
				}
			}
			s := &sums[at]
			s.r += p.r * p.weight
			s.g += p.g * p.weight
			s.b += p.b * p.weight
			s.weight += p.weight
		}
		for i, s := range sums {
			if s.weight > 0 {
				s.r /= s.weight
				s.g /= s.weight
				s.b /= s.weight
				centers[i] = s
			} else {
				centers[i].weight = 0
			}
		}
	}
	sort.SliceStable(centers, func(i, j int) bool { return centers[i].weight > centers[j].weight })
	return assignPaletteRoles(centers), nil
}

func assignPaletteRoles(cs []paletteCluster) doc.PaletteColors {
	bg := cs[0].paletteRGB
	score := -1.0
	for _, c := range cs {
		if s := c.weight * (1 + .25*(1-c.saturation())); s > score {
			bg, score = c.paletteRGB, s
		}
	}
	black, white := paletteRGB{}, paletteRGB{255, 255, 255}
	text := black
	if paletteContrast(bg, white) > paletteContrast(bg, black) {
		text = white
	}
	// Keep a source colour if it already has strong readability.
	best := 0.0
	for _, c := range cs {
		if ratio := paletteContrast(bg, c.paletteRGB); c.weight > 0 && ratio >= 7 && ratio > best {
			text, best = c.paletteRGB, ratio
		}
	}
	highlight := paletteRGB{255 - bg.r, 255 - bg.g, 255 - bg.b}
	score = -1
	for _, c := range cs {
		if c.weight == 0 || paletteDistance(c.paletteRGB, bg) < 1600 || paletteDistance(c.paletteRGB, text) < 400 {
			continue
		}
		if s := c.saturation(); s > .15 && s > score {
			highlight, score = c.paletteRGB, s
		}
	}
	if score < 0 { // A monochrome photo still gets a chromatic highlight.
		if bg.luminance() > .4 {
			highlight = paletteRGB{205, 72, 38}
		} else {
			highlight = paletteRGB{255, 193, 71}
		}
	}
	text2 := text
	best = 0
	for _, c := range cs {
		r := paletteContrast(bg, c.paletteRGB)
		if c.weight > 0 && r >= 4.5 && r < paletteContrast(bg, text)*.85 && r > best {
			text2, best = c.paletteRGB, r
		}
	}
	if best == 0 {
		// Find the quietest readable shade between background and primary text.
		for i := 1; i <= 1000; i++ {
			c := paletteMix(bg, text, float64(i)/1000)
			if paletteContrast(bg, c) >= 4.55 {
				text2 = c
				break
			}
		}
	}
	// A hue rotation must also differ from the background (a red/green
	// two-colour image must not derive a green accent on a green background).
	accent := paletteRGB{highlight.b, highlight.r, highlight.g}
	other := paletteRGB{highlight.g, highlight.b, highlight.r}
	separation := func(c paletteRGB) float64 {
		return min(paletteDistance(c, bg), paletteDistance(c, highlight), paletteDistance(c, text), paletteDistance(c, text2))
	}
	if separation(other) > separation(accent) {
		accent = other
	}
	score = -1
	for _, c := range cs {
		if c.weight == 0 || paletteDistance(c.paletteRGB, bg) < 1600 || paletteDistance(c.paletteRGB, text) < 1600 || paletteDistance(c.paletteRGB, text2) < 900 || paletteDistance(c.paletteRGB, highlight) < 2500 {
			continue
		}
		if s := paletteDistance(c.paletteRGB, highlight) * (.25 + c.saturation()); s > score {
			accent, score = c.paletteRGB, s
		}
	}
	return doc.PaletteColors{BG: bg.hex(), Text: text.hex(), Highlight: highlight.hex(), Text2: text2.hex(), Accent: accent.hex()}
}
