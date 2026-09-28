// Package report answers "what did this evidence produce?" over a tool
// OUT_DIR tree — the role plaso's pinfo plays for a storage file, done for
// our on-disk layout (docs/linux §3.5). Per record file it reports the
// tool, the item, the record count, the EventTime span, and the snapshot
// and residue sets seen.
//
// It reads OUTPUTS only and reports on RUNS, not evidence: it never joins
// records across tools, never mints an identity, and is no substitute for
// byakugan's timeline or cross-source views (the rules).
package report

import (
	"bufio"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Item is the report row for one record file.
type Item struct {
	Tool      string   `json:"Tool"`
	Item      string   `json:"Item"`
	File      string   `json:"File"`
	Records   int      `json:"Records"`
	First     string   `json:"First,omitempty"`
	Last      string   `json:"Last,omitempty"`
	Types     []string `json:"Types,omitempty"`
	Snapshots []string `json:"Snapshots,omitempty"`
	Residues  []string `json:"Residues,omitempty"`
}

// Tree walks an output root and reports every *.jsonl record file, sorted
// by path. CSV record files are counted by line only (header excluded).
func Tree(root string) ([]Item, error) {
	var out []Item
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		name := d.Name()
		isJSONL := strings.HasSuffix(name, ".jsonl")
		isCSV := strings.HasSuffix(name, ".csv")
		if !isJSONL && !isCSV {
			return nil
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			rel = p
		}
		it := Item{
			Tool: strings.TrimSuffix(strings.TrimSuffix(name, ".jsonl"), ".csv"),
			Item: filepath.ToSlash(filepath.Dir(rel)),
			File: filepath.ToSlash(rel),
		}
		if isJSONL {
			if e := scanJSONL(p, &it); e != nil {
				return nil // an unreadable file is skipped, not fatal
			}
		} else if e := countCSV(p, &it); e != nil {
			return nil
		}
		out = append(out, it)
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].File < out[j].File })
	return out, err
}

type envelopeView struct {
	Tool       string `json:"Tool"`
	RecordType string `json:"RecordType"`
	EventTime  string `json:"EventTime"`
	Snapshot   *struct {
		Backend string `json:"Backend"`
		ID      string `json:"ID"`
	} `json:"Snapshot"`
	Residue *struct {
		Kind string `json:"Kind"`
	} `json:"Residue"`
}

func scanJSONL(path string, it *Item) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	types := map[string]bool{}
	snaps := map[string]bool{}
	residues := map[string]bool{}
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		it.Records++
		var v envelopeView
		if json.Unmarshal(line, &v) != nil {
			continue
		}
		if v.Tool != "" {
			it.Tool = v.Tool
		}
		if v.RecordType != "" {
			types[v.RecordType] = true
		}
		if t := v.EventTime; t != "" {
			if it.First == "" || t < it.First {
				it.First = t
			}
			if t > it.Last {
				it.Last = t
			}
		}
		if v.Snapshot != nil {
			snaps[v.Snapshot.Backend+":"+v.Snapshot.ID] = true
		}
		if v.Residue != nil {
			residues[v.Residue.Kind] = true
		}
	}
	it.Types = keys(types)
	it.Snapshots = keys(snaps)
	it.Residues = keys(residues)
	return sc.Err()
}

func countCSV(path string, it *Item) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	n := 0
	for sc.Scan() {
		if len(sc.Bytes()) > 0 {
			n++
		}
	}
	if n > 0 {
		n-- // the header
	}
	it.Records = n
	return sc.Err()
}

func keys(m map[string]bool) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
