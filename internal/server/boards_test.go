package server

import (
	"encoding/json"
	"strings"
	"testing"
)

const twoBoards = `{"version":2,"active":"b2","boards":[
 {"id":"b1","name":"设计稿","canvas":{"width":1242,"height":1656},"layers":[{"id":"t1","name":"标题","type":"text"},{"id":"i1","name":"人像","type":"image"}]},
 {"id":"b2","name":"原图","canvas":{"width":1080,"height":1920},"layers":[{"id":"i2","name":"原图","type":"image"}]}]}`

func TestBoardsOfProject(t *testing.T) {
	doc := json.RawMessage(twoBoards)
	if w, h := canvasSizeOf(doc); w != 1242 || h != 1656 {
		t.Fatalf("size = %dx%d, want the first board's 1242x1656", w, h)
	}
	bs := Boards(doc)
	if len(bs) != 2 || bs[1].Name != "原图" || !bs[1].Active || bs[0].Active || bs[0].Layers != 2 || bs[1].Index != 2 {
		t.Fatalf("boards = %+v", bs)
	}
	// No board named: the active one.
	rows, err := LayerRows(doc, "")
	if err != nil || len(rows) != 1 || rows[0].ID != "i2" {
		t.Fatalf("active rows = %+v, %v", rows, err)
	}
	for _, ref := range []string{"b1", "设计稿", "1"} {
		rows, err := LayerRows(doc, ref)
		if err != nil || len(rows) != 2 || rows[0].ID != "i1" {
			t.Fatalf("rows(%q) = %+v, %v", ref, rows, err)
		}
	}
	if _, err := LayerRows(doc, "3"); err == nil {
		t.Fatal("board 3 should not exist")
	}
}

func TestBoardsOfLegacyDocument(t *testing.T) {
	doc := json.RawMessage(`{"version":1,"canvas":{"width":900,"height":383},"layers":[{"id":"a","name":"a","type":"shape"}]}`)
	if w, h := canvasSizeOf(doc); w != 900 || h != 383 {
		t.Fatalf("size = %dx%d", w, h)
	}
	if bs := Boards(doc); len(bs) != 1 || bs[0].Layers != 1 || !bs[0].Active {
		t.Fatalf("boards = %+v", bs)
	}
	if rows, err := LayerRows(doc, ""); err != nil || len(rows) != 1 {
		t.Fatalf("rows = %+v, %v", rows, err)
	}
}

func TestBlankDocumentHasOneBoard(t *testing.T) {
	doc, err := blankDocument(1242, 1656)
	if err != nil {
		t.Fatal(err)
	}
	bs := Boards(doc)
	if len(bs) != 1 || bs[0].Name != "画板 1" || bs[0].ID == "" || !bs[0].Active || bs[0].Width != 1242 {
		t.Fatalf("boards = %+v", bs)
	}
}

func TestRemoteAssetGuards(t *testing.T) {
	a := &AssetStore{dir: t.TempDir(), remote: true}
	if _, err := a.Import("/etc/passwd"); err != errRemoteLocalPath {
		t.Fatalf("local import on a remote server: %v", err)
	}
	for _, u := range []string{"http://127.0.0.1:1/x.png", "http://100.100.100.200/latest/meta-data/", "http://10.0.0.1/a.png"} {
		if _, err := a.Fetch(u); err == nil || !strings.Contains(err.Error(), "non-public") {
			t.Fatalf("fetch %s: %v", u, err)
		}
	}
}
