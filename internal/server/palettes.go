package server

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"davinci/internal/doc"
	"github.com/labstack/echo/v4"
)

const palettesDDL = `CREATE TABLE IF NOT EXISTS palettes (
 id TEXT PRIMARY KEY, name TEXT NOT NULL,
 bg TEXT NOT NULL, text TEXT NOT NULL, highlight TEXT NOT NULL, text2 TEXT NOT NULL, accent TEXT NOT NULL,
 image_url TEXT NOT NULL DEFAULT '',
 created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL
);`

type Palette struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	ImageURL string `json:"imageUrl"`
	doc.PaletteColors
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type PaletteDraft struct {
	Name     string `json:"name"`
	ImageURL string `json:"imageUrl"`
	doc.PaletteColors
}

func scanPalette(row interface{ Scan(...any) error }) (*Palette, error) {
	var p Palette
	var created, updated int64
	err := row.Scan(&p.ID, &p.Name, &p.BG, &p.Text, &p.Highlight, &p.Text2, &p.Accent, &p.ImageURL, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	p.CreatedAt, p.UpdatedAt = fromDB(created), fromDB(updated)
	return &p, nil
}

const paletteColumns = `id,name,bg,text,highlight,text2,accent,image_url,created_at,updated_at`

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
	if err := s.validatePaletteImage(d.ImageURL); err != nil {
		return nil, err
	}
	now := dbTime(time.Now())
	_, err = s.db.Exec(`INSERT INTO palettes (`+paletteColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?)`, id, d.Name, d.BG, d.Text, d.Highlight, d.Text2, d.Accent, d.ImageURL, now, now)
	if err != nil {
		return nil, err
	}
	return s.Palette(id)
}
func (s *Store) PatchPalette(id string, patch map[string]string) (*Palette, error) {
	if err := validatePalettePatch(patch); err != nil {
		return nil, err
	}
	set, args := []string{}, []any{}
	for key, v := range patch {
		column := key
		if key == "imageUrl" {
			if err := s.validatePaletteImage(v); err != nil {
				return nil, err
			}
			column = "image_url"
		}
		set = append(set, column+"=?")
		args = append(args, v)
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

// Only existing, content-addressed raster assets may be attached to a palette.
var paletteImageURL = regexp.MustCompile(`^/assets/([0-9a-f]{40})\.(png|jpg|jpeg|gif|webp)$`)

func (s *Store) validatePaletteImage(url string) error {
	if url == "" {
		return nil
	}
	m := paletteImageURL.FindStringSubmatch(url)
	if m == nil {
		return fmt.Errorf("原图必须是已上传的图片素材")
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM assets WHERE sha=? AND mime LIKE 'image/%'`, m[1]).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("原图素材不存在")
	}
	_, err := NewAssetStore(s.dir, s).Path(url)
	return err
}
func validatePalettePatch(patch map[string]string) error {
	if len(patch) == 0 {
		return fmt.Errorf("请提供名称、角色色或原图")
	}
	for key, value := range patch {
		v := strings.TrimSpace(value)
		switch key {
		case "name":
			if v == "" || len([]rune(v)) > 120 {
				return fmt.Errorf("配色名称须为 1–120 个字")
			}
		case "imageUrl":
			if v != "" && !paletteImageURL.MatchString(v) {
				return fmt.Errorf("原图必须是已上传的图片素材")
			}
		case "bg", "text", "highlight", "text2", "accent":
			c := doc.PaletteColors{BG: v, Text: v, Highlight: v, Text2: v, Accent: v}
			if err := c.Normalize(); err != nil {
				return fmt.Errorf("%s 必须是 #RRGGBB 色值", key)
			}
			v = c.BG
		default:
			return fmt.Errorf("未知配色字段：%s", key)
		}
		patch[key] = v
	}
	return nil
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
	{"GET", "/palettes", "列出全局配色", "", "{ok,palettes:[{id,name,bg,text,highlight,text2,accent,imageUrl,createdAt,updatedAt}]}", func(s *Server) echo.HandlerFunc { return s.handlePalettes }},
	{"POST", "/palettes/analyze", "解析图片，不保存", "multipart file（PNG/JPEG/GIF/WebP，≤20 MB、≤2000万像素）, name?", "{ok,palette:{name,bg,text,highlight,text2,accent,imageUrl}}", func(s *Server) echo.HandlerFunc { return s.handleAnalyzePalette }},
	{"POST", "/palettes", "保存配色", "JSON {name,bg,text,highlight,text2,accent,imageUrl?} 或 multipart：palette（同结构 JSON）+ file（原图）；五色均为 #RRGGBB", "{ok,palette}", func(s *Server) echo.HandlerFunc { return s.handleCreatePalette }},
	{"GET", "/palettes/:id", "配色详情（imageUrl 可直接 GET 原图，无图为空字符串）", "", "{ok,palette:{id,name,bg,text,highlight,text2,accent,imageUrl,createdAt,updatedAt}}", func(s *Server) echo.HandlerFunc { return s.handlePalette }},
	{"PATCH", "/palettes/:id", "重命名、修改颜色或替换原图", "JSON {name?,bg?,text?,highlight?,text2?,accent?,imageUrl?} 或 multipart：palette（同结构 JSON，可为 {}）+ file（原图，只换图不改色）", "{ok,palette}", func(s *Server) echo.HandlerFunc { return s.handlePatchPalette }},
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
	return decodePaletteReader(c.Request().Body, v)
}
func decodePaletteReader(r io.Reader, v any) error {
	dec := json.NewDecoder(io.LimitReader(r, 8193))
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
	raw, ext, err := readPaletteRequest(c, &d)
	if err != nil {
		return badRequest(c, "配色数据无效："+err.Error())
	}
	if err := d.normalize(); err != nil {
		return badRequest(c, err.Error())
	}
	if len(raw) == 0 {
		if err := s.store.validatePaletteImage(d.ImageURL); err != nil {
			return badRequest(c, err.Error())
		}
	}
	if len(raw) > 0 {
		d.ImageURL, err = s.savePaletteImage(raw, ext)
		if err != nil {
			return errJSON(c, err)
		}
	}
	p, err := s.store.AddPalette(d)
	if err != nil {
		return errJSON(c, err)
	}
	return c.JSON(201, map[string]any{"ok": true, "palette": p})
}
func (s *Server) handlePatchPalette(c echo.Context) error {
	var patch map[string]string
	raw, ext, err := readPaletteRequest(c, &patch)
	if err != nil {
		return badRequest(c, "配色数据无效："+err.Error())
	}
	if _, err := s.store.Palette(c.Param("id")); err != nil {
		return paletteError(c, err)
	}
	if patch == nil {
		patch = map[string]string{}
	}
	if len(raw) > 0 {
		patch["imageUrl"] = ""
	}
	if err := validatePalettePatch(patch); err != nil {
		return badRequest(c, err.Error())
	}
	if len(raw) > 0 {
		patch["imageUrl"], err = s.savePaletteImage(raw, ext)
		if err != nil {
			return errJSON(c, err)
		}
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

// Multipart writes bundle editable metadata and original bytes in one request.
// Analysis remains read-only; callers upload again only when saving.
func readPaletteRequest(c echo.Context, value any) ([]byte, string, error) {
	if !strings.HasPrefix(c.Request().Header.Get("Content-Type"), "multipart/form-data") {
		return nil, "", decodePaletteJSON(c, value)
	}
	c.Request().Body = http.MaxBytesReader(c.Response(), c.Request().Body, maxPaletteBytes+(1<<20))
	defer func() {
		if c.Request().MultipartForm != nil {
			c.Request().MultipartForm.RemoveAll()
		}
	}()
	if err := c.Request().ParseMultipartForm(1 << 20); err != nil {
		return nil, "", fmt.Errorf("请上传不超过 20 MB 的图片")
	}
	if err := decodePaletteReader(strings.NewReader(c.FormValue("palette")), value); err != nil {
		return nil, "", err
	}
	f, _, err := c.Request().FormFile("file")
	if err != nil {
		return nil, "", fmt.Errorf("请选择图片")
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxPaletteBytes+1))
	if err != nil {
		return nil, "", err
	}
	if len(raw) > maxPaletteBytes {
		return nil, "", fmt.Errorf("图片不能超过 20 MB")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return nil, "", fmt.Errorf("不支持的图片格式")
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > maxPalettePixels {
		return nil, "", fmt.Errorf("图片不能超过 2000 万像素")
	}
	switch format {
	case "png", "jpeg", "gif", "webp":
	default:
		return nil, "", fmt.Errorf("不支持的图片格式")
	}
	if _, _, err := image.Decode(bytes.NewReader(raw)); err != nil {
		return nil, "", fmt.Errorf("图片损坏")
	}
	return raw, format, nil
}
func (s *Server) savePaletteImage(raw []byte, ext string) (string, error) {
	// The library may already hold identical bytes under .jpg rather than .jpeg.
	// Keep that reference when possible; assets has one metadata row per hash.
	var existing string
	err := s.store.db.QueryRow(`SELECT ext FROM assets WHERE sha=?`, sha1Hex(raw)).Scan(&existing)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	if paletteImageURL.MatchString("/assets/" + sha1Hex(raw) + "." + existing) {
		ext = existing
	}
	asset, err := s.assets.SaveBytes(raw, ext)
	if err != nil {
		return "", err
	}
	return "/assets/" + asset.SHA + "." + asset.Ext, nil
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
