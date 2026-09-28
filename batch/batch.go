// Package batch is the container-framework batch runtime the GoDFIR-toolz Go
// tools share (docs/framework/03 environment contract, 04 self-orchestration)
// — the module form of the batch.go the Windows Tier-1 tools carry as a
// byte-identical copy, semantics unchanged, plus the docs/linux rule-2
// provenance stamping: Origin/Snapshot/Residue joined from the stage manifest
// onto every record, so no tool computes provenance by hand.
//
// With no arguments a bound tool runs in batch mode:
//
//  1. read the <TOOL>_* environment (INPUT_DIR, OUT_DIR, WORK_DIR, FORCE,
//     LOG_LEVEL) — every variable has a default;
//  2. discover every artefact the tool handles under INPUT_DIR (recursed);
//  3. process each item into its own subfolder under OUT_DIR, writing the
//     records to <OUT_DIR>/<item>/<tool>.jsonl through a stamping
//     record.Writer (JSONL is the one record format);
//  4. skip an item whose record file already exists unless FORCE is set, so a
//     rerun converges and never duplicates output;
//  5. print exactly one JSON summary line on stdout and exit with the uniform
//     code: 0 success · 1 nothing produced · 2 config error · 3 partial.
//
// Any argument switches to the tool's own argv mode (the debug pass-through),
// except `--version` and `--print-contract`, which the runtime answers itself.
package batch

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/get-sybers/gopinfo"
	"github.com/get-sybers/gopinfo/knowledge"
	"github.com/get-sybers/gopinfo/record"
	"github.com/get-sybers/gopinfo/toolkit"
	"github.com/get-sybers/gopinfo/tstamp"
)

// Tool binds the shared runtime to one tool.
type Tool struct {
	Name string
	// Subtool names the dispatched sub-tool of a multi-tool image (04 §4.3).
	// It is reported in the summary and names the record file; empty for a
	// single-tool image.
	Subtool string
	// Prefix overrides the variable prefix derived from Name (<NAME>_): a
	// multi-tool image's sub-tool env block.
	Prefix string
	// Discover returns every artefact under cfg.InputDir this tool handles,
	// sorted. An error here is a config error (unreadable input, bad tool
	// setting) and ends the run with exit 2.
	Discover func(cfg *Config) ([]string, error)
	// Process handles one item. w is the item's stamping record writer over
	// <itemDir>/<tool>.jsonl, created by the runtime and committed only when
	// Process returns nil; itemDir is there for tools that write further
	// files beside it. It returns the number of records written (w.Count()
	// unless the tool writes elsewhere too).
	Process func(cfg *Config, item, itemDir string, w *record.Writer) (records int, err error)
}

// Options carries what the binding main package knows: its stamped version
// and its embedded contract.yml.
type Options struct {
	Version  string
	Contract string
}

// Config is the resolved environment contract of one run.
type Config struct {
	Tool         string
	Prefix       string
	InputDir     string
	OutDir       string
	WorkDir      string
	KnowledgeDir string
	Force        bool
	LogLevel     int
	getenv       func(string) string
	manifest     *record.Manifest
	knowledge    *knowledge.Store
}

// Log levels, in decreasing severity; a message prints when its level is at
// or below the configured one. Everything goes to stderr.
const (
	LogError = iota
	LogWarn
	LogInfo
	LogDebug
)

var logLevels = map[string]int{"error": LogError, "warn": LogWarn, "info": LogInfo, "debug": LogDebug}

// fileBase is the record file's base name: the sub-tool for a multi-tool
// image, otherwise the tool.
func (t Tool) fileBase() string {
	if t.Subtool != "" {
		return t.Subtool
	}
	return t.Name
}

// envPrefix is the variable prefix this tool reads.
func (t Tool) envPrefix() string {
	if t.Prefix != "" {
		return t.Prefix
	}
	return Prefix(t.Name)
}

// Summary is the single JSON line printed on stdout.
type Summary struct {
	Tool      string    `json:"tool"`
	Subtool   string    `json:"subtool,omitempty"`
	Version   string    `json:"version"`
	Pinfo     string    `json:"pinfo"`
	Status    string    `json:"status"`
	Inputs    int       `json:"inputs"`
	Processed int       `json:"processed"`
	Skipped   int       `json:"skipped"`
	Failed    int       `json:"failed"`
	Records   int       `json:"records"`
	Outputs   []string  `json:"outputs"`
	Failures  []Failure `json:"failures,omitempty"`
	Error     string    `json:"error,omitempty"`
	Exit      int       `json:"exit"`
	Started   string    `json:"started"`
	DurationS float64   `json:"duration_s"`
}

type Failure struct {
	Item  string `json:"item"`
	Error string `json:"error"`
}

