package server

import (
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
)

// Font files for renderers. The editor and the export renderer both draw with
// CanvasKit, which reads font bytes, not system font names — so the server
// hands out the face that best matches a family, weight and style. A face
// inside a collection (PingFang.ttc holds a dozen) is cut out into a
// standalone font first, since a renderer can only load a collection's first
// face; the cut is cached under data/fontcache.

// FaceMatch is the face chosen for a request, and what it really is: a
// renderer fakes the rest (bold by emboldening, italic by slanting).
type FaceMatch struct {
	Path   string
	Weight int
	Italic bool
}

// Face returns the bytes of the face closest to (family, weight, italic).
func (f *FontStore) Face(family string, weight int, italic bool) ([]byte, FaceMatch, error) {
	f.List() // scans once
	f.mu.RLock()
	refs := f.faces[family]
	f.mu.RUnlock()
	if len(refs) == 0 {
		return nil, FaceMatch{}, fmt.Errorf("no font family %q", family)
	}
	best := pickFace(refs, weight, italic)
	var b []byte
	var err error
	if best.coll {
		b, err = f.extractFace(best.path, best.index)
	} else {
		b, err = os.ReadFile(best.path)
	}
	if err != nil {
		return nil, FaceMatch{}, err
	}
	return b, FaceMatch{Path: best.path, Weight: best.weight, Italic: best.italic}, nil
}

// pickFace follows CSS font matching loosely: the right style first, then the
// nearest weight, preferring heavier for bold requests and lighter for light.
func pickFace(refs []faceRef, weight int, italic bool) faceRef {
	c := append([]faceRef(nil), refs...)
	sort.SliceStable(c, func(i, j int) bool {
		si, sj := c[i].italic == italic, c[j].italic == italic
		if si != sj {
			return si
		}
		di, dj := weightDistance(c[i].weight, weight), weightDistance(c[j].weight, weight)
		return di < dj
	})
	return c[0]
}

func weightDistance(have, want int) int {
	d := have - want
	if d < 0 {
		d = -d
	}
	// Ties go the way CSS goes: bolder for >= 500, lighter below.
	if (want >= 500 && have < want) || (want < 500 && have > want) {
		d++
	}
	return d * 2
}

// extractFace writes face `index` of a collection as a standalone sfnt file.
func (f *FontStore) extractFace(path string, index int) ([]byte, error) {
	sum := sha1.Sum([]byte(path + "#" + strconv.Itoa(index)))
	cache := filepath.Join(filepath.Dir(f.dir), "fontcache", hex.EncodeToString(sum[:10])+".ttf")
	if b, err := os.ReadFile(cache); err == nil {
		return b, nil
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	out, err := cutFace(src, index)
	if err != nil {
		return nil, err
	}
	_ = writeFile(cache, out)
	return out, nil
}

// cutFace rebuilds one face of a TTC/OTC as its own font file: the face's
// table directory is copied and every table it points at is laid out anew.
func cutFace(src []byte, index int) ([]byte, error) {
	be := binary.BigEndian
	if len(src) < 12 || string(src[:4]) != "ttcf" {
		return src, nil
	}
	n := int(be.Uint32(src[8:12]))
	if index < 0 || index >= n || len(src) < 12+4*n {
		return nil, errors.New("face index out of range")
	}
	off := int(be.Uint32(src[12+4*index:]))
	if off+12 > len(src) {
		return nil, errors.New("truncated collection")
	}
	numTables := int(be.Uint16(src[off+4:]))
	type table struct {
		tag       [4]byte
		sum       uint32
		off, size int
	}
	tables := make([]table, numTables)
	for i := range tables {
		r := off + 12 + 16*i
		if r+16 > len(src) {
			return nil, errors.New("truncated table directory")
		}
		copy(tables[i].tag[:], src[r:r+4])
		tables[i].sum = be.Uint32(src[r+4:])
		tables[i].off = int(be.Uint32(src[r+8:]))
		tables[i].size = int(be.Uint32(src[r+12:]))
		if tables[i].off+tables[i].size > len(src) {
			return nil, errors.New("table out of bounds")
		}
	}
	head := 12 + 16*numTables
	out := make([]byte, head)
	copy(out[:12], src[off:off+12])
	for i, t := range tables {
		for len(out)%4 != 0 {
			out = append(out, 0)
		}
		r := 12 + 16*i
		copy(out[r:r+4], t.tag[:])
		be.PutUint32(out[r+4:], t.sum)
		be.PutUint32(out[r+8:], uint32(len(out)))
		be.PutUint32(out[r+12:], uint32(t.size))
		out = append(out, src[t.off:t.off+t.size]...)
	}
	for len(out)%4 != 0 {
		out = append(out, 0)
	}
	return out, nil
}
