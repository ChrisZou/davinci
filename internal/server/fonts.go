package server

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"

	"golang.org/x/image/font/sfnt"
)

// FontFamily is one typeface offered to the editor and to AI. The backend only
// reads the font's name table here — glyphs are drawn by the browser, so this
// is metadata, not rendering.
type FontFamily struct {
	Family  string `json:"family"`
	Weights []int  `json:"weights"`
	Italic  bool   `json:"italic"`
	Source  string `json:"source"` // "system" | "user"
	URL     string `json:"url,omitempty"`
	// Favorite and Hidden are the person's font list (see fontprefs.go).
	// Aliases are the other names the family answers to: its localised
	// (Chinese, Japanese) names first, for search and display.
	Aliases  []string `json:"aliases,omitempty"`
	Favorite bool     `json:"favorite,omitempty"`
	Hidden   bool     `json:"hidden,omitempty"`
}

// FontStore discovers system fonts and serves user-uploaded ones.
type FontStore struct {
	dir     string // data/fonts
	mu      sync.RWMutex
	list    []FontFamily
	faces   map[string][]faceRef // family → every face file for it
	scanned bool
}

// faceRef locates one face: a file, and its index when the file is a collection.
type faceRef struct {
	path   string
	index  int
	coll   bool
	weight int
	italic bool
}

// NewFontStore returns a store rooted at dataDir.
func NewFontStore(dataDir string) *FontStore {
	return &FontStore{dir: filepath.Join(dataDir, "fonts")}
}

// systemFontDirs are scanned in priority order. First occurrence of a family
// wins, so user fonts and ~/Library/Fonts are consulted before /System.
func systemFontDirs() []string {
	home, _ := os.UserHomeDir()
	dirs := []string{
		filepath.Join(home, "Library", "Fonts"),
		filepath.Join(home, ".fonts"),
		filepath.Join(home, ".local", "share", "fonts"),
		"/Library/Fonts",
	}
	if runtime.GOOS == "darwin" {
		dirs = append(dirs,
			"/System/Library/Fonts",
			"/System/Library/Fonts/supplemental",
		)
		// Fonts macOS downloads on demand (PingFang among them) live here.
		assets, _ := filepath.Glob("/System/Library/AssetsV2/com_apple_MobileAsset_Font*/*/AssetData")
		dirs = append(dirs, assets...)
	} else {
		dirs = append(dirs, "/usr/share/fonts", "/usr/local/share/fonts")
	}
	return dirs
}

var fontExtensions = map[string]bool{".ttf": true, ".otf": true, ".ttc": true, ".otc": true}

// Scan walks the font directories and rebuilds the cached family list. It is
// idempotent, never fails on a bad file, and safe to call repeatedly.
func (f *FontStore) Scan() error {
	type acc struct {
		weights map[int]bool
		italic  bool
		source  string
		aliases []string
	}
	byFamily := map[string]*acc{}
	order := []string{}
	faces := map[string][]faceRef{}
	// Uploaded faces are served under their real filename, so remember that
	// mapping now (userFonts re-reads the directory per call).
	userFile := map[string]string{}
	for _, u := range f.userFonts() {
		userFile[u.family] = u.file
	}

	absorb := func(file, source string) {
		found, err := readFontFaces(file)
		if err != nil {
			return // unparseable or an unsupported container; never fatal
		}
		for _, face := range found {
			if face.family == "" {
				continue
			}
			ref := faceRef{path: file, index: face.index, coll: face.coll, weight: face.weight, italic: face.italic}
			faces[face.family] = append(faces[face.family], ref)
			for _, alias := range face.aliases {
				faces[alias] = append(faces[alias], ref)
			}
			a := byFamily[face.family]
			if a == nil {
				a = &acc{weights: map[int]bool{}}
				byFamily[face.family] = a
				order = append(order, face.family)
			}
			if a.source == "" {
				a.source = source
			}
			a.weights[face.weight] = true
			if face.italic {
				a.italic = true
			}
			for _, alias := range face.aliases {
				if alias != face.family && !slices.Contains(a.aliases, alias) {
					a.aliases = append(a.aliases, alias)
				}
			}
		}
	}

	for _, dir := range systemFontDirs() {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !fontExtensions[strings.ToLower(filepath.Ext(e.Name()))] {
				continue
			}
			absorb(filepath.Join(dir, e.Name()), "system")
		}
	}

	// User-uploaded fonts live under data/fonts as <sha><ext> and are served
	// over HTTP so the page can turn them into @font-face rules. Collections
	// are rejected at upload time, so one file maps to exactly one family.
	for _, r := range f.userFonts() {
		absorb(filepath.Join(f.dir, r.file), "user")
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]FontFamily, 0, len(order))
	for _, family := range order {
		a := byFamily[family]
		weights := make([]int, 0, len(a.weights))
		for w := range a.weights {
			weights = append(weights, w)
		}
		sort.Ints(weights)
		// Names in another script (中文, 日本語) are the ones worth showing.
		aliases := slices.Clone(a.aliases)
		sort.SliceStable(aliases, func(i, j int) bool { return !isASCII(aliases[i]) && isASCII(aliases[j]) })
		ff := FontFamily{Family: family, Weights: weights, Italic: a.italic, Source: a.source, Aliases: aliases}
		if a.source == "user" {
			ff.URL = "/fonts/" + userFile[family]
		}
		out = append(out, ff)
	}
	// Stable, readable order: system families alphabetically, then user's.
	sort.SliceStable(out, func(i, j int) bool {
		if (out[i].Source == "user") != (out[j].Source == "user") {
			return out[i].Source == "system"
		}
		return out[i].Family < out[j].Family
	})
	f.list = out
	f.faces = faces
	f.scanned = true
	return nil
}

