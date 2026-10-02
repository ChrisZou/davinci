package server

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// layerRow is the server's summary of one layer. The document is owned by the
// frontend, so these field names follow the shape commands.ts emits; anything
// missing is reported as a zero value rather than an error.
type LayerRow struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Type     string  `json:"type"`
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	Width    float64 `json:"width"`
	Height   float64 `json:"height"`
	Rotation float64 `json:"rotation"`
	Opacity  float64 `json:"opacity"`
	Visible  bool    `json:"visible"`
	Locked   bool    `json:"locked"`
	// Index is the stacking index setZIndex takes: 0 is the backmost layer.
	// Rows themselves come back front-to-back, so the frontmost layer is
	// rows[0] and its index is len(rows)-1. Keep this in step with
	// Editor.layerRows() in the frontend — both are AI-facing listings and
	// must not disagree about which end is the top.
	Index   int    `json:"index"`
	Preview string `json:"preview,omitempty"` // text content or image URL
}

// layerRows extracts a layer summary straight out of a document, front-to-back
// (the same order the editor's layer panel shows, and the reverse of the
// document's own back-to-front array).
//
// board picks one board of the project (id, name or 1-based number); empty
// means the active board.
func LayerRows(doc json.RawMessage, board string) ([]LayerRow, error) {
	raw, err := boardLayers(doc, board)
	if err != nil {
		return nil, err
	}
	var d struct {
		Layers []map[string]any
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &d.Layers); err != nil {
			return nil, err
		}
	}
	out := make([]LayerRow, 0, len(d.Layers))
	for i, l := range d.Layers {
		out = append(out, LayerRow{
			ID:       str(l["id"]),
			Name:     str(l["name"]),
			Type:     str(l["type"]),
			X:        num(l["x"]),
			Y:        num(l["y"]),
			Width:    num(l["width"]),
			Height:   num(l["height"]),
			Rotation: num(l["rotation"]),
			Opacity:  num(l["opacity"]),
			Visible:  boolean(l["visible"], true),
			Locked:   boolean(l["locked"], false),
			Index:    i,
			Preview:  firstNonEmpty(str(l["text"]), str(l["src"])),
		})
	}
	// Reverse so rows[0] is the frontmost layer, matching the layer panel and
	// the frontend's listLayers. The indices stay as-they-were because they
	// address the document's own array.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func num(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case json.Number:
		f, _ := n.Float64()
		return f
	}
	return 0
}

func boolean(v any, def bool) bool {
	switch b := v.(type) {
	case bool:
		return b
	case nil:
		return def
	}
	return def
}

// doc reads a project's document for a summary row; failures yield the columns.
func (p ProjectSummary) doc(s *Server) json.RawMessage {
	full, err := s.store.GetProject(p.ID)
	if err != nil || full == nil {
		return nil
	}
	return full.Document
}

// writeFile writes b to path, creating parent directories.
func writeFile(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// fontFamiliesOf reports the families a font file declares, reading only its
// name table.
func fontFamiliesOf(path string) []string {
	faces, err := readFontFaces(path)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, f := range faces {
		if f.family == "" || seen[f.family] {
			continue
		}
		seen[f.family] = true
		out = append(out, f.family)
	}
	return out
}
