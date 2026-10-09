// Package doc is the davinci document and every command that changes it.
//
// The server owns the document: an edit — from a person in the editor, from
// the CLI, from an AI agent — is a command applied here, and the result is
// saved and pushed to every open page, which only draws it. This package has
// no I/O of its own: text measurement, image sizes and rendering come in
// through the Services interface, so the commands can be tested as plain
// functions.
package doc

import (
	"encoding/json"
	"strings"
)

// Project is a stored document: one or more boards (画板).
type Project struct {
	Version int      `json:"version"`
	Active  string   `json:"active,omitempty"`
	Boards  []*Board `json:"boards"`
}

// Board is one page: a canvas and its layers, back to front.
type Board struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Canvas Canvas   `json:"canvas"`
	Layers []*Layer `json:"layers"`
}

// Canvas is a board's size and background.
type Canvas struct {
	Width           float64 `json:"width"`
	Height          float64 `json:"height"`
	Background      string  `json:"background,omitempty"`
	BackgroundImage string  `json:"backgroundImage,omitempty"`
}

// Layer is one element on a board.
//
// x, y is the top-left corner of the layer's box, which turns about that
// corner by Rotation degrees. A text layer's Height is derived — measured by
// the renderer after every change — and kept here so listings and alignment
// can use it without a renderer.
type Layer struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Type     string         `json:"type"` // text | image | shape | group
	X        float64        `json:"x"`
	Y        float64        `json:"y"`
	Width    float64        `json:"width"`
	Height   float64        `json:"height"`
	Rotation float64        `json:"rotation"`
	Opacity  float64        `json:"opacity"`
	Visible  bool           `json:"visible"`
	Locked   bool           `json:"locked"`
	Text     *string        `json:"text,omitempty"`
	Style    map[string]any `json:"style,omitempty"`
	Image    *ImageRef      `json:"image,omitempty"`
	Children []*Layer       `json:"children,omitempty"`
	Shape    *ShapeRef      `json:"shape,omitempty"`
}

// ImageRef is an image layer's picture.
type ImageRef struct {
	URL            string    `json:"url"`
	SHA            string    `json:"sha,omitempty"`
	OriginalWidth  float64   `json:"originalWidth,omitempty"`
	OriginalHeight float64   `json:"originalHeight,omitempty"`
	Crop           *CropRect `json:"crop,omitempty"`
	// Original is the picture before its background was removed, so the
	// cut-out can be undone later (or redone with another model).
	Original string `json:"original,omitempty"`
}

// CropRect is the visible part of a picture, in its own pixels.
type CropRect struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// ShapeRef says which shape a shape layer is.
type ShapeRef struct {
	Kind string `json:"kind"` // rect | ellipse | triangle | line
}

