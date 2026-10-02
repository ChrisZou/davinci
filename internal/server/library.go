package server

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// The material library: reusable pictures (cut-out portraits, backgrounds, app
// logos…) sorted into categories, kept apart from any one project so every
// design — and every AI session driving the CLI — can pull from the same shelf.
//
// An item's bytes are an ordinary asset (content-addressed under data/assets),
// so inserting one into a design is just `addImage` with its /assets/ URL.
// The item row adds what makes a library searchable: a name, tags, a
// description and free-form metadata (for the imported portraits: the
// annotation that says what the pose is good for).

var libraryDDL = `
CREATE TABLE IF NOT EXISTS library_categories (
  id         TEXT PRIMARY KEY,
  name       TEXT NOT NULL UNIQUE,
  sort       INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS library_items (
  id          TEXT PRIMARY KEY,
  category_id TEXT NOT NULL REFERENCES library_categories(id),
  name        TEXT NOT NULL,
  url         TEXT NOT NULL,
  sha         TEXT NOT NULL,
  width       INTEGER NOT NULL DEFAULT 0,
  height      INTEGER NOT NULL DEFAULT 0,
  tags        TEXT NOT NULL DEFAULT '[]',
  description TEXT NOT NULL DEFAULT '',
  meta        TEXT NOT NULL DEFAULT '',
  source      TEXT NOT NULL DEFAULT '',
  created_at  INTEGER NOT NULL,
  updated_at  INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS library_items_category ON library_items(category_id, created_at);`

// defaultCategories seed an empty library, in display order.
var defaultCategories = []string{"人像", "背景", "APP Logo", "装饰元素"}

// LibraryCategory is a shelf in the library.
type LibraryCategory struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Sort      int       `json:"sort"`
	Count     int       `json:"count"`
	CreatedAt time.Time `json:"createdAt"`
}

// LibraryItem is one reusable picture.
type LibraryItem struct {
	ID          string          `json:"id"`
	CategoryID  string          `json:"categoryId"`
	Category    string          `json:"category"`
	Name        string          `json:"name"`
	URL         string          `json:"url"`
	Thumb       string          `json:"thumb"`
	Width       int             `json:"width"`
	Height      int             `json:"height"`
	Tags        []string        `json:"tags"`
	Description string          `json:"description"`
	Meta        json.RawMessage `json:"meta,omitempty"`
	Source      string          `json:"source,omitempty"`
	CreatedAt   time.Time       `json:"createdAt"`
	UpdatedAt   time.Time       `json:"updatedAt"`
}

// ErrNotFound is returned for an unknown category or item.
var ErrNotFound = errors.New("not found")

// initLibrary creates the tables and seeds the default shelves once.
func (s *Store) initLibrary() error {
	if _, err := s.db.Exec(libraryDDL); err != nil {
		return fmt.Errorf("create library tables: %w", err)
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM library_categories`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	for i, name := range defaultCategories {
		if _, err := s.CreateCategory(name, i+1); err != nil {
			return err
		}
	}
	return nil
}

// --- categories ---------------------------------------------------------

// Categories lists every shelf with its item count, in display order.
func (s *Store) Categories() ([]LibraryCategory, error) {
	rows, err := s.db.Query(`SELECT c.id, c.name, c.sort, c.created_at, COUNT(i.id)
		FROM library_categories c LEFT JOIN library_items i ON i.category_id = c.id
		GROUP BY c.id ORDER BY c.sort, c.created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LibraryCategory{}
	for rows.Next() {
		var c LibraryCategory
		var created int64
		if err := rows.Scan(&c.ID, &c.Name, &c.Sort, &created, &c.Count); err != nil {
			return nil, err
		}
		c.CreatedAt = fromDB(created)
		out = append(out, c)
	}
	return out, rows.Err()
}

// CreateCategory adds a shelf; sort 0 puts it last.
func (s *Store) CreateCategory(name string, sort int) (*LibraryCategory, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("category name is empty")
	}
	if existing, _ := s.ResolveCategory(name); existing != nil {
		return nil, fmt.Errorf("category %q already exists", name)
	}
	if sort == 0 {
		_ = s.db.QueryRow(`SELECT COALESCE(MAX(sort),0)+1 FROM library_categories`).Scan(&sort)
	}
	id, err := newID("cat")
	if err != nil {
		return nil, err
	}
	now := time.Now()
	if _, err := s.db.Exec(`INSERT INTO library_categories (id,name,sort,created_at) VALUES (?,?,?,?)`, id, name, sort, dbTime(now)); err != nil {
		return nil, err
	}
	return &LibraryCategory{ID: id, Name: name, Sort: sort, CreatedAt: now}, nil
}

