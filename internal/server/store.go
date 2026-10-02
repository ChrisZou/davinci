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
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// ErrIncompatibleDB is returned when the data directory holds a database whose
// schema was written by a different generation of davinci.
var ErrIncompatibleDB = errors.New("incompatible database schema")

// Project is one design. Document is an opaque JSON blob owned by the editor;
// the backend only reads canvas width/height out of it for listings.
type Project struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
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
	return s.initLibrary()
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

// ListProjects sums up every project, newest first.
func (s *Store) ListProjects() ([]ProjectSummary, error) {
	rows, err := s.db.Query(`SELECT id,name,width,height,revision,updated_at FROM projects ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProjectSummary
	for rows.Next() {
		var p ProjectSummary
		var updated int64
		if err := rows.Scan(&p.ID, &p.Name, &p.Width, &p.Height, &p.Revision, &updated); err != nil {
			return nil, err
		}
		p.UpdatedAt = fromDB(updated)
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetProject loads one project.
func (s *Store) GetProject(id string) (*Project, error) {
	row := s.db.QueryRow(`SELECT id,name,width,height,document,thumbnail,revision,created_at,updated_at FROM projects WHERE id=?`, id)
	var p Project
	// The document column is TEXT; database/sql will not scan a string straight
	// into a json.RawMessage, so it goes through a []byte first.
	var doc []byte
	var created, updated int64
	if err := row.Scan(&p.ID, &p.Name, &p.Width, &p.Height, &doc, &p.Thumbnail, &p.Revision, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
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
