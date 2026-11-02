package server

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/labstack/echo/v4"
)

// Single-colour favourites are independent of the five-colour palettes.
const favoriteColorsDDL = `CREATE TABLE IF NOT EXISTS favorite_colors (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 color TEXT NOT NULL UNIQUE
);`

var favoriteColorHex = regexp.MustCompile(`^#(?:[0-9a-f]{3}|[0-9a-f]{4}|[0-9a-f]{6}|[0-9a-f]{8})$`)

var favoriteColorRoutes = []paletteRoute{
	{"GET", "/colors/favorites", "列出收藏的单个颜色（按收藏顺序）", "", "{ok,colors:[#rrggbb 或 #rrggbbaa]}", func(s *Server) echo.HandlerFunc { return s.handleFavoriteColors }},
	{"PUT", "/colors/favorites", "收藏或取消收藏单个颜色，规范化并去重", "JSON {color:#RGB/#RGBA/#RRGGBB/#RRGGBBAA,favorite:bool}", "{ok,colors:[#rrggbb 或 #rrggbbaa]}", func(s *Server) echo.HandlerFunc { return s.handleSetFavoriteColor }},
}

func normalizeFavoriteColor(raw string) (string, error) {
	c := strings.ToLower(strings.TrimSpace(raw))
	if !favoriteColorHex.MatchString(c) {
		return "", fmt.Errorf("请提供有效的十六进制色值")
	}
	if len(c) == 4 || len(c) == 5 {
		var b strings.Builder
		b.WriteByte('#')
		for _, ch := range c[1:] {
			b.WriteRune(ch)
			b.WriteRune(ch)
		}
		c = b.String()
	}
	if len(c) == 9 {
		if c[7:] == "00" {
			return "", fmt.Errorf("不能收藏完全透明的颜色")
		}
		if c[7:] == "ff" {
			c = c[:7]
		}
	}
	return c, nil
}

func (s *Store) FavoriteColors() ([]string, error) {
	rows, err := s.db.Query(`SELECT color FROM favorite_colors ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	colors := []string{}
	for rows.Next() {
		var color string
		if err := rows.Scan(&color); err != nil {
			return nil, err
		}
		colors = append(colors, color)
	}
	return colors, rows.Err()
}

func (s *Store) SetFavoriteColor(raw string, favorite bool) error {
	color, err := normalizeFavoriteColor(raw)
	if err != nil {
		return err
	}
	if favorite {
		_, err = s.db.Exec(`INSERT INTO favorite_colors(color) VALUES (?) ON CONFLICT(color) DO NOTHING`, color)
	} else {
		_, err = s.db.Exec(`DELETE FROM favorite_colors WHERE color=?`, color)
	}
	return err
}

func (s *Server) handleFavoriteColors(c echo.Context) error {
	colors, err := s.store.FavoriteColors()
	if err != nil {
		return errJSON(c, err)
	}
	return c.JSON(http.StatusOK, map[string]any{"ok": true, "colors": colors})
}

func (s *Server) handleSetFavoriteColor(c echo.Context) error {
	var req struct {
		Color    string `json:"color"`
		Favorite *bool  `json:"favorite"`
	}
	if err := c.Bind(&req); err != nil || req.Favorite == nil {
		return badRequest(c, "请提供颜色和收藏状态")
	}
	color, err := normalizeFavoriteColor(req.Color)
	if err != nil {
		return badRequest(c, err.Error())
	}
	if err := s.store.SetFavoriteColor(color, *req.Favorite); err != nil {
		return errJSON(c, err)
	}
	return s.handleFavoriteColors(c)
}
