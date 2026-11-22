package config

import (
	"path/filepath"
	"testing"
)

func TestPickPrecedence(t *testing.T) {
	t.Setenv("WARPDROP_CONFIG", filepath.Join(t.TempDir(), "c.json"))
	t.Setenv("WARPDROP_SERVER", "")

	if s, src, _ := Pick(""); s != DefaultServer || src != FromDefault {
		t.Fatalf("nothing set: got %q from %s", s, src)
	}
	if err := (File{Server: "saved.example"}).Save(); err != nil {
		t.Fatal(err)
	}
	if s, src, _ := Pick(""); s != "saved.example" || src != FromFile {
		t.Fatalf("saved: got %q from %s", s, src)
	}
	t.Setenv("WARPDROP_SERVER", "env.example")
	if s, src, _ := Pick(""); s != "env.example" || src != FromEnv {
		t.Fatalf("env beats file: got %q from %s", s, src)
	}
	if s, src, _ := Pick("flag.example"); s != "flag.example" || src != FromFlag {
		t.Fatalf("flag beats env: got %q from %s", s, src)
	}
}

func TestNormalizeServer(t *testing.T) {
	for in, want := range map[string]string{"drop.example": "https://drop.example", "http://localhost:8080/": "http://localhost:8080"} {
		if got, err := NormalizeServer(in); err != nil || got != want {
			t.Errorf("%q: got %q, %v", in, got, err)
		}
	}
	if _, err := NormalizeServer("ftp://x"); err == nil {
		t.Error("ftp accepted")
	}
}
