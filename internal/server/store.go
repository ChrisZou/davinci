// Package server implements davinci's local backend: it owns every document
// and applies every edit (internal/doc), saves them, pushes each change to the
// editor pages over a WebSocket, and serves the HTTP API the CLI speaks.
// Pictures — exports, thumbnails, text measurements — come from the shared
// CanvasKit renderer running in a Node sidecar (internal/render).
package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// ErrIncompatibleDB is returned when the data directory holds a database whose
// schema was written by a different generation of davinci.
var ErrIncompatibleDB = errors.New("incompatible database schema")

// Project kinds. A template is a project like any other — layered, editable,
// exportable — kept in the template library (模板库) as a reference to build
// new designs from, instead of among the works (作品).
const (
	KindDesign   = "design"
	KindTemplate = "template"
)

// Project is one design. Document is an opaque JSON blob owned by the editor;
// the backend only reads canvas width/height out of it for listings.
type Project struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Kind      string          `json:"kind"`
	Tags      []string        `json:"tags"`
	Note      string          `json:"note"`
	Link      string          `json:"link"`
	Width     int             `json:"width"`
	Height    int             `json:"height"`
	Document  json.RawMessage `json:"document"`
	Thumbnail string          `json:"thumbnail,omitempty"`
	Revision  int64           `json:"revision"`
	CreatedAt time.Time       `json:"createdAt"`
	UpdatedAt time.Time       `json:"updatedAt"`
}

// Asset is an uploaded or imported binary (image or font) kept under data/assets.
type Asset struct {
	SHA       string    `json:"sha"`
	Ext       string    `json:"ext"`
	MIME      string    `json:"mime"`
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"createdAt"`
}

// ProjectSummary is the lightweight form used by listings and the CLI.
type ProjectSummary struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Kind      string    `json:"kind"`
	Tags      []string  `json:"tags"`
	Note      string    `json:"note,omitempty"`
	Link      string    `json:"link,omitempty"`
	Width     int       `json:"width"`
	Height    int       `json:"height"`
	Revision  int64     `json:"revision"`
	UpdatedAt time.Time `json:"updatedAt"`
}

var projectsDDL = `
CREATE TABLE IF NOT EXISTS projects (
  id         TEXT PRIMARY KEY,
  name       TEXT NOT NULL,
  width      INTEGER NOT NULL,
  height     INTEGER NOT NULL,
  document   TEXT NOT NULL,
  thumbnail  TEXT NOT NULL DEFAULT '',
  revision   INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);`

var assetsDDL = `
CREATE TABLE IF NOT EXISTS assets (
  sha        TEXT PRIMARY KEY,
  ext        TEXT NOT NULL,
  mime       TEXT NOT NULL,
  size       INTEGER NOT NULL,
  created_at INTEGER NOT NULL
);`

var fontsDDL = `
CREATE TABLE IF NOT EXISTS fonts (
  sha        TEXT PRIMARY KEY,
  filename   TEXT NOT NULL,
  family     TEXT NOT NULL,
  created_at INTEGER NOT NULL
);`

var metaDDL = `
CREATE TABLE IF NOT EXISTS meta (
  k TEXT PRIMARY KEY,
  v TEXT NOT NULL
);`

// Store owns the SQLite database and the data directory layout.
type Store struct {
	db  *sql.DB
	dir string
}

// OpenStore opens (creating if needed) the database inside dir.
func OpenStore(dir string) (*Store, error) {
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(dir, "fonts"), 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "davinci.db")
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	db.SetMaxOpenConns(1) // modernc.org/sqlite is happiest with a single writer
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("connect database: %w", err)
	}
	s := &Store{db: db, dir: dir}
	if err := s.initSchema(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// initSchema creates tables and refuses to touch a foreign-generation database.
func (s *Store) initSchema() error {
	// Detect a pre-existing projects table written by a different davinci.
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='projects'`).Scan(&n)
	if err != nil {
		return fmt.Errorf("inspect database: %w", err)
	}
	if n > 0 {
		cols, err := s.tableColumns("projects")
		if err != nil {
			return err
		}
		if !cols["document"] || !cols["revision"] {
			return fmt.Errorf("%w: the projects table in %s is missing %s — point DAVINCI_DATA (or --data) at a fresh directory, it has %v",
				ErrIncompatibleDB, filepath.Join(s.dir, "davinci.db"), missing(cols), keys(cols))
		}
	}
	for _, ddl := range []string{projectsDDL, assetsDDL, fontsDDL, metaDDL} {
		if _, err := s.db.Exec(ddl); err != nil {
			return fmt.Errorf("create tables: %w", err)
		}
	}
	if err := s.addProjectColumns(); err != nil {
		return err
	}
	return s.initLibrary()
}

