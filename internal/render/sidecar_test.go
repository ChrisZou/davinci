package render

import (
	"bytes"
	"davinci/internal/doc"
	"davinci/web"
	"image"
	"image/color"
	"image/png"
	"os/exec"
	"testing"

	xwebp "golang.org/x/image/webp"
)

func TestWebPQualityAndAlpha(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 64, 48))
	for y := 0; y < 48; y++ {
		for x := 0; x < 64; x++ {
			src.SetNRGBA(x, y, color.NRGBA{uint8(x*7 + y*3), uint8(x*5 + y*11), uint8(x*17 + y*13), 255})
		}
	}
	src.SetNRGBA(0, 0, color.NRGBA{})
	var pngBytes bytes.Buffer
	if err := png.Encode(&pngBytes, src); err != nil {
		t.Fatal(err)
	}
	low, err := toWebP(pngBytes.Bytes(), 0.3)
	if err != nil {
		t.Fatal(err)
	}
	high, err := toWebP(pngBytes.Bytes(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(low, high) {
		t.Fatal("quality did not change encoding")
	}
	for _, data := range [][]byte{low, high} {
		decoded, err := xwebp.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		if decoded.Bounds() != src.Bounds() {
			t.Fatal(decoded.Bounds())
		}
		_, _, _, alpha := decoded.At(0, 0).RGBA()
		if alpha != 0 {
			t.Fatal("lost alpha")
		}
	}
	if _, err := toWebP([]byte("not an image"), 0.8); err == nil {
		t.Fatal("invalid PNG accepted")
	}
}

func TestSidecarExportFormats(t *testing.T) {
	if _, _, err := web.RendererFiles(); err != nil {
		t.Skip("run ./start.sh build first")
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node is required")
	}
	s := New(t.TempDir(), func() string { return "http://127.0.0.1:1" }, t.Logf)
	defer s.Close()
	b := doc.Blank(24, 16).ActiveBoard()
	b.Canvas.Background = "#00ff00"
	for _, format := range []string{"png", "jpg", "webp"} {
		for _, transparent := range []bool{false, true} {
			data, err := s.Render(b, doc.RenderOptions{Format: format, Scale: 3, Quality: 0.8, Transparent: transparent})
			if err != nil {
				t.Fatalf("%s: %v", format, err)
			}
			decoded, gotFormat, err := image.Decode(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			wantFormat := format
			if format == "jpg" {
				wantFormat = "jpeg"
			}
			if gotFormat != wantFormat || decoded.Bounds().Dx() != 72 || decoded.Bounds().Dy() != 48 {
				t.Fatal(gotFormat, decoded.Bounds())
			}
			r, g, blue, alpha := decoded.At(0, 0).RGBA()
			if transparent && format != "jpg" && alpha != 0 {
				t.Fatalf("%s lost transparency", format)
			}
			if transparent && format == "jpg" && (r < 65000 || g < 65000 || blue < 65000) {
				t.Fatal("JPEG did not flatten to white")
			}
			if !transparent && (g < 60000 || r > 5000 || blue > 5000) {
				t.Fatal("lost board background")
			}
		}
	}
}

func TestSidecarSelectedLayers(t *testing.T) {
	if _, _, err := web.RendererFiles(); err != nil {
		t.Skip("run ./start.sh build first")
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node is required")
	}
	s := New(t.TempDir(), func() string { return "http://127.0.0.1:1" }, t.Logf)
	defer s.Close()
	b := doc.Blank(40, 30).ActiveBoard()
	b.Layers = []*doc.Layer{{ID: "chosen", Type: "shape", X: 5, Y: 6, Width: 10, Height: 8, Opacity: 1, Visible: true, Shape: &doc.ShapeRef{Kind: "rect"}, Style: map[string]any{"fill": "#ff0000", "strokeWidth": 0}}}
	data, err := s.Render(b, doc.RenderOptions{Format: "png", Scale: 2, Quality: 0.92, IDs: []string{"chosen"}})
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 20 || img.Bounds().Dy() != 16 {
		t.Fatal(img.Bounds())
	}
	r, g, blue, _ := img.At(10, 8).RGBA()
	if r < 65000 || g > 0 || blue > 0 {
		t.Fatal("selected layer not rendered")
	}
}
