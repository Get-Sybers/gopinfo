package toolkit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrefix(t *testing.T) {
	for in, want := range map[string]string{
		"window-licker": "WINDOW_LICKER",
		"gomft":         "GOMFT",
		"da-emon-hunt":  "DA_EMON_HUNT",
	} {
		if got := Prefix(in); got != want {
			t.Errorf("Prefix(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseBool(t *testing.T) {
	tru := []string{"1", "true", "TRUE", "Yes", " on ", "On"}
	fls := []string{"", "0", "false", "No", "off", "  "}
	for _, s := range tru {
		got, err := ParseBool(s)
		if err != nil || !got {
			t.Errorf("ParseBool(%q) = %v, %v; want true, nil", s, got, err)
		}
	}
	for _, s := range fls {
		got, err := ParseBool(s)
		if err != nil || got {
			t.Errorf("ParseBool(%q) = %v, %v; want false, nil", s, got, err)
		}
	}
	if _, err := ParseBool("maybe"); err == nil {
		t.Error("ParseBool(\"maybe\") should error")
	}
}

func TestFoldName(t *testing.T) {
	cases := map[[2]string]string{
		{"/in", "/in/a/b/$MFT"}:          "a_b_$MFT",
		{"/in", "/in/C:/Windows/x.evtx"}: "C__Windows_x.evtx",
		{"/in", "/in/with space.pf"}:     "with_space.pf",
		{"/in", "/in/top.lnk"}:           "top.lnk",
		{"/in", "/in/sub dir/b:c.txt"}:   "sub_dir_b_c.txt",
		{"/in", "/in/a.txt"}:             "a.txt",
		// an item outside the root folds its base name
		{"/in", "/elsewhere/x"}: "x",
		{"/in", "/other/e.bin"}: "e.bin",
	}
	for k, want := range cases {
		if got := FoldName(k[0], k[1]); got != want {
			t.Errorf("FoldName(%q, %q) = %q, want %q", k[0], k[1], got, want)
		}
	}
	// an over-long name is truncated and hash-suffixed so it stays unique
	long := "/in/" + strings.Repeat("n", 300)
	if got := FoldName("/in", long); len(got) > 200 {
		t.Errorf("over-long name not truncated: %d chars", len(got))
	}
}

func TestEnsureWritableDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "created", "nested")
	if err := EnsureWritableDir(dir); err != nil {
		t.Fatalf("EnsureWritableDir(new dir): %v", err)
	}
	// the probe file must be cleaned up, leaving the directory empty
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(ents) != 0 {
		t.Errorf("probe file left behind: %v", ents)
	}
	// a path under a plain file cannot be made a directory
	f := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := EnsureWritableDir(filepath.Join(f, "under")); err == nil {
		t.Error("EnsureWritableDir under a file should fail")
	}
}
