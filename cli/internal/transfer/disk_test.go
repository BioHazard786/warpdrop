package transfer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BioHazard786/Warpdrop/cli/internal/protocol"
)

func named(name string) protocol.NamedFile {
	return protocol.NamedFile{FileEntry: protocol.FileEntry{Name: name}, SafeName: protocol.SanitizeName(name)}
}

func TestHostileNamesCannotEscapeTheOutputDir(t *testing.T) {
	parent := t.TempDir()
	out := filepath.Join(parent, "out")
	d, err := NewDiskSink(out)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	for _, name := range []string{"../../etc/passwd", `..\..\evil.txt`, "/abs/path/x", "a/../../b", "\x1b[2Jboom", "CON", "..", ".", ""} {
		w, err := d.Open(named(name), 0)
		if err != nil {
			t.Fatalf("%q: %v", name, err)
		}
		_ = w.Write([]byte("x"))
		if err := w.Close(); err != nil {
			t.Fatalf("%q: %v", name, err)
		}
	}
	// everything landed directly inside out/ and nothing outside it
	if entries, _ := os.ReadDir(parent); len(entries) != 1 || entries[0].Name() != "out" {
		t.Fatalf("something was written outside the output dir: %v", entries)
	}
	got, _ := os.ReadDir(out)
	for _, e := range got {
		if e.IsDir() || strings.ContainsAny(e.Name(), `/\`) || strings.HasSuffix(e.Name(), ".part") {
			t.Errorf("unexpected entry %q", e.Name())
		}
	}
	if len(got) != 9 {
		t.Fatalf("want 9 files, got %d", len(got))
	}
}

func TestNeverOverwritesAndDedupes(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("precious"), 0o644); err != nil {
		t.Fatal(err)
	}
	d, _ := NewDiskSink(dir)
	defer d.Close()
	var names []string
	for _, content := range []string{"one", "two", "three"} {
		w, err := d.Open(named("a.txt"), 0)
		if err != nil {
			t.Fatal(err)
		}
		_ = w.Write([]byte(content))
		names = append(names, w.(*diskWriter).FinalName())
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(b) != "precious" {
		t.Fatal("existing file was overwritten")
	}
	if strings.Join(names, ",") != "a (1).txt,a (2).txt,a (3).txt" {
		t.Fatalf("names %v", names)
	}
}

func TestConcurrentSameNameFilesGetDistinctNames(t *testing.T) {
	d, _ := NewDiskSink(t.TempDir())
	defer d.Close()
	w1, _ := d.Open(named("x"), 0) // both open before either finishes: the .part reservation must separate them
	w2, err := d.Open(named("x"), 1)
	if err != nil {
		t.Fatal(err)
	}
	if w1.(*diskWriter).name == w2.(*diskWriter).name {
		t.Fatal("two open writers share a name")
	}
}

func TestAbortLeavesNothingBehind(t *testing.T) {
	dir := t.TempDir()
	d, _ := NewDiskSink(dir)
	defer d.Close()
	w, _ := d.Open(named("big.bin"), 0)
	_ = w.Write([]byte("partial"))
	if err := w.Abort(); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("leftovers: %v", entries)
	}
}

func TestRefusesToFollowSymlinkOutOfDir(t *testing.T) {
	parent := t.TempDir()
	out := filepath.Join(parent, "out")
	_ = os.MkdirAll(out, 0o755)
	outside := filepath.Join(parent, "outside")
	_ = os.MkdirAll(outside, 0o755)
	if err := os.Symlink(outside, filepath.Join(out, "x.part")); err != nil {
		t.Skip("symlinks unavailable")
	}
	d, _ := NewDiskSink(out)
	defer d.Close()
	w, err := d.Open(named("x"), 0) // "x.part" is a planted symlink: must not be followed
	if err == nil {
		_ = w.Write([]byte("pwned"))
		_ = w.Close()
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Fatalf("wrote through a symlink: %v", entries)
	}
}