// projectColumns arrived after the projects table did; a database from before
// gets them added, with every existing project a design.
var projectColumns = []struct{ name, ddl string }{
	{"kind", `ALTER TABLE projects ADD COLUMN kind TEXT NOT NULL DEFAULT 'design'`},
	{"tags", `ALTER TABLE projects ADD COLUMN tags TEXT NOT NULL DEFAULT '[]'`},
	{"note", `ALTER TABLE projects ADD COLUMN note TEXT NOT NULL DEFAULT ''`},
	{"link", `ALTER TABLE projects ADD COLUMN link TEXT NOT NULL DEFAULT ''`},
}

func (s *Store) addProjectColumns() error {
	cols, err := s.tableColumns("projects")
	if err != nil {
		return err
	}
	for _, c := range projectColumns {
		if cols[c.name] {
			continue
		}
		if _, err := s.db.Exec(c.ddl); err != nil {
			return fmt.Errorf("add projects.%s: %w", c.name, err)
		}
	}
	return nil
}

func (s *Store) tableColumns(table string) (map[string]bool, error) {
	rows, err := s.db.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return nil, fmt.Errorf("inspect table %s: %w", table, err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out[name] = true
	}
	return out, rows.Err()
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// missing names the schema columns a table lacks, for the incompatibility hint.
func missing(m map[string]bool) string {
	var out []string
	for _, want := range []string{"document", "revision"} {
		if !m[want] {
			out = append(out, want)
		}
	}
	if len(out) == 0 {
		return "nothing"
	}
	return strings.Join(out, ", ")
}

func (s *Store) Close() error { return s.db.Close() }

// Dir is the data directory.
func (s *Store) Dir() string { return s.dir }

func dbTime(t time.Time) int64  { return t.UnixMilli() }
func fromDB(ms int64) time.Time { return time.UnixMilli(ms) }

// NewProject inserts a project with a minimal, frontend-owned document.
func (s *Store) NewProject(name string, width, height int, document json.RawMessage) (*Project, error) {
	now := time.Now()
	id, err := newID("p")
	if err != nil {
		return nil, err
	}
	if document == nil {
		if document, err = blankDocument(width, height); err != nil {
			return nil, err
		}
	}
	p := &Project{
		ID:        id,
		Name:      name,
		Kind:      KindDesign,
		Tags:      []string{},
		Width:     width,
		Height:    height,
		Document:  document,
		CreatedAt: now,
		UpdatedAt: now,
	}
	_, err = s.db.Exec(`INSERT INTO projects (id,name,width,height,document,thumbnail,revision,created_at,updated_at)
		VALUES (?,?,?,?,?,'',0,?,?)`,
		p.ID, p.Name, p.Width, p.Height, string(p.Document), dbTime(p.CreatedAt), dbTime(p.UpdatedAt))
	if err != nil {
		return nil, fmt.Errorf("insert project: %w", err)
	}
	return p, nil
}

// ProjectQuery narrows a project listing.
type ProjectQuery struct {
	// Kind is KindDesign, KindTemplate, or "" for every project.
	Kind string
	// Q matches the name, tags, note and link; spaces separate terms that
	// must all match.
	Q string
	// Tag keeps only projects carrying exactly this tag.
	Tag string
}

const summaryColumns = `id,name,kind,tags,note,link,width,height,revision,updated_at`

// ListProjects sums up the projects a query picks, newest first.
func (s *Store) ListProjects(q ProjectQuery) ([]ProjectSummary, error) {
	where := []string{"1=1"}
	args := []any{}
	if q.Kind != "" {
		where = append(where, "kind = ?")
		args = append(args, q.Kind)
	}
	for _, term := range strings.Fields(q.Q) {
		like := "%" + term + "%"
		where = append(where, "(name LIKE ? OR tags LIKE ? OR note LIKE ? OR link LIKE ?)")
		args = append(args, like, like, like, like)
	}
	if tag := strings.TrimSpace(q.Tag); tag != "" {
		// Tags are a JSON array of strings; match the quoted element so "红"
		// does not also pick up "红底白字".
		b, _ := json.Marshal(tag)
		where = append(where, "tags LIKE ?")
		args = append(args, "%"+string(b)+"%")
	}
	rows, err := s.db.Query(`SELECT `+summaryColumns+` FROM projects WHERE `+strings.Join(where, " AND ")+` ORDER BY updated_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProjectSummary{}
	for rows.Next() {
		var p ProjectSummary
		var tags string
		var updated int64
		if err := rows.Scan(&p.ID, &p.Name, &p.Kind, &tags, &p.Note, &p.Link, &p.Width, &p.Height, &p.Revision, &updated); err != nil {
			return nil, err
		}
		p.Tags = parseTags(tags)
		p.UpdatedAt = fromDB(updated)
		out = append(out, p)
	}
	return out, rows.Err()
}

// ProjectTags counts the tags used by projects of a kind, most used first.
func (s *Store) ProjectTags(kind string) ([]TagCount, error) {
	list, err := s.ListProjects(ProjectQuery{Kind: kind})
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, p := range list {
		for _, t := range p.Tags {
			counts[t]++
		}
	}
	out := make([]TagCount, 0, len(counts))
	for name, n := range counts {
		out = append(out, TagCount{Name: name, Count: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// TagCount is a tag with how many projects carry it.
type TagCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func parseTags(raw string) []string {
	var tags []string
	if err := json.Unmarshal([]byte(raw), &tags); err != nil || tags == nil {
		return []string{}
	}
	return tags
}

// GetProject loads one project.
func (s *Store) GetProject(id string) (*Project, error) {
	row := s.db.QueryRow(`SELECT id,name,kind,tags,note,link,width,height,document,thumbnail,revision,created_at,updated_at FROM projects WHERE id=?`, id)
	var p Project
	// The document column is TEXT; database/sql will not scan a string straight
	// into a json.RawMessage, so it goes through a []byte first.
	var doc []byte
	var tags string
	var created, updated int64
	if err := row.Scan(&p.ID, &p.Name, &p.Kind, &tags, &p.Note, &p.Link, &p.Width, &p.Height, &doc, &p.Thumbnail, &p.Revision, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	p.Tags = parseTags(tags)
	p.Document = json.RawMessage(doc)
	p.CreatedAt, p.UpdatedAt = fromDB(created), fromDB(updated)
	return &p, nil
}

// ResolveProject finds a project by id or by exact name.
func (s *Store) ResolveProject(ref string) (*Project, error) {
	if p, err := s.GetProject(ref); err != nil || p != nil {
		return p, err
	}
	row := s.db.QueryRow(`SELECT id FROM projects WHERE name=? ORDER BY updated_at DESC LIMIT 1`, ref)
	var id string
	if err := row.Scan(&id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return s.GetProject(id)
}

// SaveDocument writes a project's document, leaving its thumbnail alone, and
// returns the new revision.
func (s *Store) SaveDocument(id string, document json.RawMessage, width, height int) (int64, error) {
	if _, err := s.db.Exec(`UPDATE projects SET document=?, width=?, height=?, revision=revision+1, updated_at=? WHERE id=?`,
		string(document), width, height, dbTime(time.Now()), id); err != nil {
		return 0, err
	}
	var rev int64
	err := s.db.QueryRow(`SELECT revision FROM projects WHERE id=?`, id).Scan(&rev)
	return rev, err
}

// SetThumbnail stores a project's home-page tile (a data URL).
func (s *Store) SetThumbnail(id, thumbnail string) error {
	_, err := s.db.Exec(`UPDATE projects SET thumbnail=? WHERE id=?`, thumbnail, id)
	return err
}

// RenameProject changes a project's name.
func (s *Store) RenameProject(id, name string) error {
	_, err := s.db.Exec(`UPDATE projects SET name=?, updated_at=? WHERE id=?`, name, dbTime(time.Now()), id)
	return err
}

// ProjectMeta changes what a project is filed as and how it is found; nil
// leaves a field alone.
type ProjectMeta struct {
	Kind *string   `json:"kind"`
	Tags *[]string `json:"tags"`
	Note *string   `json:"note"`
	Link *string   `json:"link"`
}

// SetProjectMeta applies a ProjectMeta. Moving a project between the works and
// the template library does not count as an edit: updated_at stays put.
func (s *Store) SetProjectMeta(id string, m ProjectMeta) error {
	set := []string{}
	args := []any{}
	if m.Kind != nil {
		if *m.Kind != KindDesign && *m.Kind != KindTemplate {
			return fmt.Errorf("kind must be %q or %q", KindDesign, KindTemplate)
		}
		set, args = append(set, "kind=?"), append(args, *m.Kind)
	}
	if m.Tags != nil {
		b, _ := json.Marshal(cleanTags(*m.Tags))
		set, args = append(set, "tags=?"), append(args, string(b))
	}
	if m.Note != nil {
		set, args = append(set, "note=?"), append(args, strings.TrimSpace(*m.Note))
	}
	if m.Link != nil {
		set, args = append(set, "link=?"), append(args, strings.TrimSpace(*m.Link))
	}
	if len(set) == 0 {
		return nil
	}
	_, err := s.db.Exec(`UPDATE projects SET `+strings.Join(set, ", ")+` WHERE id=?`, append(args, id)...)
	return err
}

// DuplicateProject copies a project's document and thumbnail as a new project
// of the given kind.
func (s *Store) DuplicateProject(id, name, kind string) (*Project, error) {
	src, err := s.GetProject(id)
	if err != nil || src == nil {
		return nil, err
	}
	p, err := s.NewProject(name, src.Width, src.Height, src.Document)
	if err != nil {
		return nil, err
	}
	if err := s.SetThumbnail(p.ID, src.Thumbnail); err != nil {
		return nil, err
	}
	meta := ProjectMeta{Kind: &kind}
	if kind == src.Kind {
		// A copy within the same library keeps how the original is filed; a
		// design started from a template starts out bare.
		meta.Tags, meta.Note, meta.Link = &src.Tags, &src.Note, &src.Link
	}
	if err := s.SetProjectMeta(p.ID, meta); err != nil {
		return nil, err
	}
	return s.GetProject(p.ID)
}

// DeleteProject removes a project.
func (s *Store) DeleteProject(id string) error {
	_, err := s.db.Exec(`DELETE FROM projects WHERE id=?`, id)
	return err
}

// --- meta key/value (schema cache) ---

// GetMeta reads a key from the meta table.
func (s *Store) GetMeta(k string) (string, error) {
	var v string
	err := s.db.QueryRow(`SELECT v FROM meta WHERE k=?`, k).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

// SetMeta writes a key to the meta table.
func (s *Store) SetMeta(k, v string) error {
	_, err := s.db.Exec(`INSERT INTO meta (k,v) VALUES (?,?) ON CONFLICT(k) DO UPDATE SET v=excluded.v`, k, v)
	return err
}