// List returns the cached family list, scanning once on first use.
func (f *FontStore) List() []FontFamily {
	f.mu.RLock()
	ok := f.scanned
	f.mu.RUnlock()
	if !ok {
		_ = f.Scan()
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	out := make([]FontFamily, len(f.list))
	copy(out, f.list)
	return out
}

// face is one weight/style row inside a font file or collection.
type face struct {
	family  string
	weight  int
	italic  bool
	full    string
	aliases []string // other names the family answers to (localised, legacy)
	index   int      // position inside a collection
	coll    bool     // the file is a collection (TTC/OTC)
}

// readFontFaces parses a TTF/OTF or TTC/OTC and reports each face under its
// preferred (English) family name, with the other names it answers to.
func readFontFaces(path string) ([]face, error) {
	infos, err := readFaceInfos(path)
	if err != nil {
		return nil, err
	}
	out := make([]face, 0, len(infos))
	for i, in := range infos {
		out = append(out, face{family: in.names[0], aliases: in.names[1:], weight: in.weight, italic: in.italic, index: i, coll: len(infos) > 1})
	}
	return out, nil
}

func fontName(f *sfnt.Font, id sfnt.NameID) string {
	name, err := f.Name(nil, id)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(name)
}

// classifySubfamily turns a subfamily label into a CSS weight + italic flag.
// It understands the usual words, "Bold Italic" compounds, and the W0..W9
// scale used by CJK collections such as Hiragino Sans and PingFang.
func classifySubfamily(sub, full string) (int, bool) {
	hay := strings.ToLower(sub + " " + full)
	weight := 400
	italic := false
	for _, w := range strings.Fields(hay) {
		switch w {
		case "thin", "extralight", "ultralight":
			weight = 200
		case "light":
			weight = 300
		case "medium":
			weight = 500
		case "semibold", "demibold":
			weight = 600
		case "bold":
			weight = 700
		case "extrabold":
			weight = 800
		case "black", "heavy":
			weight = 900
		case "italic", "oblique":
			italic = true
		}
		if strings.HasPrefix(w, "w") && len(w) == 2 && w[1] >= '0' && w[1] <= '9' {
			weight = (int(w[1]-'0') + 1) * 100
		}
	}
	// Some files carry the compound in one token.
	if strings.Contains(hay, "bold italic") || strings.Contains(hay, "boldoblique") {
		weight, italic = 700, true
	}
	return weight, italic
}

// userFont is an uploaded font file plus the family it declares.
type userFont struct {
	sha    string
	file   string
	family string
}

// userFonts reads uploaded font metadata. It is safe to call before any upload.
func (f *FontStore) userFonts() []userFont {
	entries, err := os.ReadDir(f.dir)
	if err != nil {
		return nil
	}
	var out []userFont
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		faces, err := readFontFaces(filepath.Join(f.dir, name))
		if err != nil || len(faces) != 1 || faces[0].family == "" {
			continue
		}
		out = append(out, userFont{
			sha:    strings.TrimSuffix(name, filepath.Ext(name)),
			file:   name,
			family: faces[0].family,
		})
	}
	return out
}

func slugify(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = "font"
	}
	return out
}

func isASCII(s string) bool {
	for _, r := range s {
		if r > 127 {
			return false
		}
	}
	return true
}
