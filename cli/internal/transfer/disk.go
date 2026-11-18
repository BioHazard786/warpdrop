// Package transfer wires the protocol to the real world: disk, signaling, WebRTC and the relay.
package transfer

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/BioHazard786/Warpdrop/cli/internal/protocol"
)

// DiskSink saves received files into one directory. Every operation goes through os.Root, so even a bug
// in name handling cannot write outside Dir (it also refuses symlinks that point out of it).
type DiskSink struct {
	root *os.Root
	Dir  string
}

func NewDiskSink(dir string) (*DiskSink, error) {
	if dir == "" {
		dir = "."
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	return &DiskSink{root: root, Dir: dir}, nil
}

func (d *DiskSink) Close() error { return d.root.Close() }

const maxCollisions = 10_000

// Open reserves a unique final name (never overwriting) and returns a writer onto "<name>.part".
// The file only gets its real name after Close, i.e. after the checksum matched.
func (d *DiskSink) Open(f protocol.NamedFile, _ int) (protocol.FileWriter, error) {
	ext := filepath.Ext(f.SafeName)
	stem := strings.TrimSuffix(f.SafeName, ext)
	for i := 0; i < maxCollisions; i++ {
		name := f.SafeName
		if i > 0 {
			name = fmt.Sprintf("%s (%d)%s", stem, i, ext)
		}
		if len(name)+len(".part") > 255 {
			name = shorten(name, 255-len(".part"))
		}
		if _, err := d.root.Lstat(name); err == nil || !errors.Is(err, fs.ErrNotExist) {
			continue // taken (or unreadable: do not risk it)
		}
		// O_EXCL on the .part name makes the reservation atomic between concurrent receivers and files
		file, err := d.root.OpenFile(name+".part", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		return &diskWriter{d: d, f: file, name: name}, nil
	}
	return nil, fmt.Errorf("no free name for %q", f.SafeName)
}

func shorten(name string, max int) string {
	ext := filepath.Ext(name)
	if len(ext) > 20 {
		ext = ""
	}
	stem := strings.TrimSuffix(name, ext)
	for len(stem)+len(ext) > max {
		stem = stem[:len(stem)-1]
	}
	return stem + ext
}

type diskWriter struct {
	d    *DiskSink
	f    *os.File
	name string
}

func (w *diskWriter) Write(p []byte) error {
	_, err := w.f.Write(p)
	return err
}

// Close finalizes: flush to disk, then rename .part → name. Refuses to replace an existing file.
func (w *diskWriter) Close() error {
	if err := w.f.Sync(); err != nil {
		_ = w.f.Close()
		return err
	}
	if err := w.f.Close(); err != nil {
		return err
	}
	if _, err := w.d.root.Lstat(w.name); err == nil {
		_ = w.d.root.Remove(w.name + ".part")
		return fmt.Errorf("%s appeared while receiving; not overwriting", w.name)
	}
	return w.d.root.Rename(w.name+".part", w.name)
}

// Abort discards the partial file.
func (w *diskWriter) Abort() error {
	_ = w.f.Close()
	return w.d.root.Remove(w.name + ".part")
}

// FinalName is where the file lands (relative to Dir).
func (w *diskWriter) FinalName() string { return w.name }
