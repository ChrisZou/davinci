package server

import (
	"encoding/json"
	"reflect"
	"sync"
	"testing"
)

func TestFavoriteColorsDeduplicatePersistAndRemove(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for _, c := range []string{"#fff", "#FFF", " #ffffff ", "#FFFFFFFF", "#ffff"} {
		wg.Go(func() {
			if err := s.SetFavoriteColor(c, true); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	for _, c := range []string{"#1238", "#11223388", "#f07a34"} {
		if err := s.SetFavoriteColor(c, true); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.FavoriteColors()
	if want := []string{"#ffffff", "#11223388", "#f07a34"}; err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("persisted colours = %v, %v; want %v", got, err, want)
	}
	for i := 0; i < 2; i++ {
		if err := s.SetFavoriteColor("#FFF", false); err != nil {
			t.Fatal(err)
		}
	}
	got, _ = s.FavoriteColors()
	if !reflect.DeepEqual(got, []string{"#11223388", "#f07a34"}) {
		t.Fatal(got)
	}
	palettes, err := s.Palettes()
	if err != nil || len(palettes) != 0 {
		t.Fatal("single colours changed palette combinations", palettes, err)
	}
}

func TestFavoriteColorsAPI(t *testing.T) {
	h := start(t)
	for _, body := range []any{
		map[string]any{"color": "#fff"},
		map[string]any{"color": "invalid", "favorite": true},
		map[string]any{"color": "#12345", "favorite": true},
		map[string]any{"color": "#ffffff00", "favorite": true},
	} {
		if status, response := h.do("PUT", "/api/colors/favorites", body); status != 400 {
			t.Fatalf("invalid request accepted: %d %s", status, response)
		}
	}
	for _, input := range []struct {
		color    string
		favorite bool
		want     []string
	}{
		{"#F07A34", true, []string{"#f07a34"}},
		{"#f07a34ff", true, []string{"#f07a34"}},
		{"#F07A34", false, []string{}},
	} {
		status, body := h.do("PUT", "/api/colors/favorites", map[string]any{"color": input.color, "favorite": input.favorite})
		var response struct{ Colors []string }
		if err := json.Unmarshal(body, &response); err != nil || status != 200 || !reflect.DeepEqual(response.Colors, input.want) {
			t.Fatalf("update: %d %s, %v", status, body, err)
		}
	}
	status, body := h.do("GET", "/api/colors/favorites", nil)
	var response struct{ Colors []string }
	if err := json.Unmarshal(body, &response); err != nil || status != 200 || response.Colors == nil || len(response.Colors) != 0 {
		t.Fatalf("empty list: %d %s, %v", status, body, err)
	}
}
