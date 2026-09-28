package framework

// batch_test.go exercises the shared batch runtime (batch.go) with a fake tool,
// so every Tier-1 parser package proves the same contract: env parsing,
// discovery, one output folder per item, idempotency under FORCE, the single
// summary line and the uniform exit codes.

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// fakeBatchTool discovers every regular file under INPUT_DIR and "parses" it by
// copying its bytes; a file whose name starts with "bad" fails.
func fakeBatchTool() Tool {
	return Tool{
		Name:    "faketool",
		Formats: []string{"json", "csv"},
		Discover: func(cfg *Config) ([]string, error) {
			var out []string
			err := filepath.WalkDir(cfg.InputDir, func(p string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if !d.IsDir() {
					out = append(out, p)
				}
				return nil
			})
			sort.Strings(out)
			return out, err
		},
		Process: func(cfg *Config, item, itemDir string, w io.Writer) (int, error) {
			if strings.HasPrefix(filepath.Base(item), "bad") {
				return 0, errors.New("synthetic parse failure")
			}
			b, err := os.ReadFile(item)
			if err != nil {
				return 0, err
			}
			_, err = w.Write(b)
			return 1, err
		},
	}
}

func envOf(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

// runFake runs the fake tool and decodes the one summary line.
func runFake(t *testing.T, env map[string]string) (int, Summary, string) {
	t.Helper()
	var out bytes.Buffer
	code := Run(fakeBatchTool(), Options{Version: "0.0.0-test"}, envOf(env), &out)
	raw := out.String()
	if strings.Count(raw, "\n") != 1 || !strings.HasSuffix(raw, "\n") {
		t.Fatalf("stdout must be exactly one line, got %q", raw)
	}
	var sum Summary
	if err := json.Unmarshal([]byte(raw), &sum); err != nil {
		t.Fatalf("summary is not JSON: %v (%q)", err, raw)
	}
	if sum.Exit != code {
		t.Errorf("summary exit %d != returned code %d", sum.Exit, code)
	}
	if sum.Tool != "faketool" || sum.Version == "" || sum.Started == "" {
		t.Errorf("summary identity fields missing: %+v", sum)
	}
	if sum.Outputs == nil {
		t.Errorf("outputs must be a list, never null")
	}
	return code, sum, raw
}

func TestParseBool(t *testing.T) {
	for _, s := range []string{"1", "true", "YES", "on", " On "} {
		if v, err := ParseBool(s); err != nil || !v {
			t.Errorf("ParseBool(%q) = %v, %v; want true", s, v, err)
		}
	}
	for _, s := range []string{"", "0", "false", "no", "OFF"} {
		if v, err := ParseBool(s); err != nil || v {
			t.Errorf("ParseBool(%q) = %v, %v; want false", s, v, err)
		}
	}
	if _, err := ParseBool("maybe"); err == nil {
		t.Error("ParseBool(maybe) must fail")
	}
}

func TestPrefix(t *testing.T) {
	if got := Prefix("window-licker"); got != "WINDOW_LICKER" {
		t.Errorf("Prefix = %q", got)
	}
}

func TestRunLifecycle(t *testing.T) {
	in, out, work := t.TempDir(), t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(in, "sub dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(in, "one.bin"), []byte("one"), 0o644)
	os.WriteFile(filepath.Join(in, "sub dir", "two.bin"), []byte("two"), 0o644)
	env := map[string]string{
		"FAKETOOL_INPUT_DIR": in, "FAKETOOL_OUT_DIR": out, "FAKETOOL_WORK_DIR": work,
	}

	// first run: everything processed, one folder per item, records in files
	code, sum, _ := runFake(t, env)
	if code != 0 || sum.Status != "ok" || sum.Inputs != 2 || sum.Processed != 2 || sum.Skipped != 0 || sum.Failed != 0 {
		t.Fatalf("first run: code=%d sum=%+v", code, sum)
	}
	if len(sum.Outputs) != 2 {
		t.Fatalf("outputs = %v", sum.Outputs)
	}
	got, err := os.ReadFile(filepath.Join(out, "sub_dir_two.bin", "faketool.jsonl"))
	if err != nil || string(got) != "two" {
		t.Errorf("record file for nested item: %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(out, "one.bin", "faketool.jsonl.part")); err == nil {
		t.Error("a .part file survived a successful run")
	}

	// second run: idempotent — every item skipped, nothing rewritten
	code, sum, _ = runFake(t, env)
	if code != 0 || sum.Status != "ok" || sum.Processed != 0 || sum.Skipped != 2 {
		t.Fatalf("rerun: code=%d sum=%+v", code, sum)
	}

	// FORCE reprocesses
	env["FAKETOOL_FORCE"] = "yes"
	code, sum, _ = runFake(t, env)
	if code != 0 || sum.Processed != 2 || sum.Skipped != 0 {
		t.Fatalf("forced rerun: code=%d sum=%+v", code, sum)
	}
	delete(env, "FAKETOOL_FORCE")

	// csv format lands in its own record file, so it is not "done" yet
	env["FAKETOOL_FORMAT"] = "CSV"
	code, sum, _ = runFake(t, env)
	if code != 0 || sum.Processed != 2 {
		t.Fatalf("csv run: code=%d sum=%+v", code, sum)
	}
	if _, err := os.Stat(filepath.Join(out, "one.bin", "faketool.csv")); err != nil {
		t.Errorf("csv record file missing: %v", err)
	}
	delete(env, "FAKETOOL_FORMAT")
}

func TestRunPartialAndNothing(t *testing.T) {
	in, out := t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(in, "good"), []byte("ok"), 0o644)
	os.WriteFile(filepath.Join(in, "bad1"), []byte("x"), 0o644)
	env := map[string]string{"FAKETOOL_INPUT_DIR": in, "FAKETOOL_OUT_DIR": out}

	code, sum, _ := runFake(t, env)
	if code != 3 || sum.Status != "partial" || sum.Processed != 1 || sum.Failed != 1 || len(sum.Failures) != 1 {
		t.Fatalf("partial: code=%d sum=%+v", code, sum)
	}
	if _, err := os.Stat(filepath.Join(out, "bad1", "faketool.jsonl")); err == nil {
		t.Error("a failed item must not leave a record file")
	}

	// only failures -> nothing produced
	os.Remove(filepath.Join(in, "good"))
	os.RemoveAll(out)
	code, sum, _ = runFake(t, env)
	if code != 1 || sum.Status != "nothing" || sum.Failed != 1 {
		t.Fatalf("all failed: code=%d sum=%+v", code, sum)
	}

	// empty input tree -> nothing to do
	os.Remove(filepath.Join(in, "bad1"))
	code, sum, _ = runFake(t, env)
	if code != 1 || sum.Status != "nothing" || sum.Inputs != 0 {
		t.Fatalf("empty tree: code=%d sum=%+v", code, sum)
	}
}

func TestRunConfigErrors(t *testing.T) {
	in, out := t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(in, "f"), []byte("x"), 0o644)
	base := func() map[string]string {
		return map[string]string{"FAKETOOL_INPUT_DIR": in, "FAKETOOL_OUT_DIR": out}
	}
	cases := map[string]func(map[string]string){
		"missing input dir": func(m map[string]string) { m["FAKETOOL_INPUT_DIR"] = filepath.Join(in, "nope") },
		"input is a file":   func(m map[string]string) { m["FAKETOOL_INPUT_DIR"] = filepath.Join(in, "f") },
		"bad FORCE":         func(m map[string]string) { m["FAKETOOL_FORCE"] = "maybe" },
		"bad FORMAT":        func(m map[string]string) { m["FAKETOOL_FORMAT"] = "xml" },
		"bad LOG_LEVEL":     func(m map[string]string) { m["FAKETOOL_LOG_LEVEL"] = "loud" },
		"unwritable output": func(m map[string]string) { m["FAKETOOL_OUT_DIR"] = filepath.Join(in, "f", "sub") },
	}
	for name, mutate := range cases {
		env := base()
		mutate(env)
		code, sum, _ := runFake(t, env)
		if code != 2 || sum.Status != "config_error" || sum.Error == "" {
			t.Errorf("%s: code=%d sum=%+v", name, code, sum)
		}
	}
}

// A multi-tool image's sub-tool reads its own env block and names its record
// file after itself, while the summary still identifies the image.
func TestRunSubtoolPrefix(t *testing.T) {
	in, out := t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(in, "f"), []byte("x"), 0o644)
	tool := fakeBatchTool()
	tool.Name, tool.Subtool, tool.Prefix = "multi", "sub", "MULTI_SUB"
	var buf bytes.Buffer
	code := Run(tool, Options{Version: "0.0.0-test"}, envOf(map[string]string{"MULTI_SUB_INPUT_DIR": in, "MULTI_SUB_OUT_DIR": out}), &buf)
	var sum Summary
	if err := json.Unmarshal(buf.Bytes(), &sum); err != nil || code != 0 {
		t.Fatalf("code=%d err=%v out=%q", code, err, buf.String())
	}
	if sum.Tool != "multi" || sum.Subtool != "sub" {
		t.Errorf("summary identity = %s/%s", sum.Tool, sum.Subtool)
	}
	if _, err := os.Stat(filepath.Join(out, "f", "sub.jsonl")); err != nil {
		t.Errorf("record file named after the sub-tool: %v", err)
	}
}

