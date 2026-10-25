package server

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestProjectMetadataMigration(t *testing.T) {
	for _, previousMeta := range []bool{false, true} {
		t.Run(map[bool]string{false: "original", true: "previous-metadata"}[previousMeta], func(t *testing.T) {
			dir := t.TempDir()
			db, err := sql.Open("sqlite", filepath.Join(dir, "davinci.db"))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(projectsDDL); err != nil {
				t.Fatal(err)
			}
			if previousMeta {
				for _, c := range projectColumns[:4] {
					if _, err := db.Exec(c.ddl); err != nil {
						t.Fatal(err)
					}
				}
			}
			if _, err := db.Exec(`INSERT INTO projects (id,name,width,height,document,created_at,updated_at) VALUES ('p_old','旧项目',600,800,'{}',1,2)`); err != nil {
				t.Fatal(err)
			}
			if previousMeta {
				if _, err := db.Exec(`UPDATE projects SET kind='template', tags='["大字"]', note='旧备注', link='https://example.com/old'`); err != nil {
					t.Fatal(err)
				}
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			// Opening twice also proves migrations are idempotent.
			for i := 0; i < 2; i++ {
				s, err := OpenStore(dir)
				if err != nil {
					t.Fatal(err)
				}
				for _, field := range []struct{ name, typ, def string }{
					{"source", "TEXT", "''"}, {"author", "TEXT", "''"}, {"likes", "INTEGER", "0"}, {"template_id", "TEXT", "''"},
				} {
					var typ, def string
					var required int
					if err := s.db.QueryRow(`SELECT type, "notnull", dflt_value FROM pragma_table_info('projects') WHERE name=?`, field.name).Scan(&typ, &required, &def); err != nil {
						t.Fatal(err)
					}
					if typ != field.typ || def != field.def || required != 1 {
						t.Errorf("%s: %s, %s, required=%d", field.name, typ, def, required)
					}
				}
				p, err := s.GetProject("p_old")
				if err != nil {
					t.Fatal(err)
				}
				if p == nil || p.Name != "旧项目" || p.Source != "" || p.Author != "" || p.Likes != 0 || p.TemplateID != "" {
					t.Fatalf("migrated project: %+v", p)
				}
				if previousMeta && (p.Kind != KindTemplate || p.Note != "旧备注" || p.Link != "https://example.com/old" || len(p.Tags) != 1) {
					t.Fatalf("lost metadata: %+v", p)
				}
				if err := s.Close(); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestStoreProjectMetadataAndCopies(t *testing.T) {
	s, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p, err := s.NewProject("模板", 600, 800, nil)
	if err != nil {
		t.Fatal(err)
	}
	kind, source, author, templateID, likes := KindTemplate, " 小红书 ", " 作者 ", "", 123
	if err := s.SetProjectMeta(p.ID, ProjectMeta{Kind: &kind, Source: &source, Author: &author, Likes: &likes, TemplateID: &templateID}); err != nil {
		t.Fatal(err)
	}
	copy, err := s.DuplicateProject(p.ID, "模板副本", KindTemplate)
	if err != nil {
		t.Fatal(err)
	}
	if copy.Source != "小红书" || copy.Author != "作者" || copy.Likes != likes {
		t.Fatalf("copy: %+v", copy)
	}
	design, err := s.DuplicateProject(p.ID, "作品", KindDesign)
	if err != nil {
		t.Fatal(err)
	}
	if design.TemplateID != p.ID || design.Source != "" || design.Author != "" || design.Likes != 0 {
		t.Fatalf("design: %+v", design)
	}
	copy, err = s.DuplicateProject(design.ID, "作品副本", KindDesign)
	if err != nil {
		t.Fatal(err)
	}
	if copy.TemplateID != p.ID {
		t.Fatalf("lost templateId: %+v", copy)
	}
	likes = -1
	if err := s.SetProjectMeta(p.ID, ProjectMeta{Likes: &likes}); err == nil {
		t.Fatal("accepted negative likes")
	}
}