// ResolveCategory finds a shelf by id or by name (case-insensitive).
func (s *Store) ResolveCategory(ref string) (*LibraryCategory, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, ErrNotFound
	}
	row := s.db.QueryRow(`SELECT id, name, sort, created_at FROM library_categories
		WHERE id = ? OR lower(name) = lower(?) LIMIT 1`, ref, ref)
	var c LibraryCategory
	var created int64
	if err := row.Scan(&c.ID, &c.Name, &c.Sort, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	c.CreatedAt = fromDB(created)
	return &c, nil
}

// UpdateCategory renames or reorders a shelf.
func (s *Store) UpdateCategory(id string, name *string, sort *int) error {
	if name != nil {
		n := strings.TrimSpace(*name)
		if n == "" {
			return errors.New("category name is empty")
		}
		if other, _ := s.ResolveCategory(n); other != nil && other.ID != id {
			return fmt.Errorf("category %q already exists", n)
		}
		if _, err := s.db.Exec(`UPDATE library_categories SET name=? WHERE id=?`, n, id); err != nil {
			return err
		}
	}
	if sort != nil {
		if _, err := s.db.Exec(`UPDATE library_categories SET sort=? WHERE id=?`, *sort, id); err != nil {
			return err
		}
	}
	return nil
}

// DeleteCategory removes an empty shelf; one with items must be emptied first,
// so a slip of the hand cannot throw away a folder of cut-outs.
func (s *Store) DeleteCategory(id string) error {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM library_items WHERE category_id=?`, id).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return fmt.Errorf("category still holds %d items — move or delete them first", n)
	}
	_, err := s.db.Exec(`DELETE FROM library_categories WHERE id=?`, id)
	return err
}

// --- items --------------------------------------------------------------

const itemColumns = `i.id, i.category_id, c.name, i.name, i.url, i.sha, i.width, i.height, i.tags, i.description, i.meta, i.source, i.created_at, i.updated_at`

func scanItem(sc interface{ Scan(...any) error }, withMeta bool) (*LibraryItem, error) {
	var it LibraryItem
	var sha, tags, meta string
	var created, updated int64
	if err := sc.Scan(&it.ID, &it.CategoryID, &it.Category, &it.Name, &it.URL, &sha, &it.Width, &it.Height, &tags, &it.Description, &meta, &it.Source, &created, &updated); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(tags), &it.Tags); err != nil || it.Tags == nil {
		it.Tags = []string{}
	}
	if withMeta && meta != "" {
		if json.Valid([]byte(meta)) {
			it.Meta = json.RawMessage(meta)
		} else {
			b, _ := json.Marshal(meta)
			it.Meta = b
		}
	}
	it.Thumb = "/api/library/items/" + it.ID + "/thumb"
	it.CreatedAt, it.UpdatedAt = fromDB(created), fromDB(updated)
	return &it, nil
}

// LibraryQuery narrows an item listing.
type LibraryQuery struct {
	CategoryID string
	// Q matches the name, tags and description; spaces separate terms that must
	// all match, so "指向 惊讶" finds a surprised pointing pose.
	Q      string
	Limit  int
	Offset int
}

// Items lists items newest first, with the total that matched.
func (s *Store) Items(q LibraryQuery) ([]LibraryItem, int, error) {
	where := []string{"1=1"}
	args := []any{}
	if q.CategoryID != "" {
		where = append(where, "i.category_id = ?")
		args = append(args, q.CategoryID)
	}
	// Rank by where the terms hit: a term in the name or the tags says what the
	// picture IS; the same words in a long description may only be a passing
	// mention ("不要指向左上…"). Newest first breaks ties.
	var rank []string
	var rankArgs []any
	for _, term := range strings.Fields(q.Q) {
		where = append(where, "(i.name LIKE ? OR i.tags LIKE ? OR i.description LIKE ?)")
		like := "%" + term + "%"
		args = append(args, like, like, like)
		rank = append(rank, "(CASE WHEN i.tags LIKE ? THEN 2 ELSE 0 END) + (CASE WHEN i.name LIKE ? THEN 2 ELSE 0 END)")
		rankArgs = append(rankArgs, like, like)
	}
	order := "i.created_at DESC, i.id"
	if len(rank) > 0 {
		order = "(" + strings.Join(rank, " + ") + ") DESC, " + order
	}
	cond := strings.Join(where, " AND ")
	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM library_items i WHERE `+cond, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	limit := q.Limit
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	queryArgs := append(append([]any{}, args...), rankArgs...)
	rows, err := s.db.Query(`SELECT `+itemColumns+` FROM library_items i JOIN library_categories c ON c.id = i.category_id
		WHERE `+cond+` ORDER BY `+order+` LIMIT ? OFFSET ?`, append(queryArgs, limit, q.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []LibraryItem{}
	for rows.Next() {
		it, err := scanItem(rows, false)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *it)
	}
	return out, total, rows.Err()
}

// Item returns one item, metadata included.
func (s *Store) Item(id string) (*LibraryItem, error) {
	row := s.db.QueryRow(`SELECT `+itemColumns+` FROM library_items i JOIN library_categories c ON c.id = i.category_id WHERE i.id = ?`, id)
	it, err := scanItem(row, true)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return it, err
}

// itemSHA is the asset an item points at (for its thumbnail).
func (s *Store) itemSHA(id string) (sha, url string, err error) {
	err = s.db.QueryRow(`SELECT sha, url FROM library_items WHERE id=?`, id).Scan(&sha, &url)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return
}

// NewItem is what adding an item takes.
type NewItem struct {
	CategoryID  string
	Name        string
	URL         string
	SHA         string
	Width       int
	Height      int
	Tags        []string
	Description string
	Meta        string
	Source      string
}

// AddItem stores an item. The same picture may sit on several shelves, but
// the same (source, category) pair is added only once, so re-running an
// import updates instead of duplicating.
func (s *Store) AddItem(n NewItem) (*LibraryItem, bool, error) {
	if n.Source != "" {
		var existing string
		err := s.db.QueryRow(`SELECT id FROM library_items WHERE source=? AND category_id=?`, n.Source, n.CategoryID).Scan(&existing)
		if err == nil {
			if err := s.updateItemRow(existing, n); err != nil {
				return nil, false, err
			}
			it, err := s.Item(existing)
			return it, false, err
		}
	}
	id, err := newID("lib")
	if err != nil {
		return nil, false, err
	}
	tags, _ := json.Marshal(cleanTags(n.Tags))
	now := dbTime(time.Now())
	_, err = s.db.Exec(`INSERT INTO library_items (id,category_id,name,url,sha,width,height,tags,description,meta,source,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		id, n.CategoryID, n.Name, n.URL, n.SHA, n.Width, n.Height, string(tags), n.Description, n.Meta, n.Source, now, now)
	if err != nil {
		return nil, false, err
	}
	it, err := s.Item(id)
	return it, true, err
}

func (s *Store) updateItemRow(id string, n NewItem) error {
	tags, _ := json.Marshal(cleanTags(n.Tags))
	_, err := s.db.Exec(`UPDATE library_items SET name=?, url=?, sha=?, width=?, height=?, tags=?, description=?, meta=?, updated_at=? WHERE id=?`,
		n.Name, n.URL, n.SHA, n.Width, n.Height, string(tags), n.Description, n.Meta, dbTime(time.Now()), id)
	return err
}

// ItemPatch changes an item's editable fields; nil leaves a field alone.
type ItemPatch struct {
	Name        *string   `json:"name"`
	Category    *string   `json:"category"`
	Tags        *[]string `json:"tags"`
	Description *string   `json:"description"`
}

// UpdateItem applies a patch.
func (s *Store) UpdateItem(id string, p ItemPatch) error {
	if _, err := s.Item(id); err != nil {
		return err
	}
	set := []string{}
	args := []any{}
	if p.Name != nil {
		n := strings.TrimSpace(*p.Name)
		if n == "" {
			return errors.New("name is empty")
		}
		set, args = append(set, "name=?"), append(args, n)
	}
	if p.Category != nil {
		c, err := s.ResolveCategory(*p.Category)
		if err != nil {
			return fmt.Errorf("no category %q", *p.Category)
		}
		set, args = append(set, "category_id=?"), append(args, c.ID)
	}
	if p.Tags != nil {
		b, _ := json.Marshal(cleanTags(*p.Tags))
		set, args = append(set, "tags=?"), append(args, string(b))
	}
	if p.Description != nil {
		set, args = append(set, "description=?"), append(args, *p.Description)
	}
	if len(set) == 0 {
		return nil
	}
	set, args = append(set, "updated_at=?"), append(args, dbTime(time.Now()))
	_, err := s.db.Exec(`UPDATE library_items SET `+strings.Join(set, ", ")+` WHERE id=?`, append(args, id)...)
	return err
}

// DeleteItem removes an item from the library. The asset stays: designs that
// already use the picture keep working.
func (s *Store) DeleteItem(id string) error {
	res, err := s.db.Exec(`DELETE FROM library_items WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// cleanTags trims, drops empties and de-duplicates, keeping order.
func cleanTags(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, t := range in {
		t = strings.TrimSpace(t)
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
}

// splitTags accepts "a,b，c" or a JSON array.
func splitTags(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if strings.HasPrefix(s, "[") {
		var arr []string
		if json.Unmarshal([]byte(s), &arr) == nil {
			return arr
		}
	}
	return strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == '，' || r == '、' })
}

// trimTransparent crops a picture to the bounding box of its visible pixels,
// keeping a small margin. Cut-outs often arrive on a big transparent frame (a
// 3840×2160 canvas with a person in the middle); fitted to a cover as-is, the
// person comes out tiny and the invisible margin is what gets grabbed.
// Returns the bytes unchanged (and false) when there is nothing to trim or the
// picture is not a PNG with transparency.
func trimTransparent(b []byte) ([]byte, bool, error) {
	img, format, err := image.Decode(bytes.NewReader(b))
	if err != nil || format != "png" {
		return b, false, nil
	}
	bounds := img.Bounds()
	minX, minY, maxX, maxY := bounds.Max.X, bounds.Max.Y, bounds.Min.X-1, bounds.Min.Y-1
	const threshold = 8 // alpha below this counts as empty (anti-aliasing dust)
	visit := func(x, y int, a uint8) {
		if a < threshold {
			return
		}
		if x < minX {
			minX = x
		}
		if x > maxX {
			maxX = x
		}
		if y < minY {
			minY = y
		}
		if y > maxY {
			maxY = y
		}
	}
	switch m := img.(type) {
	case *image.NRGBA:
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			row := m.Pix[(y-bounds.Min.Y)*m.Stride:]
			for x := 0; x < bounds.Dx(); x++ {
				visit(bounds.Min.X+x, y, row[x*4+3])
			}
		}
	case *image.RGBA:
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			row := m.Pix[(y-bounds.Min.Y)*m.Stride:]
			for x := 0; x < bounds.Dx(); x++ {
				visit(bounds.Min.X+x, y, row[x*4+3])
			}
		}
	default:
		// Paletted, grey, 16-bit: no alpha worth trimming, or rare enough to skip.
		return b, false, nil
	}
	if maxX < minX {
		return b, false, nil // fully transparent: leave it alone
	}
	const margin = 4
	crop := image.Rect(minX-margin, minY-margin, maxX+1+margin, maxY+1+margin).Intersect(bounds)
	if crop == bounds {
		return b, false, nil
	}
	sub, ok := img.(interface {
		SubImage(image.Rectangle) image.Image
	})
	if !ok {
		return b, false, nil
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, sub.SubImage(crop)); err != nil {
		return nil, false, err
	}
	return buf.Bytes(), true, nil
}

