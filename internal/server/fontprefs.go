package server

import (
	"encoding/json"
	"regexp"
	"strings"
)

// A person's font list: favourites first, and the fonts nobody puts on a cover
// hidden. macOS alone ships hundreds of families — system UI faces (".SF …"),
// fallback and private-use fonts, and faces for scripts a Chinese cover never
// uses — and a picker that lists them all is useless. Favourites and hidden
// fonts are kept on the server, so every page and the CLI (which is what an
// AI reads fonts from) see the same list.

const fontPrefsKey = "fontPrefs"

type fontPrefs struct {
	Favorites []string `json:"favorites,omitempty"`
	// Hidden are fonts hidden by hand; Shown are fonts the rules below hide
	// that were brought back by hand.
	Hidden []string `json:"hidden,omitempty"`
	Shown  []string `json:"shown,omitempty"`
}

func (s *Server) loadFontPrefs() fontPrefs {
	var p fontPrefs
	if raw, err := s.store.GetMeta(fontPrefsKey); err == nil && raw != "" {
		_ = json.Unmarshal([]byte(raw), &p)
	}
	return p
}

func (s *Server) saveFontPrefs(p fontPrefs) error {
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return s.store.SetMeta(fontPrefsKey, string(b))
}

// setFontPref marks a family a favourite (or not) and hidden (or not).
func (s *Server) setFontPref(family string, favorite, hidden *bool) error {
	s.fontPrefsMu.Lock()
	defer s.fontPrefsMu.Unlock()
	p := s.loadFontPrefs()
	if favorite != nil {
		p.Favorites = without(p.Favorites, family)
		if *favorite {
			p.Favorites = append(p.Favorites, family)
		}
	}
	if hidden != nil {
		p.Hidden = without(p.Hidden, family)
		p.Shown = without(p.Shown, family)
		if *hidden && !autoHidden(family) {
			p.Hidden = append(p.Hidden, family)
		}
		if !*hidden && autoHidden(family) {
			p.Shown = append(p.Shown, family)
		}
	}
	return s.saveFontPrefs(p)
}

// fontList is every font, favourites first (in the order they were starred),
// each marked favourite and/or hidden.
func (s *Server) fontList() []FontFamily {
	p := s.loadFontPrefs()
	fav := index(p.Favorites)
	hid := index(p.Hidden)
	shown := index(p.Shown)
	all := s.fonts.List()
	out := make([]FontFamily, 0, len(all))
	var favs []FontFamily
	for _, f := range all {
		_, isFav := fav[f.Family]
		_, byHand := hid[f.Family]
		_, back := shown[f.Family]
		f.Favorite = isFav
		f.Hidden = !isFav && (byHand || (autoHidden(f.Family) && !back))
		if isFav {
			favs = append(favs, f)
		} else {
			out = append(out, f)
		}
	}
	ordered := make([]FontFamily, 0, len(all))
	for _, name := range p.Favorites {
		for _, f := range favs {
			if f.Family == name {
				ordered = append(ordered, f)
			}
		}
	}
	return append(ordered, out...)
}

func index(list []string) map[string]struct{} {
	m := make(map[string]struct{}, len(list))
	for _, v := range list {
		m[v] = struct{}{}
	}
	return m
}

func without(list []string, v string) []string {
	out := list[:0:0]
	for _, x := range list {
		if x != v {
			out = append(out, x)
		}
	}
	return out
}

// Scripts and uses a cover never needs, by words in the family name.
var hiddenWords = regexp.MustCompile(`(?i)(PUA|Fallback|LastResort|Emoji|Sangam|Devanagari|Bangla|Gujarati|Gurmukhi|Kannada|Malayalam|Oriya|Odia|Tamil|Telugu|Sinhala|Khmer|\bLao\b|Myanmar|Mongolian|Tibetan|Ethiopic|Hebrew|Arabic|Urdu|Nastaliq|Naskh|Kufi|Kohinoor|Mukta|Cherokee|UCAS|InaiMathi|Kailasa|Georgian|Armenian|Syriac|Thaana|Braille|Dingbats|Wingdings|Webdings|Ornaments|\bSymbols?\b|^STIX)`)

var notoCJK = regexp.MustCompile(` (SC|TC|HK|JP|KR)$`)

// Arabic, Hebrew, Thai and other script families macOS ships under plain names.
var hiddenNames = map[string]bool{
	"Al Bayan": true, "Al Nile": true, "Al Tarikh": true, "Baghdad": true, "Beirut": true, "Damascus": true,
	"Diwan Thuluth": true, "Farah": true, "Farisi": true, "Geeza Pro": true, "Mishafi": true, "Mishafi Gold": true,
	"Muna": true, "Nadeem": true, "Raanana": true, "Sana": true, "Waseem": true, "Kefa": true, "Corsiva Hebrew": true,
	"New Peninim MT": true, "Ayuthaya": true, "Krungthep": true, "Sathu": true, "Silom": true, "Thonburi": true,
	"Kokonor": true, "Noto Nastaliq Urdu": true, "Bodoni 72 Smallcaps": true,
	// Korean faces (no Chinese glyphs), and a few more scripts and system faces.
	"AppleGothic": true, "AppleMyungjo": true, "Apple SD Gothic Neo": true, "GungSeo": true, "PilGi": true,
	"HeadLineA": true, "PCMyungjo": true, "Mshtakan": true, "Kefa III": true, "Sukhumvit Set": true,
	"GB18030 Bitmap": true, "SF UI Text": true,
}

// autoHidden says whether a family is hidden unless someone asks for it.
func autoHidden(family string) bool {
	f := strings.TrimSpace(family)
	if f == "" || strings.HasPrefix(f, ".") || hiddenNames[f] || strings.HasPrefix(f, "BM ") || strings.HasPrefix(f, "Nanum ") {
		return true
	}
	// Noto ships a family per script; keep the CJK ones and the plain ones.
	if strings.HasPrefix(f, "Noto ") {
		keep := strings.Contains(f, "CJK") || notoCJK.MatchString(f) ||
			f == "Noto Sans" || f == "Noto Serif" || f == "Noto Sans Mono"
		return !keep
	}
	return hiddenWords.MatchString(f)
}
