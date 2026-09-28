package record

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type rec struct {
	Envelope
	V string `json:"V"`
}

func TestWriterJSONLStampsFillOnlyBlank(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)
	w.SetStamp(Stamp{
		Tool: "tool", ToolVersion: "1.0", SourceFilename: "staged/path",
		SourceModified: "2026-01-01T00:00:00Z",
		Origin:         &Origin{Image: "img.E01", Path: "/var/log/x"},
	})
	r1 := &rec{V: "a"}
	r1.RecordType = "t1"
	r2 := &rec{V: "b"}
	r2.RecordType = "t2"
	r2.SourceFilename = "/from/tar" // tool-set, must survive
	if err := w.Write(r1); err != nil {
		t.Fatal(err)
	}
	if err := w.Write(r2); err != nil {
		t.Fatal(err)
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	if w.Count() != 2 {
		t.Fatalf("count %d", w.Count())
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines: %q", lines)
	}
	var got rec
	if err := json.Unmarshal([]byte(lines[0]), &got); err != nil {
		t.Fatal(err)
	}
	if got.Tool != "tool" || got.SourceFilename != "staged/path" || got.Origin == nil || got.Origin.Image != "img.E01" {
		t.Fatalf("stamp not applied: %+v", got)
	}
	var got2 rec
	if err := json.Unmarshal([]byte(lines[1]), &got2); err != nil {
		t.Fatal(err)
	}
	if got2.SourceFilename != "/from/tar" {
		t.Fatalf("tool-set SourceFilename overwritten: %+v", got2)
	}
}

func TestManifest(t *testing.T) {
	dir := t.TempDir()
	rows := strings.Join([]string{
		`{"image":"disk.dd","volume":"p2"}`,
		`{"path":"\\Users\\Bob\\NTUSER.DAT","size":1,"mtime":"x","mftid":77}`,
		`{"path":"var/log/audit/audit.log","staged":"residue/orphan-9/var/log/audit/audit.log",` +
			`"inode":9,"residue":{"kind":"orphan_inode","detail":"inode 9"}}`,
		// 2^53+1: float64 would round this to ...992 — UseNumber keeps it.
		`{"path":"var/log/wtmp","inode":9007199254740993}`,
		"",
		"not json",
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, ManifestName), []byte(rows), 0o644); err != nil {
		t.Fatal(err)
	}
	m := LoadManifest(dir)
	if m == nil {
		t.Fatal("manifest not loaded")
	}

	o, s, r := m.Stamp("Users/Bob/NTUSER.DAT")
	if o == nil || o.Image != "disk.dd" || o.Volume != "p2" || o.Path != "/Users/Bob/NTUSER.DAT" || o.Inode != 77 {
		t.Fatalf("ntfs row: %+v", o)
	}
	if s != nil || r != nil {
		t.Fatalf("unexpected snapshot/residue: %v %v", s, r)
	}
	// case-insensitive staged-tree lookup
	if o2, _, _ := m.Stamp("users/bob/ntuser.dat"); o2 == nil {
		t.Fatal("case-insensitive lookup failed")
	}

	o, _, r = m.Stamp("residue/orphan-9/var/log/audit/audit.log")
	if o == nil || o.Path != "/var/log/audit/audit.log" || o.Inode != 9 {
		t.Fatalf("residue row origin: %+v", o)
	}
	if r == nil || r.Kind != "orphan_inode" {
		t.Fatalf("residue row: %+v", r)
	}

	o, _, _ = m.Stamp("var/log/wtmp")
	if o == nil || o.Inode != 9007199254740993 {
		t.Fatalf("64-bit inode lost precision: %+v", o)
	}

	if o, s, r := m.Stamp("nowhere"); o != nil || s != nil || r != nil {
		t.Fatal("unknown path must stamp nothing")
	}
	var nilM *Manifest
	if o, _, _ := nilM.Stamp("x"); o != nil {
		t.Fatal("nil manifest must stamp nothing")
	}
	if LoadManifest(t.TempDir()) != nil {
		t.Fatal("missing manifest must load nil")
	}
}

// TestWriterCountOnEncodeError pins that Count moves only on a successful
// encode: a failed record is not reported as written.
func TestWriterCountOnEncodeError(t *testing.T) {
	type badRec struct {
		Envelope
		C chan int `json:"C"` // json.Encode cannot marshal a channel
	}
	var buf bytes.Buffer
	w := NewWriter(&buf)
	if err := w.Write(&badRec{C: make(chan int)}); err == nil {
		t.Fatal("encode of a channel must error")
	}
	if w.Count() != 0 {
		t.Fatalf("failed write counted: %d", w.Count())
	}
	r := &rec{V: "ok"}
	if err := w.Write(r); err != nil {
		t.Fatal(err)
	}
	if w.Count() != 1 {
		t.Fatalf("count %d", w.Count())
	}
}
