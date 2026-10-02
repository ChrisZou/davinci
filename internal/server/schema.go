package server

import (
	"davinci/internal/doc"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// docShape is the server's minimal view of the frontend-owned document. Only
// canvas sizes and board ids are needed here; everything else is opaque JSON
// passed through. Two shapes exist: a project of boards (version 2), and the
// single-page document from before boards (version 1), which the editor
// upgrades the first time it opens it.
type docShape struct {
	Version int         `json:"version"`
	Canvas  canvasShape `json:"canvas"`
	Active  string      `json:"active"`
	Boards  []struct {
		ID     string          `json:"id"`
		Name   string          `json:"name"`
		Canvas canvasShape     `json:"canvas"`
		Layers json.RawMessage `json:"layers"`
	} `json:"boards"`
	Layers json.RawMessage `json:"layers"`
}

type canvasShape struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// canvasSizeOf reads the project's size — that of its first board, which is
// also what its thumbnail shows — tolerating a missing or partial block.
func canvasSizeOf(doc json.RawMessage) (int, int) {
	var d docShape
	if err := json.Unmarshal(doc, &d); err != nil {
		return 0, 0
	}
	if len(d.Boards) > 0 {
		return d.Boards[0].Canvas.Width, d.Boards[0].Canvas.Height
	}
	return d.Canvas.Width, d.Canvas.Height
}

// BoardInfo is one board of a project, as listings report it.
type BoardInfo struct {
	Index  int    `json:"index"` // 1-based, as "画板 2/5" reads
	ID     string `json:"id"`
	Name   string `json:"name"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Layers int    `json:"layers"`
	Active bool   `json:"active"`
}

// Boards lists a document's boards. A version-1 document is one board.
func Boards(doc json.RawMessage) []BoardInfo {
	var d docShape
	if err := json.Unmarshal(doc, &d); err != nil {
		return nil
	}
	if len(d.Boards) == 0 {
		var layers []json.RawMessage
		_ = json.Unmarshal(d.Layers, &layers)
		return []BoardInfo{{Index: 1, Name: "画板 1", Width: d.Canvas.Width, Height: d.Canvas.Height, Layers: len(layers), Active: true}}
	}
	active := d.Active
	if active == "" {
		active = d.Boards[0].ID
	}
	out := make([]BoardInfo, 0, len(d.Boards))
	for i, b := range d.Boards {
		var layers []json.RawMessage
		_ = json.Unmarshal(b.Layers, &layers)
		out = append(out, BoardInfo{Index: i + 1, ID: b.ID, Name: b.Name, Width: b.Canvas.Width, Height: b.Canvas.Height, Layers: len(layers), Active: b.ID == active})
	}
	return out
}

// boardLayers returns the layers array of one board: the one named by ref (id,
// name or 1-based number), or the active board when ref is empty.
func boardLayers(doc json.RawMessage, ref string) (json.RawMessage, error) {
	var d docShape
	if err := json.Unmarshal(doc, &d); err != nil {
		return nil, err
	}
	if len(d.Boards) == 0 {
		if ref != "" && ref != "1" && ref != "画板 1" {
			return nil, fmt.Errorf("no board %q — this project has one board", ref)
		}
		return d.Layers, nil
	}
	pick := -1
	if ref == "" {
		for i, b := range d.Boards {
			if b.ID == d.Active {
				pick = i
			}
		}
		if pick < 0 {
			pick = 0
		}
	} else {
		for i, b := range d.Boards {
			if b.ID == ref || (pick < 0 && b.Name == ref) {
				pick = i
			}
		}
		if n, err := strconv.Atoi(ref); pick < 0 && err == nil && n >= 1 && n <= len(d.Boards) {
			pick = n - 1
		}
	}
	if pick < 0 {
		names := make([]string, 0, len(d.Boards))
		for i, b := range d.Boards {
			names = append(names, fmt.Sprintf("%d:%s[%s]", i+1, b.Name, b.ID))
		}
		return nil, fmt.Errorf("no board %q — boards: %s", ref, strings.Join(names, ", "))
	}
	return d.Boards[pick].Layers, nil
}

// loadDocument reads a project's document and revision, falling back to a fresh
// canvas-sized document when the stored one is unusable.
func (s *Server) loadDocument(id string) (json.RawMessage, int64, error) {
	p, err := s.store.GetProject(id)
	if err != nil {
		return nil, 0, err
	}
	if p == nil {
		return nil, 0, errors.New("project not found: " + id)
	}
	if len(p.Document) == 0 || !json.Valid(p.Document) {
		w, h := p.Width, p.Height
		if w <= 0 || h <= 0 {
			w, h = 1080, 1080
		}
		doc, err := blankDocument(w, h)
		if err != nil {
			return nil, 0, err
		}
		return doc, p.Revision, nil
	}
	return p.Document, p.Revision, nil
}

// revision returns a project's revision counter.
func (s *Server) revision(id string) (int64, error) {
	p, err := s.store.GetProject(id)
	if err != nil || p == nil {
		return 0, err
	}
	return p.Revision, nil
}

// blankDocument is the minimal document shape the server can mint on its own.
// The frontend enriches it with defaults when it loads.
func blankDocument(width, height int) (json.RawMessage, error) {
	return doc.Blank(float64(width), float64(height)).JSON(), nil
}
