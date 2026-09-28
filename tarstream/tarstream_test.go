package tarstream

import (
	"archive/tar"
	"bytes"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"
)

func mkTar(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	mod := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for name, body := range map[string]string{
		"var/log/wtmp":   "one",
		"var/log/wtmp.1": "two",
	} {
		tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), ModTime: mod, Typeflag: tar.TypeReg})
		tw.Write([]byte(body))
	}
	tw.WriteHeader(&tar.Header{Name: "var/log", Typeflag: tar.TypeDir, Mode: 0o755, ModTime: mod})
	tw.Close()
	return &buf
}

func TestEach(t *testing.T) {
	got := map[string]string{}
	err := Each(mkTar(t), func(e Entry) error {
		b, err := io.ReadAll(e.R)
		if err != nil {
			return err
		}
		got[e.Name] = string(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["var/log/wtmp"] != "one" || got["var/log/wtmp.1"] != "two" {
		t.Fatalf("entries: %v", got)
	}
}

func TestEachCallbackError(t *testing.T) {
	boom := errors.New("boom")
	err := Each(mkTar(t), func(e Entry) error { return boom })
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}

// TestEachNulTypeflag pins that an old-style (V7) regular file whose
// typeflag byte is NUL (tar.TypeRegA) is still delivered: archive/tar
// normalises it to TypeReg on read, so the --tar pathway keeps such
// evidence files (PR #69 review).
func TestEachNulTypeflag(t *testing.T) {
	content := []byte("old tar payload")
	hdr := make([]byte, 512)
	copy(hdr, "var/log/wtmp")
	copy(hdr[100:], "0000644\x00")
	copy(hdr[108:], "0000000\x00")
	copy(hdr[116:], "0000000\x00")
	copy(hdr[124:], fmt.Sprintf("%011o\x00", len(content)))
	copy(hdr[136:], "00000000000\x00")
	hdr[156] = 0 // typeflag: NUL, the pre-POSIX regular file
	for i := 148; i < 156; i++ {
		hdr[i] = ' '
	}
	var sum int
	for _, c := range hdr {
		sum += int(c)
	}
	copy(hdr[148:], fmt.Sprintf("%06o\x00 ", sum))
	img := append(hdr, content...)
	img = append(img, make([]byte, 512-len(content))...)
	img = append(img, make([]byte, 1024)...) // end-of-archive blocks

	var got []string
	err := Each(bytes.NewReader(img), func(e Entry) error {
		b, rerr := io.ReadAll(e.R)
		if rerr != nil {
			return rerr
		}
		got = append(got, e.Name+"="+string(b))
		return nil
	})
	if err != nil || len(got) != 1 || got[0] != "var/log/wtmp=old tar payload" {
		t.Fatalf("nul-typeflag entry not delivered: %v %v", got, err)
	}
}