// imageSize reads a picture's pixel size without decoding it; 0×0 when the
// format is not one Go reads (SVG, say).
func imageSize(path string) (int, int) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return 0, 0
	}
	return cfg.Width, cfg.Height
}

// --- thumbnails ---------------------------------------------------------

// thumbLocks keeps two requests from rendering the same thumbnail at once.
var thumbLocks sync.Map

// thumbnail returns a PNG no wider or taller than max, rendered once from the
// asset and cached under data/thumbs. PNG keeps a cut-out's transparency.
func (s *Server) thumbnail(sha, url string, max int) (string, error) {
	dir := filepath.Join(s.dataDir, "thumbs")
	out := filepath.Join(dir, fmt.Sprintf("%s-%d.png", sha, max))
	if _, err := os.Stat(out); err == nil {
		return out, nil
	}
	mu, _ := thumbLocks.LoadOrStore(out, &sync.Mutex{})
	mu.(*sync.Mutex).Lock()
	defer mu.(*sync.Mutex).Unlock()
	if _, err := os.Stat(out); err == nil {
		return out, nil
	}
	src, err := s.assets.Path(url)
	if err != nil {
		return "", err
	}
	f, err := os.Open(src)
	if err != nil {
		return "", err
	}
	img, _, err := image.Decode(f)
	f.Close()
	if err != nil {
		// Not a raster Go can read (SVG…): the original is its own thumbnail.
		return src, nil
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w > max || h > max {
		if w >= h {
			h = h * max / w
			w = max
		} else {
			w = w * max / h
			h = max
		}
	}
	dst := image.NewNRGBA(image.Rect(0, 0, max1(w), max1(h)))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Src, nil)
	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(out, buf.Bytes(), 0o644); err != nil {
		return "", err
	}
	return out, nil
}

