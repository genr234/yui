package native

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"io/fs"
	"os"
	"testing"
	"unicode/utf16"
)

// fixtureEntry is one archive member. Directories have dir set; files need a
// non-empty body (the 7z writer below has no empty-file support).
type fixtureEntry struct {
	name string
	body string
	dir  bool
	mode fs.FileMode
}

func (e fixtureEntry) perm() fs.FileMode {
	if e.mode != 0 {
		return e.mode
	}
	if e.dir {
		return 0o755
	}
	return 0o644
}

func writeZip(t *testing.T, path string, entries []fixtureEntry) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		h := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		if e.dir {
			h.SetMode(fs.ModeDir | e.perm())
		} else {
			h.SetMode(e.perm())
		}
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if !e.dir {
			if _, err := w.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// sevenZipNumber encodes the 7z variable-length integer.
func sevenZipNumber(v uint64) []byte {
	for n := 0; n < 8; n++ {
		if v < uint64(1)<<(7*(n+1)) {
			out := make([]byte, n+1)
			out[0] = byte(0xFF<<(8-n)) | byte(v>>(8*n))
			for i := 0; i < n; i++ {
				out[1+i] = byte(v >> (8 * i))
			}
			return out
		}
	}
	out := make([]byte, 9)
	out[0] = 0xFF
	binary.LittleEndian.PutUint64(out[1:], v)
	return out
}

// write7z writes a minimal, uncompressed (Copy coder) 7z archive with one
// folder holding every file's bytes and POSIX modes in the attributes.
func write7z(t *testing.T, path string, entries []fixtureEntry) {
	t.Helper()
	var packed bytes.Buffer
	var sizes []uint64
	var crcs []uint32
	hasDir := false
	for _, e := range entries {
		if e.dir {
			hasDir = true
			continue
		}
		if e.body == "" {
			t.Fatal("write7z: empty files are not supported")
		}
		packed.WriteString(e.body)
		sizes = append(sizes, uint64(len(e.body)))
		crcs = append(crcs, crc32.ChecksumIEEE([]byte(e.body)))
	}
	total := uint64(packed.Len())

	var h bytes.Buffer
	num := func(v uint64) { h.Write(sevenZipNumber(v)) }
	h.WriteByte(0x01) // Header
	h.WriteByte(0x04) // MainStreamsInfo
	h.WriteByte(0x06) // PackInfo
	num(0)
	num(1)
	h.WriteByte(0x09)
	num(total)
	h.WriteByte(0x00)
	h.WriteByte(0x07) // UnpackInfo
	h.WriteByte(0x0B) // Folder
	num(1)
	h.WriteByte(0x00) // not external
	num(1)            // one coder
	h.WriteByte(0x01) // id size 1, simple, no properties
	h.WriteByte(0x00) // Copy
	h.WriteByte(0x0C) // CodersUnpackSize
	num(total)
	h.WriteByte(0x00)
	h.WriteByte(0x08) // SubStreamsInfo
	h.WriteByte(0x0D)
	num(uint64(len(sizes)))
	h.WriteByte(0x09)
	for _, size := range sizes[:len(sizes)-1] {
		num(size)
	}
	h.WriteByte(0x0A) // CRC
	h.WriteByte(0x01) // all defined
	for _, c := range crcs {
		_ = binary.Write(&h, binary.LittleEndian, c)
	}
	h.WriteByte(0x00)
	h.WriteByte(0x00) // end MainStreamsInfo

	h.WriteByte(0x05) // FilesInfo
	num(uint64(len(entries)))
	if hasDir {
		bits := make([]byte, (len(entries)+7)/8)
		for i, e := range entries {
			if e.dir {
				bits[i/8] |= 0x80 >> (i % 8)
			}
		}
		h.WriteByte(0x0E) // EmptyStream
		num(uint64(len(bits)))
		h.Write(bits)
	}
	var names bytes.Buffer
	names.WriteByte(0x00) // not external
	for _, e := range entries {
		for _, u := range utf16.Encode([]rune(e.name)) {
			_ = binary.Write(&names, binary.LittleEndian, u)
		}
		names.Write([]byte{0, 0})
	}
	h.WriteByte(0x11) // Name
	num(uint64(names.Len()))
	h.Write(names.Bytes())
	var attrs bytes.Buffer
	attrs.WriteByte(0x01) // all defined
	attrs.WriteByte(0x00) // not external
	for _, e := range entries {
		var attr, unix uint32
		if e.dir {
			attr = 0x10
			unix = 0o040000 | uint32(e.perm())
		} else {
			attr = 0x20
			unix = 0o100000 | uint32(e.perm())
		}
		_ = binary.Write(&attrs, binary.LittleEndian, attr|0x8000|unix<<16)
	}
	h.WriteByte(0x15) // WinAttributes
	num(uint64(attrs.Len()))
	h.Write(attrs.Bytes())
	h.WriteByte(0x00) // end FilesInfo
	h.WriteByte(0x00) // end Header

	start := make([]byte, 20)
	binary.LittleEndian.PutUint64(start[0:], uint64(packed.Len()))
	binary.LittleEndian.PutUint64(start[8:], uint64(h.Len()))
	binary.LittleEndian.PutUint32(start[16:], crc32.ChecksumIEEE(h.Bytes()))

	var out bytes.Buffer
	out.Write(sevenZipMagic)
	out.Write([]byte{0x00, 0x04})
	_ = binary.Write(&out, binary.LittleEndian, crc32.ChecksumIEEE(start))
	out.Write(start)
	out.Write(packed.Bytes())
	out.Write(h.Bytes())
	if err := os.WriteFile(path, out.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}
