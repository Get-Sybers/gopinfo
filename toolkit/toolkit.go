// Package toolkit holds the small, behaviour-identical primitives the container
// batch runtimes share (gopinfo/framework and gopinfo/batch): the environment-name
// derivation, the framework boolean spellings, the writable-dir probe, and the
// item-path -> output-folder-name fold. Keeping them here means the two runtimes
// (which diverge in their record-writer contract) still share one copy of the
// parts that are the same.
package toolkit

import (
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// Prefix derives the SCREAMING_SNAKE_CASE variable prefix from a tool name.
func Prefix(tool string) string {
	return strings.ToUpper(strings.ReplaceAll(tool, "-", "_"))
}

// ParseBool accepts the framework's boolean spellings: 1/true/yes/on and
// 0/false/no/off (case-insensitive; empty is false).
func ParseBool(s string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on":
		return true, nil
	case "", "0", "false", "no", "off":
		return false, nil
	}
	return false, fmt.Errorf("not a boolean (1/true/yes/on or 0/false/no/off): %q", s)
}

// EnsureWritableDir creates dir if needed and proves it is writable by
// creating and removing a probe file.
func EnsureWritableDir(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".probe-*")
	if err != nil {
		return fmt.Errorf("not writable: %w", err)
	}
	name := f.Name()
	if err := f.Close(); err != nil {
		os.Remove(name)
		return fmt.Errorf("not writable: %w", err)
	}
	return os.Remove(name)
}

// FoldName derives the per-item output folder name from the item's path
// relative to the input root: path separators, whitespace and ':' fold to '_';
// every other character is kept. An over-long name is truncated and suffixed
// with a short hash so it stays unique.
func FoldName(root, item string) string {
	rel, err := filepath.Rel(root, item)
	if err != nil || rel == "." || rel == "" || strings.HasPrefix(rel, "..") {
		rel = filepath.Base(item)
	}
	rel = filepath.ToSlash(rel)
	var b strings.Builder
	for _, r := range rel {
		if r == '/' || r == '\\' || r == ':' || unicode.IsSpace(r) || r == 0 {
			b.WriteByte('_')
		} else {
			b.WriteRune(r)
		}
	}
	name := b.String()
	if name == "" || name == "." || name == ".." {
		name = "item"
	}
	const maxName = 200
	if len(name) > maxName {
		h := fnv.New32a()
		h.Write([]byte(name))
		name = fmt.Sprintf("%s-%08x", name[:maxName-9], h.Sum32())
	}
	return name
}
