package discover

import (
	"bytes"
	"compress/gzip"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRotated(t *testing.T) {
	yes := [][2]string{
		{"wtmp", "wtmp"}, {"WTMP", "wtmp"}, {"wtmp.1", "wtmp"},
		{"wtmp.1.gz", "wtmp"}, {"wtmp.gz", "wtmp"},
		{"wtmp-20260901", "wtmp"}, {"wtmp-20260901.gz", "wtmp"},
		{"syslog.2.gz", "syslog"}, {"audit.log.4", "audit.log"},
	}
	no := [][2]string{
		{"syslog.conf", "syslog"}, {"wtmpx", "wtmp"}, {"awtmp", "wtmp"},
		{"syslog.gz.old", "syslog"}, {"wtmp.backup", "wtmp"},
	}
	for _, c := range yes {
		if !Rotated(c[0], c[1]) {
			t.Errorf("Rotated(%q,%q) = false, want true", c[0], c[1])
		}
	}
	for _, c := range no {
		if Rotated(c[0], c[1]) {
			t.Errorf("Rotated(%q,%q) = true, want false", c[0], c[1])
		}
	}
}

func TestFilesSortedAndFiltered(t *testing.T) {
	dir := t.TempDir()
	for _, p := range []string{"b/two.log", "a/one.log", "a/skip.txt"} {
		full := filepath.Join(dir, p)
		os.MkdirAll(filepath.Dir(full), 0o755)
		os.WriteFile(full, []byte("x"), 0o644)
	}
	got, err := Files(dir, func(rel string, d fs.DirEntry) bool {
		return strings.HasSuffix(rel, ".log")
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || !strings.HasSuffix(got[0], "a/one.log") || !strings.HasSuffix(got[1], "b/two.log") {
		t.Fatalf("got %v", got)
	}
}

func TestOpenAutoGzipByContent(t *testing.T) {
	dir := t.TempDir()
	var z bytes.Buffer
	zw := gzip.NewWriter(&z)
	zw.Write([]byte("compressed body"))
	zw.Close()
	// gzip content behind a name with no .gz suffix — content wins
	gz := filepath.Join(dir, "wtmp.1")
	os.WriteFile(gz, z.Bytes(), 0o644)
	plain := filepath.Join(dir, "wtmp")
	os.WriteFile(plain, []byte("plain body"), 0o644)

	for path, want := range map[string]string{gz: "compressed body", plain: "plain body"} {
		r, err := OpenAuto(path)
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(r)
		if err != nil {
			t.Fatal(err)
		}
		r.Close()
		if string(b) != want {
			t.Errorf("%s: got %q want %q", path, b, want)
		}
	}
}