// Entry is the first call in a bound tool's main. It answers the framework's
// own argv (`--version`, `--print-contract`) and runs batch mode when there
// are no arguments; it returns only when argv carries the tool's own flags,
// i.e. the debug pass-through mode, which main then handles itself.
func Entry(t Tool, o Options) {
	args := os.Args[1:]
	if len(args) == 0 {
		os.Exit(Run(t, o, os.Getenv, os.Stdout))
	}
	if len(args) == 1 {
		switch strings.TrimLeft(args[0], "-") {
		case "version":
			fmt.Printf("%s %s\n", t.Name, o.Version)
			os.Exit(0)
		case "print-contract":
			io.WriteString(os.Stdout, o.Contract)
			os.Exit(0)
		}
	}
}

// Env reads a tool-specific variable (<PREFIX>_<suffix>), falling back to def
// when unset or empty.
func (c *Config) Env(suffix, def string) string {
	if v := c.getenv(c.Prefix + "_" + suffix); v != "" {
		return v
	}
	return def
}

// Logf prints a `<tool>: ...` line on stderr when level is enabled.
func (c *Config) Logf(level int, format string, args ...interface{}) {
	if level > c.LogLevel {
		return
	}
	fmt.Fprintf(os.Stderr, c.Tool+": "+format+"\n", args...)
}

// Quiet reports whether per-item progress lines are suppressed (the tools'
// argv-mode -q equivalent).
func (c *Config) Quiet() bool { return c.LogLevel < LogInfo }

// Knowledge is the Layer-1 store of this run, nil when none was mounted;
// a nil store is safe and resolves nothing.
func (c *Config) Knowledge() *knowledge.Store { return c.knowledge }

// Prefix and ParseBool re-export the shared toolkit primitives so bound tools
// keep calling batch.Prefix / batch.ParseBool unchanged.
var (
	Prefix    = toolkit.Prefix
	ParseBool = toolkit.ParseBool
)

// loadConfig resolves the environment contract and validates the mounts.
// Every failure is a config error (exit 2).
func loadConfig(t Tool, getenv func(string) string) (*Config, error) {
	cfg := &Config{Tool: t.fileBase(), Prefix: t.envPrefix(), getenv: getenv}
	cfg.InputDir = cfg.Env("INPUT_DIR", "/input")
	cfg.OutDir = cfg.Env("OUT_DIR", "/output")
	cfg.WorkDir = cfg.Env("WORK_DIR", "/work")
	cfg.KnowledgeDir = cfg.Env("KNOWLEDGE_DIR", "/knowledge")

	force, err := ParseBool(cfg.Env("FORCE", "0"))
	if err != nil {
		return nil, fmt.Errorf("%s_FORCE: %w", cfg.Prefix, err)
	}
	cfg.Force = force

	lvlName := strings.ToLower(cfg.Env("LOG_LEVEL", "info"))
	lvl, ok := logLevels[lvlName]
	if !ok {
		return nil, fmt.Errorf("%s_LOG_LEVEL: %q is not one of error|warn|info|debug", cfg.Prefix, lvlName)
	}
	cfg.LogLevel = lvl

	st, err := os.Stat(cfg.InputDir)
	if err != nil {
		return nil, fmt.Errorf("%s_INPUT_DIR %s: %w", cfg.Prefix, cfg.InputDir, err)
	}
	if !st.IsDir() {
		return nil, fmt.Errorf("%s_INPUT_DIR %s: not a directory", cfg.Prefix, cfg.InputDir)
	}
	if err := toolkit.EnsureWritableDir(cfg.OutDir); err != nil {
		return nil, fmt.Errorf("%s_OUT_DIR %s: %w", cfg.Prefix, cfg.OutDir, err)
	}
	// The work dir is optional scratch: when it cannot be used the run carries
	// on with the process temp dir rather than failing.
	if err := toolkit.EnsureWritableDir(cfg.WorkDir); err != nil {
		cfg.Logf(LogWarn, "work dir %s not usable (%v); using %s", cfg.WorkDir, err, os.TempDir())
		cfg.WorkDir = os.TempDir()
	}
	return cfg, nil
}

// relItem is the item's path relative to the input root with forward slashes
// — the SourceFilename the envelope carries and the manifest lookup key.
func relItem(root, item string) string {
	rel, err := filepath.Rel(root, item)
	if err != nil || rel == "." || rel == "" || strings.HasPrefix(rel, "..") {
		rel = filepath.Base(item)
	}
	return filepath.ToSlash(rel)
}

