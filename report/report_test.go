package report

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTree(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "var_log_wtmp")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "gowtmp.jsonl"), []byte(
		`{"Tool":"gowtmp","RecordType":"utmp","EventTime":"2026-01-01T01:00:00.000000Z"}`+"\n"+
			`{"Tool":"gowtmp","RecordType":"utmp","EventTime":"2026-01-01T00:00:00.000000Z",`+
			`"Snapshot":{"Backend":"lvm","ID":"s1"}}`+"\n"+
			`{"Tool":"gowtmp","RecordType":"lastlog","Residue":{"Kind":"orphan_inode"}}`+"\n"), 0o644)
	dir2 := filepath.Join(root, "etc_passwd")
	os.MkdirAll(dir2, 0o755)
	// CSV is the Windows tools' legacy record format — the Linux matrix is
	// JSONL-only (decision 12), so the CSV fixture wears a Windows name.
	os.WriteFile(filepath.Join(dir2, "goprefetch.csv"),
		[]byte("RecordType,SourceFilename\nprefetch,Windows/Prefetch/CMD.EXE-087B4001.pf\n"), 0o644)
	os.WriteFile(filepath.Join(dir2, "notes.txt"), []byte("ignored"), 0o644)

	items, err := Tree(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("items: %d %v", len(items), items)
	}
	csv, jl := items[0], items[1]
	if csv.Tool != "goprefetch" || csv.Records != 1 {
		t.Fatalf("csv item: %+v", csv)
	}
	if jl.Tool != "gowtmp" || jl.Records != 3 ||
		jl.First != "2026-01-01T00:00:00.000000Z" || jl.Last != "2026-01-01T01:00:00.000000Z" {
		t.Fatalf("jsonl item: %+v", jl)
	}
	if len(jl.Types) != 2 || jl.Types[0] != "lastlog" ||
		len(jl.Snapshots) != 1 || jl.Snapshots[0] != "lvm:s1" ||
		len(jl.Residues) != 1 || jl.Residues[0] != "orphan_inode" {
		t.Fatalf("sets: %+v", jl)
	}
}