// Parse reads a stored document: a project of boards, or a single-page
// document from before boards existed, which becomes a project of one.
func Parse(raw []byte) (*Project, error) {
	var probe struct {
		Boards json.RawMessage `json:"boards"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, err
	}
	if len(probe.Boards) > 0 && string(probe.Boards) != "null" {
		var p Project
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		p.normalize()
		return &p, nil
	}
	var single struct {
		Canvas Canvas   `json:"canvas"`
		Layers []*Layer `json:"layers"`
	}
	if err := json.Unmarshal(raw, &single); err != nil {
		return nil, err
	}
	b := &Board{ID: NewID("board"), Name: "画板 1", Canvas: single.Canvas, Layers: single.Layers}
	p := &Project{Version: 2, Active: b.ID, Boards: []*Board{b}}
	p.normalize()
	return p, nil
}

// Blank is a new project of one empty board.
func Blank(width, height float64) *Project {
	b := &Board{ID: NewID("board"), Name: "画板 1", Canvas: Canvas{Width: width, Height: height, Background: "#ffffff"}, Layers: []*Layer{}}
	return &Project{Version: 2, Active: b.ID, Boards: []*Board{b}}
}

func (p *Project) normalize() {
	p.Version = 2
	if len(p.Boards) == 0 {
		b := &Board{ID: NewID("board"), Name: "画板 1", Canvas: Canvas{Width: 1242, Height: 1656, Background: "#ffffff"}}
		p.Boards = []*Board{b}
	}
	for i, b := range p.Boards {
		if b.ID == "" {
			b.ID = NewID("board")
		}
		if strings.TrimSpace(b.Name) == "" {
			b.Name = "画板 " + itoa(i+1)
		}
		if b.Canvas.Width <= 0 {
			b.Canvas.Width = 1242
		}
		if b.Canvas.Height <= 0 {
			b.Canvas.Height = 1656
		}
		if b.Layers == nil {
			b.Layers = []*Layer{}
		}
	}
	if p.Board(p.Active) == nil {
		p.Active = p.Boards[0].ID
	}
}

// JSON encodes the project as stored.
func (p *Project) JSON() []byte {
	b, _ := json.Marshal(p)
	return b
}

// Clone is a deep copy.
func (p *Project) Clone() *Project {
	var c Project
	_ = json.Unmarshal(p.JSON(), &c)
	return &c
}

// Board finds a board by id.
func (p *Project) Board(id string) *Board {
	for _, b := range p.Boards {
		if b.ID == id {
			return b
		}
	}
	return nil
}

// ActiveBoard is the board commands act on when they name none.
func (p *Project) ActiveBoard() *Board {
	if b := p.Board(p.Active); b != nil {
		return b
	}
	return p.Boards[0]
}

// FindBoard looks a board up by id (editor links leave off its "board_"),
// by name, or by its 1-based position.
func (p *Project) FindBoard(ref string) (*Board, int) {
	ref = strings.TrimSpace(ref)
	for i, b := range p.Boards {
		if b.ID == ref || b.ID == "board_"+ref {
			return b, i
		}
	}
	for i, b := range p.Boards {
		if b.Name == ref {
			return b, i
		}
	}
	if n, ok := atoi(ref); ok && n >= 1 && n <= len(p.Boards) {
		return p.Boards[n-1], n - 1
	}
	return nil, -1
}

// Find looks a layer up by id or by its unique name.
func (b *Board) Find(ref string) *Layer {
	for _, l := range b.Layers {
		if l.ID == ref {
			return l
		}
	}
	for _, l := range b.Layers {
		if l.Name == ref {
			return l
		}
	}
	return nil
}

// Index is a layer's position in the stack (0 = back), or -1.
func (b *Board) Index(id string) int {
	for i, l := range b.Layers {
		if l.ID == id {
			return i
		}
	}
	return -1
}

// UniqueName returns base, or base with a number, so names stay unique.
func (b *Board) UniqueName(base string) string {
	taken := map[string]bool{}
	for _, l := range b.Layers {
		taken[l.Name] = true
	}
	if !taken[base] {
		return base
	}
	for i := 2; i < 500; i++ {
		n := base + " " + itoa(i)
		if !taken[n] {
			return n
		}
	}
	return base + " " + itoa(len(b.Layers)+1)
}

// Clone is a deep copy of a layer.
func (l *Layer) Clone() *Layer {
	raw, _ := json.Marshal(l)
	var c Layer
	_ = json.Unmarshal(raw, &c)
	return &c
}

// S reads a style value.
func (l *Layer) S(key string) any {
	if l.Style == nil {
		return nil
	}
	return l.Style[key]
}

// SetS writes a style value; nil deletes it.
func (l *Layer) SetS(key string, v any) {
	if v == nil {
		if l.Style != nil {
			delete(l.Style, key)
			if len(l.Style) == 0 {
				l.Style = nil
			}
		}
		return
	}
	if l.Style == nil {
		l.Style = map[string]any{}
	}
	l.Style[key] = v
}

// TextOf is a text layer's text ("" for others).
func (l *Layer) TextOf() string {
	if l.Text == nil {
		return ""
	}
	return *l.Text
}