// Run is the batch-mode main loop. getenv and stdout are injected so the
// runtime is testable; the summary line is the only thing written to stdout.
func Run(t Tool, o Options, getenv func(string) string, stdout io.Writer) int {
	started := time.Now()
	sum := &Summary{
		Tool:    t.Name,
		Subtool: t.Subtool,
		Version: o.Version,
		Pinfo:   gopinfo.Version,
		Outputs: []string{},
		Started: started.UTC().Format(time.RFC3339),
	}
	finish := func(status string, code int) int {
		sum.Status, sum.Exit = status, code
		sum.DurationS = float64(int64(time.Since(started).Seconds()*1000)) / 1000
		enc := json.NewEncoder(stdout)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(sum); err != nil {
			fmt.Fprintf(os.Stderr, "%s: write summary: %v\n", t.Name, err)
			return 2
		}
		return code
	}

	cfg, err := loadConfig(t, getenv)
	if err != nil {
		sum.Error = err.Error()
		fmt.Fprintf(os.Stderr, "%s: config error: %v\n", t.Name, err)
		return finish("config_error", 2)
	}

	// stdout carries the summary line only: it is muted while items are
	// processed so a library that prints to stdout cannot corrupt it. The
	// summary goes to the writer captured above, not the package variable.
	if devnull, derr := os.OpenFile(os.DevNull, os.O_WRONLY, 0); derr == nil {
		saved := os.Stdout
		os.Stdout = devnull
		defer func() { os.Stdout = saved; devnull.Close() }()
	}

	// The rule-2 provenance join: a stage manifest at the input root, when
	// present, resolves each item to its Origin/Snapshot/Residue.
	cfg.manifest = record.LoadManifest(cfg.InputDir)
	if cfg.manifest != nil {
		cfg.Logf(LogDebug, "stage manifest loaded from %s", filepath.Join(cfg.InputDir, record.ManifestName))
	}

	// The Layer-1 knowledge store (docs/linux decision 14): what the image
	// says about itself, loaded from the knowledge mount when present so
	// records carry the Host context and tools can resolve against it.
	cfg.knowledge = knowledge.Load(cfg.KnowledgeDir)
	if cfg.knowledge != nil {
		cfg.Logf(LogDebug, "knowledge store loaded from %s", cfg.KnowledgeDir)
	}

	items, err := t.Discover(cfg)
	if err != nil {
		sum.Error = "discover: " + err.Error()
		cfg.Logf(LogError, "config error: %v", err)
		return finish("config_error", 2)
	}
	sum.Inputs = len(items)
	if len(items) == 0 {
		cfg.Logf(LogWarn, "no inputs found under %s", cfg.InputDir)
		return finish("nothing", 1)
	}

	for _, item := range items {
		itemDir := filepath.Join(cfg.OutDir, toolkit.FoldName(cfg.InputDir, item))
		final := filepath.Join(itemDir, t.fileBase()+".jsonl")
		if !cfg.Force {
			if st, serr := os.Stat(final); serr == nil && st.Mode().IsRegular() {
				sum.Skipped++
				sum.Outputs = append(sum.Outputs, itemDir)
				cfg.Logf(LogInfo, "skip %s (output exists: %s)", item, final)
				continue
			}
		}
		n, perr := runItem(t, o, cfg, item, itemDir, final)
		if perr != nil {
			sum.Failed++
			sum.Failures = append(sum.Failures, Failure{Item: item, Error: perr.Error()})
			cfg.Logf(LogError, "FAILED %s: %v", item, perr)
			continue
		}
		sum.Processed++
		sum.Records += n
		sum.Outputs = append(sum.Outputs, itemDir)
		cfg.Logf(LogInfo, "processed %s -> %s (%d records)", item, final, n)
	}
	cfg.Logf(LogInfo, "%d inputs: %d processed, %d skipped, %d failed, %d records",
		sum.Inputs, sum.Processed, sum.Skipped, sum.Failed, sum.Records)

	switch {
	case sum.Failed > 0 && sum.Processed+sum.Skipped == 0:
		return finish("nothing", 1)
	case sum.Failed > 0:
		return finish("partial", 3)
	}
	return finish("ok", 0)
}

// runItem processes one item into itemDir. The record file is written as
// <final>.part and renamed into place only when Process succeeds, so a partial
// or failed run never leaves a file that a later run would take as valid
// output. Under FORCE any previous record file is removed first.
func runItem(t Tool, o Options, cfg *Config, item, itemDir, final string) (int, error) {
	if err := os.MkdirAll(itemDir, 0o755); err != nil {
		return 0, err
	}
	if cfg.Force {
		if err := os.Remove(final); err != nil && !errors.Is(err, os.ErrNotExist) {
			return 0, err
		}
	}
	part := final + ".part"
	f, err := os.Create(part)
	if err != nil {
		return 0, err
	}
	w := record.NewWriter(f)
	w.SetStamp(itemStamp(t, o, cfg, item))
	n, perr := t.Process(cfg, item, itemDir, w)
	if ferr := w.Flush(); perr == nil {
		perr = ferr
	}
	if cerr := f.Close(); perr == nil {
		perr = cerr
	}
	if perr != nil {
		os.Remove(part)
		return n, perr
	}
	if err := os.Rename(part, final); err != nil {
		os.Remove(part)
		return n, err
	}
	return n, nil
}

// itemStamp resolves the envelope stamp for one item: the parser identity,
// the source path and mtime, and the manifest-joined provenance.
func itemStamp(t Tool, o Options, cfg *Config, item string) record.Stamp {
	rel := relItem(cfg.InputDir, item)
	s := record.Stamp{
		Tool:           t.Name,
		ToolVersion:    o.Version,
		SourceFilename: rel,
	}
	if st, err := os.Stat(item); err == nil {
		s.SourceModified = tstamp.ISO8601(st.ModTime())
	}
	s.Origin, s.Snapshot, s.Residue = cfg.manifest.Stamp(rel)
	s.Host = cfg.knowledge.Host()
	return s
}