func max1(n int) int {
	if n < 1 {
		return 1
	}
	return n
}

// --- HTTP ---------------------------------------------------------------

func (s *Server) registerLibrary(api *echo.Group) {
	api.GET("/library/categories", s.handleListCategories)
	api.POST("/library/categories", s.handleCreateCategory)
	api.PATCH("/library/categories/:id", s.handlePatchCategory)
	api.DELETE("/library/categories/:id", s.handleDeleteCategory)
	api.GET("/library/items", s.handleListItems)
	api.POST("/library/items", s.handleAddItem)
	api.GET("/library/items/:id", s.handleGetItem)
	api.PATCH("/library/items/:id", s.handlePatchItem)
	api.DELETE("/library/items/:id", s.handleDeleteItem)
	api.GET("/library/items/:id/thumb", s.handleItemThumb)
}

func notFound(c echo.Context, msg string) error {
	return c.JSON(http.StatusNotFound, map[string]any{"ok": false, "error": msg})
}

// categoryNames lists the shelves for an error an AI can act on.
func (s *Server) categoryNames() string {
	cats, _ := s.store.Categories()
	names := make([]string, 0, len(cats))
	for _, c := range cats {
		names = append(names, c.Name)
	}
	return strings.Join(names, "、")
}

func (s *Server) handleListCategories(c echo.Context) error {
	cats, err := s.store.Categories()
	if err != nil {
		return errJSON(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"ok": true, "categories": cats})
}

