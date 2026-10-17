package server

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"testing"
)

// Supersample a small cover with subpixel-aligned shapes and a curved edge.
// The secondary text is under 1% of the image, far smaller than the other inks.
func antialiasedPaletteImage(t *testing.T, size int) []byte {
	t.Helper()
	inks := []paletteRGB{rgbHex("#0b1f3a"), rgbHex("#f5f7fa"), rgbHex("#3da9fc"), rgbHex("#ffd166"), rgbHex("#9fb3c8")}
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			var sum paletteRGB
			for sy := 0; sy < 4; sy++ {
				for sx := 0; sx < 4; sx++ {
					px := (float64(x) + (float64(sx)+.5)/4) * 128 / float64(size)
					py := (float64(y) + (float64(sy)+.5)/4) * 128 / float64(size)
					ink := inks[0]
					switch {
					case px > 10.3 && px < 70.7 && ((py > 15.4 && py < 25.6) || (py > 32.4 && py < 42.6)):
						ink = inks[1]
					case px > 78.3 && px < 116.7 && py > 12.3 && py < 70.7:
						ink = inks[2]
					case math.Hypot(px-88.35, py-95.65) < 19.4:
						ink = inks[3]
					case px > 12.3 && px < 38.7 && ((py > 90.3 && py < 92.7) || (py > 98.3 && py < 100.7)):
						ink = inks[4]
					}
					sum.r += ink.r / 16
					sum.g += ink.g / 16
					sum.b += ink.b / 16
				}
			}
			img.SetNRGBA(x, y, color.NRGBA{uint8(math.Round(sum.r)), uint8(math.Round(sum.g)), uint8(math.Round(sum.b)), 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestExtractPaletteAntialiasedSmallInk(t *testing.T) {
	for _, size := range []int{128, 384, 640} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			p, err := ExtractPalette(antialiasedPaletteImage(t, size))
			if err != nil {
				t.Fatal(err)
			}
			got := []string{p.BG, p.Text, p.Highlight, p.Text2, p.Accent}
			for _, want := range []string{"#0b1f3a", "#f5f7fa", "#3da9fc", "#ffd166", "#9fb3c8"} {
				found := false
				for _, c := range got {
					found = found || paletteDistance(rgbHex(c), rgbHex(want)) <= 8*8
				}
				if !found {
					t.Errorf("missing source ink %s (or replaced by an edge mixture): %+v", want, p)
				}
			}
			for _, c := range got {
				if paletteDistance(rgbHex(c), rgbHex("#928152")) < 20*20 {
					t.Errorf("selected yellow/background mixture: %+v", p)
				}
			}
		})
	}
}

func TestFilterPaletteTransitionsPreservesFlatShades(t *testing.T) {
	a, b := rgbHex("#0b1f3a"), rgbHex("#ffd166")
	middle := paletteMix(a, b, .55)
	points := []paletteCluster{
		{paletteRGB: a, weight: 1000, flatWeight: 900},
		{paletteRGB: b, weight: 200, flatWeight: 180},
		{paletteRGB: middle, weight: 10},
		{paletteRGB: rgbHex("#9fb3c8"), weight: 5},
	}
	got := filterPaletteTransitions(points)
	if len(got) != 3 || got[2].paletteRGB != points[3].paletteRGB {
		t.Fatalf("should remove only the transition: %+v", got)
	}
	points[2].flatWeight = 8
	if got := filterPaletteTransitions(points); len(got) != 4 {
		t.Fatalf("an intentional flat intermediate shade must survive: %+v", got)
	}
}
