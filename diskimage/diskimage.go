// Disk images as items. The parsers read artefact FILES; a disk image under
// the input tree is not one, it holds them. This file makes the image the
// item: the artefact sets the selected parsers read are pulled out of the
// image's OS volume by the baked-in gomount (materialise — no mount, no FUSE,
// no privilege, the image opened read-only) into a scratch tree under the
// work dir, the ordinary batch loop runs over that tree exactly as it would
// over a loose evidence folder, the records land under <OUT_DIR>/…/<image>/,
// and the scratch tree goes. Nothing is exported to the output tree: the
// parsers run on the image.
//
// Selection: GOWINDOWLICKER_IMAGE (a sub-tool run: <SUBTOOL>_IMAGE) names ONE
// image relative to INPUT_DIR; empty means every disk image found directly
// under INPUT_DIR (an EWF set's first segment, a VMDK descriptor or
// monolithic extent — never a -flat/-sNNN extent or an .E02… segment) plus,
// for the sweep, the loose artefacts of the tree itself.
package diskimage

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// ImageExt matches every container gomount decodes, by extension.
var ImageExt = regexp.MustCompile(`(?i)\.(e01|ex01|raw|dd|img|vmdk|vhd|vhdx|qcow2|qcow|vdi|dmg|sparseimage|aff4|001|bin)$`)

// ImagePart matches the parts of ANOTHER item: a VMDK's flat or split
// extents, an EWF set's continuation segments.
var ImagePart = regexp.MustCompile(`(?i)(-flat\.vmdk|-s[0-9]{3,}\.vmdk|\.e(0[2-9]|[1-9][0-9]|[a-z]{2}))$`)

// gomountBinary is where the image bakes gomount; GOMOUNT_BIN overrides it
// (tests run a stub on the host).
const gomountBinary = "/usr/local/bin/gomount"

func GomountPath(getenv func(string) string) string {
	if p := getenv("GOMOUNT_BIN"); p != "" {
		return p
	}
	if _, err := os.Stat(gomountBinary); err == nil {
		return gomountBinary
	}
	if p, err := exec.LookPath("gomount"); err == nil {
		return p
	}
	return gomountBinary
}

// IsImageItem reports whether path names a disk image that is an item.
func IsImageItem(path string) bool {
	return ImageExt.MatchString(path) && !ImagePart.MatchString(path)
}

// DiscoverImages lists the disk-image items directly under dir, sorted.
func DiscoverImages(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.Type().IsRegular() && IsImageItem(e.Name()) {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(out)
	return out
}

// ImageItemName folds an image path (relative to the input root) into one
// output folder name, the batch runtime's item-naming rule: separators,
// whitespace and ':' become '_'.
func ImageItemName(root, image string) string {
	rel, err := filepath.Rel(root, image)
	if err != nil || rel == "." || rel == "" || strings.HasPrefix(rel, "..") {
		rel = filepath.Base(image)
	}
	var b strings.Builder
	for _, r := range filepath.ToSlash(rel) {
		if r == '/' || r == '\\' || r == ':' || unicode.IsSpace(r) || r == 0 {
			b.WriteByte('_')
		} else {
			b.WriteRune(r)
		}
	}
	if s := b.String(); s != "" && s != "." && s != ".." {
		return s
	}
	return "image"
}

// MaterialiseImage pulls the artefact sets out of image into the scratch
// tree <work>/<image item> and returns it. The name is deterministic — the
// records' origin paths read <work>/<image>/<volume path>, and a tree a
// killed run left behind is cleared before the pull. gomount's exit 1
// (nothing on the volume matched) is not an error: the tree is simply empty
// and the parsers find nothing, like a loose folder without their artefact.
func MaterialiseImage(getenv func(string) string, work, image string, sets []string) (string, error) {
	scratch := filepath.Join(work, ImageItemName(filepath.Dir(image), image))
	if err := os.RemoveAll(scratch); err != nil {
		return "", fmt.Errorf("clear scratch %s: %w", scratch, err)
	}
	if err := os.MkdirAll(scratch, 0o755); err != nil {
		return "", fmt.Errorf("scratch under %s: %w", work, err)
	}
	args := []string{"materialise", "--out", scratch}
	for _, s := range sets {
		args = append(args, "--set", s)
	}
	args = append(args, image)
	cmd := exec.Command(GomountPath(getenv), args...)
	cmd.Stderr = os.Stderr
	cmd.Stdout = nil // its summary line is not ours to print
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && ee.ExitCode() == 1 {
			return scratch, nil // nothing pulled
		}
		os.RemoveAll(scratch)
		return "", fmt.Errorf("gomount materialise %s: %w", filepath.Base(image), err)
	}
	return scratch, nil
}

// SelectedImages resolves the IMAGE setting against the input tree: one
// named image, or every image item directly under it.
func SelectedImages(inputDir, selected string) ([]string, error) {
	if selected == "" {
		return DiscoverImages(inputDir), nil
	}
	p := selected
	if !filepath.IsAbs(p) {
		p = filepath.Join(inputDir, filepath.FromSlash(selected))
	}
	st, err := os.Stat(p)
	if err != nil {
		return nil, fmt.Errorf("IMAGE %s: %w", selected, err)
	}
	if !st.Mode().IsRegular() {
		return nil, fmt.Errorf("IMAGE %s: not a file", selected)
	}
	// the same rule discovery applies: a VMDK's -flat/-sNNN extent or an
	// EWF set's .E02… segment is a part of another item — name that item
	if ImagePart.MatchString(p) {
		return nil, fmt.Errorf("IMAGE %s: a part of another image (a VMDK extent or an EWF segment) — name its descriptor or first segment", selected)
	}
	if !ImageExt.MatchString(p) {
		return nil, fmt.Errorf("IMAGE %s: not a disk image by name (%s)", selected, ImageExt.String())
	}
	return []string{p}, nil
}