// TestItemNamesFoldExts covers the FoldExts item-name folding shared by the
// tools that set it (signatures, zeek): a folded extension is dropped, a
// collision keeps full names, and an empty FoldExts keeps every name.
func TestItemNamesFoldExts(t *testing.T) {
	root := "/in"
	tool := Tool{FoldExts: map[string]bool{".pcap": true, ".pcapng": true}}
	names := tool.itemNames(root, []string{"/in/cap.pcap", "/in/notes.txt"})
	if names["/in/cap.pcap"] != "cap" {
		t.Errorf("cap.pcap folded name = %q, want cap", names["/in/cap.pcap"])
	}
	if names["/in/notes.txt"] != "notes.txt" {
		t.Errorf("notes.txt name = %q, want notes.txt", names["/in/notes.txt"])
	}
	coll := tool.itemNames(root, []string{"/in/cap.pcap", "/in/cap.pcapng"})
	if coll["/in/cap.pcap"] != "cap.pcap" || coll["/in/cap.pcapng"] != "cap.pcapng" {
		t.Errorf("colliding captures should keep full names, got %v", coll)
	}
	if plain := (Tool{}).itemNames(root, []string{"/in/cap.pcap"}); plain["/in/cap.pcap"] != "cap.pcap" {
		t.Errorf("no-fold name = %q, want cap.pcap", plain["/in/cap.pcap"])
	}
}
