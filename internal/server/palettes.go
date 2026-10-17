package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"davinci/internal/doc"
	"github.com/labstack/echo/v4"
)

const palettesDDL = `CREATE TABLE IF NOT EXISTS palettes (
 id TEXT PRIMARY KEY, name TEXT NOT NULL,
 bg TEXT NOT NULL, text TEXT NOT NULL, highlight TEXT NOT NULL, text2 TEXT NOT NULL, accent TEXT NOT NULL,
 created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL
);`

type Palette struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	doc.PaletteColors
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type PaletteDraft struct {
	Name string `json:"name"`
	doc.PaletteColors
}

func scanPalette(row interface{ Scan(...any) error }) (*Palette, error) {
	var p Palette
	var created, updated int64
	err := row.Scan(&p.ID, &p.Name, &p.BG, &p.Text, &p.Highlight, &p.Text2, &p.Accent, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	p.CreatedAt, p.UpdatedAt = fromDB(created), fromDB(updated)
	return &p, nil
}

const paletteColumns = `id,name,bg,text,highlight,text2,accent,created_at,updated_at`

func (s *Store) Palette(id string) (*Palette, error) {
	return scanPalette(s.db.QueryRow(`SELECT `+paletteColumns+` FROM palettes WHERE id=?`, id))
}
func (s *Store) Palettes() ([]Palette, error) {
	rows, err := s.db.Query(`SELECT ` + paletteColumns + ` FROM palettes ORDER BY created_at DESC,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Palette{}
	for rows.Next() {
		p, err := scanPalette(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}
func (d *PaletteDraft) normalize() error {
	d.Name = strings.TrimSpace(d.Name)
	if d.Name == "" || len([]rune(d.Name)) > 120 {
		return fmt.Errorf("配色名称须为 1–120 个字")
	}
	return d.PaletteColors.Normalize()
}
func (s *Store) AddPalette(d PaletteDraft) (*Palette, error) {
	if err := d.normalize(); err != nil {
		return nil, err
	}
	id, err := newID("pal")
	if err != nil {
		return nil, err
	}
	now := dbTime(time.Now())
	_, err = s.db.Exec(`INSERT INTO palettes (`+paletteColumns+`) VALUES (?,?,?,?,?,?,?,?,?)`, id, d.Name, d.BG, d.Text, d.Highlight, d.Text2, d.Accent, now, now)
	if err != nil {
		return nil, err
	}
	return s.Palette(id)
}
func (s *Store) PatchPalette(id string, patch map[string]string) (*Palette, error) {
	// Validate every value before a single UPDATE, and only update supplied columns:
	// concurrent edits to different roles cannot overwrite one another.
	set, args := []string{}, []any{}
	for _, key := range []string{"name", "bg", "text", "highlight", "text2", "accent"} {
		v, ok := patch[key]
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		if key == "name" {
			if v == "" || len([]rune(v)) > 120 {
				return nil, fmt.Errorf("配色名称须为 1–120 个字")
			}
		} else {
			c := doc.PaletteColors{BG: v, Text: v, Highlight: v, Text2: v, Accent: v}
			if err := c.Normalize(); err != nil {
				return nil, fmt.Errorf("%s 必须是 #RRGGBB 色值", key)
			}
			v = c.BG
		}
		set = append(set, key+"=?")
		args = append(args, v)
	}
	if len(set) != len(patch) || len(set) == 0 {
		return nil, fmt.Errorf("请提供 name、bg、text、highlight、text2 或 accent")
	}
	set = append(set, "updated_at=?")
	args = append(args, dbTime(time.Now()), id)
	result, err := s.db.Exec(`UPDATE palettes SET `+strings.Join(set, ",")+` WHERE id=?`, args...)
	if err != nil {
		return nil, err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return nil, ErrNotFound
	}
	return s.Palette(id)
}
func (s *Store) DeletePalette(id string) error {
	r, err := s.db.Exec(`DELETE FROM palettes WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
func (v services) PaletteColors(id string) (doc.PaletteColors, error) {
	p, err := v.s.store.Palette(id)
	if err != nil {
		return doc.PaletteColors{}, fmt.Errorf("找不到配色 %q", id)
	}
	return p.PaletteColors, nil
}

// Route descriptions and mounted handlers share this table so schema cannot omit an endpoint.
type paletteRoute struct {
	Method   string `json:"method"`
	Path     string `json:"path"`
	Summary  string `json:"summary"`
	Request  string `json:"request,omitempty"`
	Response string `json:"response"`
	handler  func(*Server) echo.HandlerFunc
}

var paletteRoutes = []paletteRoute{
	{"GET", "/palettes", "列出全局配色", "", "{ok,palettes:[{id,name,bg,text,highlight,text2,accent,createdAt,updatedAt}]}", func(s *Server) echo.HandlerFunc { return s.handlePalettes }},
	{"POST", "/palettes/analyze", "解析图片，不保存", "multipart file（PNG/JPEG/GIF/WebP，≤20 MB、≤2000万像素）, name?", "{ok,palette:{name,bg,text,highlight,text2,accent}}", func(s *Server) echo.HandlerFunc { return s.handleAnalyzePalette }},
	{"POST", "/palettes", "保存配色", "JSON {name,bg,text,highlight,text2,accent}；五色均为 #RRGGBB", "{ok,palette}", func(s *Server) echo.HandlerFunc { return s.handleCreatePalette }},
	{"GET", "/palettes/:id", "配色详情", "", "{ok,palette}", func(s *Server) echo.HandlerFunc { return s.handlePalette }},
	{"PATCH", "/palettes/:id", "重命名或修改颜色", "JSON {name?,bg?,text?,highlight?,text2?,accent?}", "{ok,palette}", func(s *Server) echo.HandlerFunc { return s.handlePatchPalette }},
	{"DELETE", "/palettes/:id", "删除配色", "", "{ok}", func(s *Server) echo.HandlerFunc { return s.handleDeletePalette }},
	{"POST", "/projects/:id/palettes/:palette/apply", "应用到当前或指定画板，一步撤销", "JSON {board?}，画板 id、名称或序号（字符串）", "{ok,data,document,revision,history}", func(s *Server) echo.HandlerFunc { return s.handleApplyPalette }},
}

func (s *Server) registerPalettes(api *echo.Group) {
	for _, r := range paletteRoutes {
		api.Add(r.Method, r.Path, r.handler(s))
	}
}
func paletteError(c echo.Context, err error) error {
	if errors.Is(err, ErrNotFound) {
		return notFound(c, "找不到配色")
	}
	return errJSON(c, err)
}
func (s *Server) handlePalettes(c echo.Context) error {
	ps, err := s.store.Palettes()
	if err != nil {
		return errJSON(c, err)
	}
	return c.JSON(200, map[string]any{"ok": true, "palettes": ps})
}
func (s *Server) handlePalette(c echo.Context) error {
	p, err := s.store.Palette(c.Param("id"))
	if err != nil {
		return paletteError(c, err)
	}
	return c.JSON(200, map[string]any{"ok": true, "palette": p})
}
func decodePaletteJSON(c echo.Context, v any) error {
	dec := json.NewDecoder(io.LimitReader(c.Request().Body, 8193))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return fmt.Errorf("只接受一个 JSON 对象")
	}
	return nil
}
func (s *Server) handleCreatePalette(c echo.Context) error {
	var d PaletteDraft
	if err := decodePaletteJSON(c, &d); err != nil {
		return badRequest(c, "配色数据无效："+err.Error())
	}
	if err := d.normalize(); err != nil {
		return badRequest(c, err.Error())
	}
	p, err := s.store.AddPalette(d)
	if err != nil {
		return errJSON(c, err)
	}
	return c.JSON(201, map[string]any{"ok": true, "palette": p})
}
func (s *Server) handlePatchPalette(c echo.Context) error {
	var patch map[string]string
	if err := decodePaletteJSON(c, &patch); err != nil {
		return badRequest(c, "配色数据无效："+err.Error())
	}
	p, err := s.store.PatchPalette(c.Param("id"), patch)
	if errors.Is(err, ErrNotFound) {
		return notFound(c, "找不到配色")
	}
	if err != nil {
		return badRequest(c, err.Error())
	}
	return c.JSON(200, map[string]any{"ok": true, "palette": p})
}
func (s *Server) handleDeletePalette(c echo.Context) error {
	if err := s.store.DeletePalette(c.Param("id")); err != nil {
		return paletteError(c, err)
	}
	return c.JSON(200, map[string]any{"ok": true})
}
func (s *Server) handleAnalyzePalette(c echo.Context) error {
	c.Request().Body = http.MaxBytesReader(c.Response(), c.Request().Body, maxPaletteBytes+(1<<20))
	if err := c.Request().ParseMultipartForm(1 << 20); err != nil {
		return badRequest(c, "请上传不超过 20 MB 的图片")
	}
	if c.Request().MultipartForm != nil {
		defer c.Request().MultipartForm.RemoveAll()
	}
	file, header, err := c.Request().FormFile("file")
	if err != nil {
		return badRequest(c, "请选择图片")
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, maxPaletteBytes+1))
	if err != nil {
		return badRequest(c, "读取图片失败")
	}
	colors, err := ExtractPalette(raw)
	if err != nil {
		return badRequest(c, err.Error())
	}
	name := strings.TrimSpace(c.FormValue("name"))
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(header.Filename), filepath.Ext(header.Filename))
	}
	if name == "" {
		name = "新配色"
	}
	runes := []rune(name)
	if len(runes) > 120 {
		name = string(runes[:120])
	}
	return c.JSON(200, map[string]any{"ok": true, "palette": PaletteDraft{Name: name, PaletteColors: colors}})
}
func (s *Server) handleApplyPalette(c echo.Context) error {
	if _, err := s.store.Palette(c.Param("palette")); err != nil {
		return paletteError(c, err)
	}
	var req struct {
		Board string `json:"board"`
	}
	if err := decodePaletteJSON(c, &req); err != nil {
		return badRequest(c, "请提供 JSON {board?}")
	}
	raw, _ := json.Marshal(map[string]any{"type": "applyPalette", "id": c.Param("palette"), "board": req.Board})
	out, err := s.Exec(c.Param("id"), raw)
	if err != nil {
		return errJSON(c, err)
	}
	if !out.OK {
		return c.JSON(400, out)
	}
	return c.JSON(200, out)
}
