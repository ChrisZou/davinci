package doc

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"golang.org/x/text/unicode/norm"
)

var reservedExportName = regexp.MustCompile(`(?i)^(con|prn|aux|nul|com[1-9¹²³]|lpt[1-9¹²³])(?:\.|$)`)

// exportName mirrors web/src/editor/export.ts. Both implementations are checked
// against testdata/export-names.json; keep that cross-language contract in sync.
func exportName(name, fallback string) string {
	name = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 || strings.ContainsRune(`<>:"/\|?*`, r) {
			return '_'
		}
		return r
	}, norm.NFC.String(name))
	// ECMAScript trim's whitespace set (not Go's slightly different IsSpace).
	name = strings.Trim(name, "\t\n\v\f\r \u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000\ufeff")
	name = strings.TrimLeft(name, ".")
	for i, r := range name {
		if i+len(string(r)) > 180 {
			name = name[:i]
			break
		}
	}
	name = strings.TrimRight(name, ". ")
	if name == "" {
		return fallback
	}
	if reservedExportName.MatchString(name) {
		name = "_" + name
	}
	return name
}

func exportBoardNames(boards []*Board, format string) []string {
	if format == "jpeg" || format == "jpg" {
		format = "jpg"
	}
	lower := cases.Lower(language.Und)
	taken := map[string]bool{}
	names := make([]string, len(boards))
	for i, b := range boards {
		base := exportName(b.Name, fmt.Sprintf("画板 %d", i+1))
		stem := base
		for n := 2; taken[lower.String(stem)]; n++ {
			stem = fmt.Sprintf("%s (%d)", base, n)
		}
		taken[lower.String(stem)] = true
		names[i] = stem + "." + format
	}
	return names
}

// The engine holds its project lock throughout this read-only command, so every
// image in the archive comes from the same revision, without changing Active.
func exportAllBoards(c *Ctx, opts RenderOptions) (any, error) {
	var buf bytes.Buffer
	archive := zip.NewWriter(&buf)
	names := exportBoardNames(c.P.Boards, opts.Format)
	for i, board := range c.P.Boards {
		data, err := c.svc().Render(board, opts)
		if err != nil {
			return nil, err
		}
		entry, err := archive.CreateHeader(&zip.FileHeader{Name: names[i], Method: zip.Store})
		if err != nil {
			return nil, err
		}
		if _, err = entry.Write(data); err != nil {
			return nil, err
		}
	}
	if err := archive.Close(); err != nil {
		return nil, err
	}
	return map[string]any{"dataURL": "data:application/zip;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()), "format": "zip", "files": names}, nil
}
