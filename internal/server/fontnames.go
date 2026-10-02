package server

import (
	"encoding/binary"
	"errors"
	"io"
	"os"
	"strings"
	"unicode/utf16"
)

// A small reader for the parts of a font file font matching needs: every
// family name a face answers to (English and localised — a document may say
// "Songti SC" or "宋體-簡", and CSS accepts both), its weight from the OS/2
// table, and whether it is italic. Only the headers and two small tables are
// read, so scanning hundreds of system fonts stays cheap.

type faceInfo struct {
	names  []string // family names, preferred (English) first
	weight int
	italic bool
}

// readFaceInfos reads every face of a TTF/OTF/TTC/OTC.
func readFaceInfos(path string) ([]faceInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	head := make([]byte, 12)
	if _, err := f.ReadAt(head, 0); err != nil {
		return nil, err
	}
	be := binary.BigEndian
	offsets := []int64{0}
	if string(head[:4]) == "ttcf" {
		n := int(be.Uint32(head[8:12]))
		if n <= 0 || n > 256 {
			return nil, errors.New("bad collection")
		}
		raw := make([]byte, 4*n)
		if _, err := f.ReadAt(raw, 12); err != nil {
			return nil, err
		}
		offsets = offsets[:0]
		for i := 0; i < n; i++ {
			offsets = append(offsets, int64(be.Uint32(raw[4*i:])))
		}
	}
	out := make([]faceInfo, 0, len(offsets))
	for _, off := range offsets {
		info, err := readFace(f, off)
		if err != nil {
			return nil, err
		}
		out = append(out, info)
	}
	return out, nil
}

func readFace(r io.ReaderAt, off int64) (faceInfo, error) {
	be := binary.BigEndian
	hdr := make([]byte, 12)
	if _, err := r.ReadAt(hdr, off); err != nil {
		return faceInfo{}, err
	}
	n := int(be.Uint16(hdr[4:6]))
	dir := make([]byte, 16*n)
	if _, err := r.ReadAt(dir, off+12); err != nil {
		return faceInfo{}, err
	}
	var nameOff, nameLen, os2Off, os2Len int64
	for i := 0; i < n; i++ {
		rec := dir[16*i:]
		switch string(rec[:4]) {
		case "name":
			nameOff, nameLen = int64(be.Uint32(rec[8:])), int64(be.Uint32(rec[12:]))
		case "OS/2":
			os2Off, os2Len = int64(be.Uint32(rec[8:])), int64(be.Uint32(rec[12:]))
		}
	}
	info := faceInfo{weight: 400}
	if os2Len >= 64 {
		os2 := make([]byte, 64)
		if _, err := r.ReadAt(os2, os2Off); err == nil {
			w := int(be.Uint16(os2[4:6]))
			if w >= 1 && w <= 1000 {
				// Round to the nearest hundred: 330 (MiSans Regular) is a 300.
				info.weight = ((w + 50) / 100) * 100
				if info.weight < 100 {
					info.weight = 100
				}
			}
			fsSel := be.Uint16(os2[62:64])
			info.italic = fsSel&1 != 0
		}
	}
	if nameLen <= 0 || nameLen > 4<<20 {
		return info, errors.New("no name table")
	}
	tbl := make([]byte, nameLen)
	if _, err := r.ReadAt(tbl, nameOff); err != nil {
		return info, err
	}
	info.names = familyNames(tbl)
	if len(info.names) == 0 {
		return info, errors.New("no family name")
	}
	return info, nil
}

// familyNames lists the typographic family (16) names first, then the legacy
// family (1) names, English before localised, without duplicates.
func familyNames(tbl []byte) []string {
	be := binary.BigEndian
	if len(tbl) < 6 {
		return nil
	}
	count := int(be.Uint16(tbl[2:4]))
	strOff := int(be.Uint16(tbl[4:6]))
	type rec struct {
		id, lang int
		value    string
	}
	var recs []rec
	for i := 0; i < count; i++ {
		p := 6 + 12*i
		if p+12 > len(tbl) {
			break
		}
		platform := be.Uint16(tbl[p:])
		lang := int(be.Uint16(tbl[p+4:]))
		id := int(be.Uint16(tbl[p+6:]))
		length := int(be.Uint16(tbl[p+8:]))
		o := strOff + int(be.Uint16(tbl[p+10:]))
		if (id != 1 && id != 16) || o+length > len(tbl) {
			continue
		}
		raw := tbl[o : o+length]
		var v string
		switch platform {
		case 0, 3:
			u := make([]uint16, len(raw)/2)
			for j := range u {
				u[j] = be.Uint16(raw[2*j:])
			}
			v = string(utf16.Decode(u))
		case 1:
			if lang != 0 { // only Mac Roman English is plain ASCII-ish
				continue
			}
			v = string(raw)
		default:
			continue
		}
		v = strings.TrimSpace(v)
		if v != "" {
			recs = append(recs, rec{id: id, lang: lang, value: v})
		}
	}
	rank := func(r rec) int {
		k := 0
		if r.id != 16 {
			k += 2
		}
		if r.lang != 0x409 && r.lang != 0 {
			k++
		}
		return k
	}
	var out []string
	seen := map[string]bool{}
	for k := 0; k <= 3; k++ {
		for _, r := range recs {
			if rank(r) == k && !seen[r.value] {
				seen[r.value] = true
				out = append(out, r.value)
			}
		}
	}
	return out
}
