package files

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateAllowsEmptyRejectsSpecial(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty.txt")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ValidateFiles([]string{empty})
	if err != nil || len(got) != 1 || got[0].Size != 0 {
		t.Fatalf("empty file must be accepted: %v %v", got, err)
	}
	if _, err := ValidateFiles([]string{dir}); err == nil {
		t.Fatal("directory must be rejected")
	}
	if _, err := ValidateFiles([]string{"/dev/zero"}); err == nil {
		t.Fatal("device file must be rejected")
	}
	if _, err := ValidateFiles([]string{filepath.Join(dir, "missing")}); err == nil {
		t.Fatal("missing file must be rejected")
	}
}