func (s *Server) handleCreateCategory(c echo.Context) error {
	var req struct {
		Name string `json:"name"`
	}
	if err := c.Bind(&req); err != nil {
		return badRequest(c, "invalid JSON body: "+err.Error())
	}
	cat, err := s.store.CreateCategory(req.Name, 0)
	if err != nil {
		return badRequest(c, err.Error())
	}
	return c.JSON(http.StatusCreated, map[string]any{"ok": true, "category": cat})
}

func (s *Server) handlePatchCategory(c echo.Context) error {
	cat, err := s.store.ResolveCategory(c.Param("id"))
	if err != nil {
		return notFound(c, "no such category — categories: "+s.categoryNames())
	}
	var req struct {
		Name *string `json:"name"`
		Sort *int    `json:"sort"`
	}
	if err := c.Bind(&req); err != nil {
		return badRequest(c, "invalid JSON body: "+err.Error())
	}
	if err := s.store.UpdateCategory(cat.ID, req.Name, req.Sort); err != nil {
		return badRequest(c, err.Error())
	}
	updated, _ := s.store.ResolveCategory(cat.ID)
	return c.JSON(http.StatusOK, map[string]any{"ok": true, "category": updated})
}

func (s *Server) handleDeleteCategory(c echo.Context) error {
	cat, err := s.store.ResolveCategory(c.Param("id"))
	if err != nil {
		return notFound(c, "no such category — categories: "+s.categoryNames())
	}
	if err := s.store.DeleteCategory(cat.ID); err != nil {
		return c.JSON(http.StatusConflict, map[string]any{"ok": false, "error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleListItems(c echo.Context) error {
	q := LibraryQuery{Q: c.QueryParam("q")}
	if ref := c.QueryParam("category"); ref != "" {
		cat, err := s.store.ResolveCategory(ref)
		if err != nil {
			return notFound(c, fmt.Sprintf("no category %q — categories: %s", ref, s.categoryNames()))
		}
		q.CategoryID = cat.ID
	}
	q.Limit, _ = strconv.Atoi(c.QueryParam("limit"))
	q.Offset, _ = strconv.Atoi(c.QueryParam("offset"))
	items, total, err := s.store.Items(q)
	if err != nil {
		return errJSON(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"ok": true, "items": items, "total": total})
}

func (s *Server) handleGetItem(c echo.Context) error {
	it, err := s.store.Item(c.Param("id"))
	if errors.Is(err, ErrNotFound) {
		return notFound(c, "no such library item")
	}
	if err != nil {
		return errJSON(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"ok": true, "item": it})
}

// handleAddItem takes either a multipart upload ("file" plus form fields) or a
// JSON body naming the picture by "ref": a local path, an http(s) URL or an
// /assets/ URL already on the server.
func (s *Server) handleAddItem(c echo.Context) error {
	var (
		ref, category, name, desc, meta, source string
		tags                                    []string
		as                                      *Asset
		url                                     string
		trim                                    bool
	)
	if strings.HasPrefix(c.Request().Header.Get("Content-Type"), "multipart/") {
		category, name, desc, meta, source = c.FormValue("category"), c.FormValue("name"), c.FormValue("description"), c.FormValue("meta"), c.FormValue("source")
		tags = splitTags(c.FormValue("tags"))
		trim, _ = strconv.ParseBool(c.FormValue("trim"))
		fh, err := c.FormFile("file")
		if err != nil {
			ref = c.FormValue("ref")
			if ref == "" {
				return badRequest(c, `multipart field "file" (or a "ref") is required`)
			}
		} else {
			src, err := fh.Open()
			if err != nil {
				return errJSON(c, err)
			}
			b, err := io.ReadAll(io.LimitReader(src, maxAssetBytes+1))
			src.Close()
			if err != nil {
				return errJSON(c, err)
			}
			ext := safeExt(extOf(fh.Filename))
			if ext == "" {
				ext = "img"
			}
			if trim {
				if b, _, err = trimTransparent(b); err != nil {
					return errJSON(c, err)
				}
			}
			if as, err = s.assets.SaveBytes(b, ext); err != nil {
				return badRequest(c, err.Error())
			}
			url = "/assets/" + as.SHA + "." + as.Ext
			if name == "" {
				name = strings.TrimSuffix(fh.Filename, filepath.Ext(fh.Filename))
			}
		}
	} else {
		var req struct {
			Ref         string          `json:"ref"`
			Category    string          `json:"category"`
			Name        string          `json:"name"`
			Tags        []string        `json:"tags"`
			Description string          `json:"description"`
			Meta        json.RawMessage `json:"meta"`
			Source      string          `json:"source"`
			Trim        bool            `json:"trim"`
		}
		if err := c.Bind(&req); err != nil {
			return badRequest(c, "invalid JSON body: "+err.Error())
		}
		ref, category, name, tags, desc, source, trim = req.Ref, req.Category, req.Name, req.Tags, req.Description, req.Source, req.Trim
		if len(req.Meta) > 0 && string(req.Meta) != "null" {
			meta = string(req.Meta)
		}
		if ref == "" {
			return badRequest(c, `"ref" (a local path, an http(s) URL or an /assets/ URL) is required`)
		}
	}
	cat, err := s.store.ResolveCategory(category)
	if err != nil {
		return badRequest(c, fmt.Sprintf("unknown category %q — categories: %s (or create one first)", category, s.categoryNames()))
	}
	if as == nil {
		u, a, err := s.assets.Add(ref)
		if err != nil {
			return badRequest(c, err.Error())
		}
		url, as = u, a
		if trim {
			// The reference was stored as-is; store the trimmed picture beside it.
			if p, err := s.assets.Path(url); err == nil {
				if raw, err := os.ReadFile(p); err == nil {
					if cut, changed, err := trimTransparent(raw); err == nil && changed {
						if t, err := s.assets.SaveBytes(cut, "png"); err == nil {
							url, as = "/assets/"+t.SHA+"."+t.Ext, t
						}
					}
				}
			}
		}
		if name == "" {
			base := filepath.Base(strings.SplitN(ref, "?", 2)[0])
			name = strings.TrimSuffix(base, filepath.Ext(base))
		}
	}
	path, err := s.assets.Path(url)
	if err != nil {
		return errJSON(c, err)
	}
	w, h := imageSize(path)
	it, created, err := s.store.AddItem(NewItem{
		CategoryID: cat.ID, Name: strings.TrimSpace(firstNonEmptyStr(name, "素材")), URL: url, SHA: as.SHA,
		Width: w, Height: h, Tags: tags, Description: desc, Meta: meta, Source: source,
	})
	if err != nil {
		return errJSON(c, err)
	}
	status := http.StatusCreated
	if !created {
		status = http.StatusOK
	}
	return c.JSON(status, map[string]any{"ok": true, "item": it, "created": created})
}

func (s *Server) handlePatchItem(c echo.Context) error {
	var p ItemPatch
	if err := c.Bind(&p); err != nil {
		return badRequest(c, "invalid JSON body: "+err.Error())
	}
	err := s.store.UpdateItem(c.Param("id"), p)
	if errors.Is(err, ErrNotFound) {
		return notFound(c, "no such library item")
	}
	if err != nil {
		return badRequest(c, err.Error())
	}
	it, _ := s.store.Item(c.Param("id"))
	return c.JSON(http.StatusOK, map[string]any{"ok": true, "item": it})
}

func (s *Server) handleDeleteItem(c echo.Context) error {
	err := s.store.DeleteItem(c.Param("id"))
	if errors.Is(err, ErrNotFound) {
		return notFound(c, "no such library item")
	}
	if err != nil {
		return errJSON(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleItemThumb(c echo.Context) error {
	sha, url, err := s.store.itemSHA(c.Param("id"))
	if err != nil {
		return echo.ErrNotFound
	}
	size, _ := strconv.Atoi(c.QueryParam("w"))
	if size <= 0 {
		size = 320
	}
	if size > 1024 {
		size = 1024
	}
	path, err := s.thumbnail(sha, url, size)
	if err != nil {
		return echo.ErrNotFound
	}
	setCacheHeaders(c.Response())
	http.ServeFile(c.Response(), c.Request(), path)
	return nil
}

func firstNonEmptyStr(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
