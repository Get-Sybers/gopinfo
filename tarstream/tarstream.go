// Package tarstream consumes the `gomount stream` pipe — a tar archive on
// stdin, one regular-file entry per volume file, entry name = the file's
// volume path — so a disk image is parsed with no mount:
//
//	gomount stream --filter 'wtmp*' disk.E01 | <tool> --tar
//
// It is the one implementation of the --tar mode the Windows tools carry as
// per-tool copies (docs/linux §3.2).
package tarstream

import (
	"archive/tar"
	"errors"
	"io"
	"time"
)

// Entry is one regular file out of the stream. R is valid only inside the
// callback; the entry name is the volume path and lands in the envelope's
// SourceFilename verbatim (rule 2: the --tar path names the origin itself).
type Entry struct {
	Name string
	Size int64
	Mod  time.Time
	R    io.Reader
}

// Each reads the tar stream and calls fn for every regular-file entry.
// A callback error stops the walk and is returned; a clean EOF is nil.
func Each(r io.Reader, fn func(e Entry) error) error {
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		if err := fn(Entry{Name: hdr.Name, Size: hdr.Size, Mod: hdr.ModTime, R: tr}); err != nil {
			return err
		}
	}
}
