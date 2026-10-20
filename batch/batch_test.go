package batch

// batch_test.go exercises the shared batch runtime with a fake tool, proving
// the same contract every bound tool relies on: env parsing, discovery, one
// output folder per item, idempotency under FORCE, the single summary line,
// the uniform exit codes — and the rule-2 provenance stamping from a stage
// manifest.

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/Get-Sybers/gopinfo/record"
)

type fakeRecord struct {
	record.Envelope
	Body string `json:"Body"`
}

// fakeTool discovers every regular file under INPUT_DIR (the manifest
// excluded) and "parses" it into one record carrying its bytes; a file whose
// name starts with "bad" fails.
func fakeTool() Tool {
	return Tool{
		Name: "faketool",
		Discover: func(cfg *Config) ([]string, error) {
			var out []string
			err := filepath.WalkDir(cfg.InputDir, func(p string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if !d.IsDir() && d.Name() != record.ManifestName {
					out = append(out, p)
				}
				return nil
			})
			sort.Strings(out)
			return out, err
		},
		Process: func(cfg *Config, item, itemDir string, w *record.Writer) (int, error) {
			if strings.HasPrefix(filepath.Base(item), "bad") {
				return 0, os.ErrInvalid
			}
			b, err := os.ReadFile(item)
			if err != nil {
				return 0, err
			}
			rec := &fakeRecord{Body: string(b)}
			rec.RecordType = "fake"
			if err := w.Write(rec); err != nil {
				return 0, err
			}
			return w.Count(), nil
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
	code := Run(fakeTool(), Options{Version: "9.9.9"}, envOf(env), &out)
	raw := out.String()
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	if len(lines) != 1 {
		t.Fatalf("stdout is not exactly one line: %q", raw)
	}
	var sum Summary
	if err := json.Unmarshal([]byte(lines[0]), &sum); err != nil {
		t.Fatalf("summary line is not JSON: %v\n%s", err, lines[0])
	}
	return code, sum, raw
}

func dirs(t *testing.T) (in, out string, env map[string]string) {
	t.Helper()
	in, out = t.TempDir(), t.TempDir()
	env = map[string]string{
		"FAKETOOL_INPUT_DIR": in,
		"FAKETOOL_OUT_DIR":   out,
		"FAKETOOL_WORK_DIR":  t.TempDir(),
	}
	return
}

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBatchOK(t *testing.T) {
	in, out, env := dirs(t)
	write(t, in, "a.txt", "alpha")
	write(t, in, "sub/b.txt", "beta")

	code, sum, _ := runFake(t, env)
	if code != 0 || sum.Status != "ok" || sum.Exit != 0 {
		t.Fatalf("want ok/0, got %d %+v", code, sum)
	}
	if sum.Inputs != 2 || sum.Processed != 2 || sum.Records != 2 {
		t.Fatalf("counts wrong: %+v", sum)
	}
	if sum.Version != "9.9.9" || sum.Pinfo == "" {
		t.Fatalf("versions missing: %+v", sum)
	}
	b, err := os.ReadFile(filepath.Join(out, "sub_b.txt", "faketool.jsonl"))
	if err != nil {
		t.Fatalf("record file: %v", err)
	}
	var rec fakeRecord
	if err := json.Unmarshal(bytes.TrimSpace(b), &rec); err != nil {
		t.Fatalf("record: %v", err)
	}
	if rec.Tool != "faketool" || rec.ToolVersion != "9.9.9" || rec.SourceFilename != "sub/b.txt" || rec.Body != "beta" {
		t.Fatalf("envelope not stamped: %+v", rec)
	}
	if rec.SourceModified == "" {
		t.Fatalf("SourceModified not stamped: %+v", rec)
	}
}

func TestBatchIdempotentAndForce(t *testing.T) {
	in, out, env := dirs(t)
	write(t, in, "a.txt", "alpha")

	if code, sum, _ := runFake(t, env); code != 0 || sum.Processed != 1 {
		t.Fatalf("first run: %d %+v", code, sum)
	}
	code, sum, _ := runFake(t, env)
	if code != 0 || sum.Skipped != 1 || sum.Processed != 0 {
		t.Fatalf("rerun should skip: %d %+v", code, sum)
	}
	env["FAKETOOL_FORCE"] = "yes"
	code, sum, _ = runFake(t, env)
	if code != 0 || sum.Processed != 1 || sum.Skipped != 0 {
		t.Fatalf("forced rerun should process: %d %+v", code, sum)
	}
	if _, err := os.Stat(filepath.Join(out, "a.txt", "faketool.jsonl")); err != nil {
		t.Fatal(err)
	}
}

func TestBatchNothingAndPartial(t *testing.T) {
	_, _, env := dirs(t)
	if code, sum, _ := runFake(t, env); code != 1 || sum.Status != "nothing" {
		t.Fatalf("empty input: %d %+v", code, sum)
	}

	in2, _, env2 := dirs(t)
	write(t, in2, "a.txt", "alpha")
	write(t, in2, "bad.txt", "boom")
	code, sum, _ := runFake(t, env2)
	if code != 3 || sum.Status != "partial" || sum.Failed != 1 || sum.Processed != 1 {
		t.Fatalf("partial: %d %+v", code, sum)
	}
	if len(sum.Failures) != 1 || !strings.HasSuffix(sum.Failures[0].Item, "bad.txt") {
		t.Fatalf("failures: %+v", sum.Failures)
	}

	in3, _, env3 := dirs(t)
	write(t, in3, "bad.txt", "boom")
	if code, sum, _ := runFake(t, env3); code != 1 || sum.Status != "nothing" {
		t.Fatalf("all-failed: %d %+v", code, sum)
	}
}

func TestBatchConfigErrors(t *testing.T) {
	_, _, env := dirs(t)
	env["FAKETOOL_INPUT_DIR"] = filepath.Join(env["FAKETOOL_INPUT_DIR"], "missing")
	if code, sum, _ := runFake(t, env); code != 2 || sum.Status != "config_error" {
		t.Fatalf("missing input: %d %+v", code, sum)
	}

	_, _, env3 := dirs(t)
	env3["FAKETOOL_LOG_LEVEL"] = "loud"
	if code, sum, _ := runFake(t, env3); code != 2 || sum.Status != "config_error" {
		t.Fatalf("bad level: %d %+v", code, sum)
	}

	_, _, env4 := dirs(t)
	env4["FAKETOOL_FORCE"] = "maybe"
	if code, sum, _ := runFake(t, env4); code != 2 || sum.Status != "config_error" {
		t.Fatalf("bad bool: %d %+v", code, sum)
	}
}

func TestBatchManifestStamping(t *testing.T) {
	in, out, env := dirs(t)
	write(t, in, "snap/lvm-home1/var/log/wtmp", "bytes")
	write(t, in, record.ManifestName, strings.Join([]string{
		`{"image":"srv01.E01","volume":"vg0/root"}`,
		`{"path":"var/log/wtmp","staged":"snap/lvm-home1/var/log/wtmp","inode":1234,` +
			`"snapshot":{"backend":"lvm","id":"home1","time":"2026-01-01T00:00:00Z"}}`,
	}, "\n")+"\n")

	if code, sum, _ := runFake(t, env); code != 0 || sum.Processed != 1 {
		t.Fatalf("run: %d %+v", code, sum)
	}
	b, err := os.ReadFile(filepath.Join(out, "snap_lvm-home1_var_log_wtmp", "faketool.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var rec fakeRecord
	if err := json.Unmarshal(bytes.TrimSpace(b), &rec); err != nil {
		t.Fatal(err)
	}
	if rec.Origin == nil || rec.Origin.Image != "srv01.E01" || rec.Origin.Volume != "vg0/root" ||
		rec.Origin.Path != "/var/log/wtmp" || rec.Origin.Inode != 1234 {
		t.Fatalf("origin not stamped: %+v", rec.Origin)
	}
	if rec.Snapshot == nil || rec.Snapshot.Backend != "lvm" || rec.Snapshot.ID != "home1" {
		t.Fatalf("snapshot not stamped: %+v", rec.Snapshot)
	}
	if rec.Residue != nil {
		t.Fatalf("residue should be absent: %+v", rec.Residue)
	}
}

// relItem is batch-specific (the manifest-lookup key / envelope SourceFilename);
// the fold behaviour it feeds is proven once in the toolkit package.
func TestRelItem(t *testing.T) {
	cases := map[[2]string]string{
		{"/in", "/in/a.txt"}:            "a.txt",
		{"/in", "/in/sub dir/b.txt"}:    "sub dir/b.txt",
		{"/in", "/other/elsewhere.bin"}: "elsewhere.bin",
	}
	for k, want := range cases {
		if got := relItem(k[0], k[1]); got != want {
			t.Errorf("relItem(%q,%q) = %q, want %q", k[0], k[1], got, want)
		}
	}
}

// readRec reads the single record the fake tool wrote to path.
func readRec(t *testing.T, path string) fakeRecord {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var rec fakeRecord
	if err := json.Unmarshal(bytes.TrimSpace(b), &rec); err != nil {
		t.Fatal(err)
	}
	return rec
}

// TestBatchStampHook covers the optional injection hook, the batch analogue of
// the framework stamp hook: a binding that sets Tool.Stamp supplies a per-item
// spindle, merged fill-only beneath the manifest/knowledge-derived stamp and
// applied through the record.Writer. On loose evidence (no manifest) the
// injected spindle becomes the Origin; a field the manifest already carries is
// never overwritten; and a tool with no hook stamps exactly as before.
func TestBatchStampHook(t *testing.T) {
	// loose evidence, with a hook: the injected spindle reaches the record,
	// and the runtime still supplies the item-intrinsic envelope fields.
	in, out, env := dirs(t)
	write(t, in, "wtmp", "bytes")
	tool := fakeTool()
	var calledItem, calledDir string
	tool.Stamp = func(cfg *Config, item, itemDir string) record.Stamp {
		calledItem, calledDir = item, itemDir
		return record.Stamp{
			Origin: &record.Origin{DiskSerial: "DS-1", VolumeGUID: "VG-1", ImageSHA256: "SHA-1"},
			Host:   &record.Host{MachineID: "host-1"},
		}
	}
	var buf bytes.Buffer
	if code := Run(tool, Options{Version: "9.9.9"}, envOf(env), &buf); code != 0 {
		t.Fatalf("hook run code=%d out=%q", code, buf.String())
	}
	if calledItem == "" || calledDir == "" {
		t.Fatalf("hook not called with item/itemDir: %q %q", calledItem, calledDir)
	}
	rec := readRec(t, filepath.Join(out, "wtmp", "faketool.jsonl"))
	if rec.Origin == nil || rec.Origin.DiskSerial != "DS-1" ||
		rec.Origin.VolumeGUID != "VG-1" || rec.Origin.ImageSHA256 != "SHA-1" {
		t.Fatalf("injected spindle not on record Origin: %+v", rec.Origin)
	}
	if rec.Host == nil || rec.Host.MachineID != "host-1" {
		t.Fatalf("injected host.id not on record Host: %+v", rec.Host)
	}
	if rec.Tool != "faketool" || rec.SourceFilename != "wtmp" || rec.SourceModified == "" {
		t.Fatalf("runtime envelope fields lost under injection: %+v", rec.Envelope)
	}

	// manifest present: fill-only. The manifest wins where it has a value; the
	// injected spindle fills only what the manifest left blank.
	in2, out2, env2 := dirs(t)
	write(t, in2, "wtmp", "bytes")
	write(t, in2, record.ManifestName, strings.Join([]string{
		`{"image":"srv.E01","volume":"vg0/root","disk_serial":"MANIFEST-DS"}`,
		`{"path":"wtmp","staged":"wtmp"}`,
	}, "\n")+"\n")
	tool2 := fakeTool()
	tool2.Stamp = func(cfg *Config, item, itemDir string) record.Stamp {
		return record.Stamp{
			Origin: &record.Origin{DiskSerial: "INJECT-DS", VolumeGUID: "INJECT-VG", ImageSHA256: "INJECT-SHA"},
			Host:   &record.Host{MachineID: "inject-host"},
		}
	}
	var buf2 bytes.Buffer
	if code := Run(tool2, Options{Version: "9.9.9"}, envOf(env2), &buf2); code != 0 {
		t.Fatalf("manifest+hook run code=%d out=%q", code, buf2.String())
	}
	rec2 := readRec(t, filepath.Join(out2, "wtmp", "faketool.jsonl"))
	if rec2.Origin == nil {
		t.Fatal("origin missing under manifest+hook")
	}
	if rec2.Origin.DiskSerial != "MANIFEST-DS" {
		t.Fatalf("manifest disk_serial overwritten by injection: %q", rec2.Origin.DiskSerial)
	}
	if rec2.Origin.Image != "srv.E01" || rec2.Origin.Volume != "vg0/root" {
		t.Fatalf("manifest origin fields lost: %+v", rec2.Origin)
	}
	if rec2.Origin.VolumeGUID != "INJECT-VG" || rec2.Origin.ImageSHA256 != "INJECT-SHA" {
		t.Fatalf("injection did not fill blank spindle fields: %+v", rec2.Origin)
	}
	if rec2.Host == nil || rec2.Host.MachineID != "inject-host" {
		t.Fatalf("injected host did not fill (no knowledge mounted): %+v", rec2.Host)
	}

	// no hook: records stamp exactly as before — loose evidence leaves
	// Origin/Host unset, so existing callers are unaffected.
	in3, out3, env3 := dirs(t)
	write(t, in3, "wtmp", "bytes")
	var buf3 bytes.Buffer
	if code := Run(fakeTool(), Options{Version: "9.9.9"}, envOf(env3), &buf3); code != 0 {
		t.Fatalf("no-hook run code=%d out=%q", code, buf3.String())
	}
	rec3 := readRec(t, filepath.Join(out3, "wtmp", "faketool.jsonl"))
	if rec3.Origin != nil || rec3.Host != nil {
		t.Fatalf("no-hook loose evidence must leave Origin/Host unset: %+v", rec3.Envelope)
	}
}
