// Package discover holds the shared input-discovery helpers: a deterministic
// walk, log-rotation name matching, and transparent gzip opening — so every
// tool selects its artefacts the same way (docs/framework/03: the input tree
// is recursed; rotated and gzipped siblings are first-class evidence).
package discover

import (
	"bytes"
	"compress/bzip2"
	"compress/gzip"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Files walks root and returns, sorted, the absolute path of every regular
// file match accepts. rel is the slash-separated path relative to root. A
// walk error (unreadable input) propagates — the runtime treats it as a
// config error, exit 2.
func Files(root string, match func(rel string, d fs.DirEntry) bool) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			rel = d.Name()
		}
		if match(filepath.ToSlash(rel), d) {
			out = append(out, p)
		}
		return nil
	})
	sort.Strings(out)
	return out, err
}

// Rotated reports whether name is base or a rotation of it: "wtmp",
// "wtmp.1", "wtmp.1.gz", "wtmp-20260901", "wtmp-20260901.gz", "wtmp.gz",
// "wifi.log.0.bz2" (macOS newsyslog rotates with bzip2). The comparison
// is case-insensitive; rotation tails are numeric so "syslog" never claims
// "syslog.conf".
func Rotated(name, base string) bool {
	name, base = strings.ToLower(name), strings.ToLower(base)
	if name == base || name == base+".gz" || name == base+".bz2" {
		return true
	}
	rest, ok := strings.CutPrefix(name, base)
	if !ok {
		return false
	}
	return rotTail.MatchString(rest)
}

var rotTail = regexp.MustCompile(`^([.-][0-9]+)+(\.gz|\.bz2)?$`)

// gzipMagic is the two-byte gzip signature; bzip2Magic the three-byte one.
var (
	gzipMagic  = []byte{0x1f, 0x8b}
	bzip2Magic = []byte("BZh")
)

// OpenAuto opens path read-only, transparently decompressing gzip and
// bzip2 — detected by content (the magic bytes), never by name, so a
// mislabelled rotation still reads. Close closes both the decompressor
// and the file.
func OpenAuto(path string) (io.ReadCloser, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	head := make([]byte, 3)
	n, _ := io.ReadFull(f, head)
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		f.Close()
		return nil, err
	}
	if n >= 2 && head[0] == gzipMagic[0] && head[1] == gzipMagic[1] {
		zr, err := gzip.NewReader(f)
		if err != nil {
			f.Close()
			return nil, err
		}
		return &zcloser{zr: zr, f: f}, nil
	}
	if n == 3 && bytes.Equal(head, bzip2Magic) {
		return &bzcloser{r: bzip2.NewReader(f), f: f}, nil
	}
	return f, nil
}

// bzcloser pairs a bzip2 reader (which has no Close) with its file.
type bzcloser struct {
	r io.Reader
	f *os.File
}

func (b *bzcloser) Read(p []byte) (int, error) { return b.r.Read(p) }
func (b *bzcloser) Close() error               { return b.f.Close() }

type zcloser struct {
	zr *gzip.Reader
	f  *os.File
}

func (z *zcloser) Read(p []byte) (int, error) { return z.zr.Read(p) }

func (z *zcloser) Close() error {
	zerr := z.zr.Close()
	ferr := z.f.Close()
	if zerr != nil {
		return zerr
	}
	return ferr
}
