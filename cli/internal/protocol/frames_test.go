package protocol

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func fixture(t *testing.T, name string, v any) {
	t.Helper()
	b, err := os.ReadFile("../../../protocol/fixtures/" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatal(err)
	}
}

func TestSanitizeNameMatchesSharedFixture(t *testing.T) {
	var tr map[string]struct {
		Steps []struct {
			Frame struct{ Files []FileEntry } `json:"frame"`
		} `json:"steps"`
		Expect struct {
			Files []struct {
				ID   int
				Name string
			}
		} `json:"expect"`
	}
	fixture(t, "transcripts", &tr)
	h := tr["hostile-names"]
	byID := map[int]string{}
	for _, f := range h.Steps[0].Frame.Files {
		byID[f.ID] = f.Name
	}
	if len(h.Expect.Files) < 7 {
		t.Fatal("fixture shrank")
	}
	for _, e := range h.Expect.Files {
		if got := SanitizeName(byID[e.ID]); got != e.Name {
			t.Errorf("id %d: SanitizeName(%q) = %q, want %q", e.ID, byID[e.ID], got, e.Name)
		}
	}
}

func TestSanitizeNameNeverUnsafe(t *testing.T) {
	for _, in := range []string{"", ".", "..", "...", "a/../b", `C:\x\y`, "NUL.txt", "aux", strings.Repeat("x", 400), "\x00", strings.Repeat("é", 200), "\x1b[2J", "a\u202eb"} {
		n := SanitizeName(in)
		if n == "" || len(n) > MaxNameBytes || strings.ContainsAny(n, `/\`) || strings.Trim(n, ".") == "" {
			t.Errorf("SanitizeName(%q) = %q", in, n)
		}
		for _, r := range n {
			if unsafeRune(r) {
				t.Errorf("SanitizeName(%q) kept control rune %U", in, r)
			}
		}
	}
}

func TestParseFrameRejectsHostile(t *testing.T) {
	for _, bad := range []string{
		"not json", "{}", `{"t":"nope"}`, `{"t":"request","id":-1,"offset":0}`, `{"t":"request","id":0,"offset":1.5}`,
		`{"t":"request","id":"0","offset":0}`, `{"t":"manifest","files":[]}`,
		`{"t":"manifest","files":[{"id":0,"name":"a","size":-1,"type":""}]}`,
		`{"t":"manifest","files":[{"id":0,"name":"a","size":1e400,"type":""}]}`,
		`{"t":"manifest","files":[{"id":0,"name":"a","size":1,"type":""},{"id":0,"name":"b","size":1,"type":""}]}`,
		`{"t":"file_end","id":0,"hash":"xyz"}`, `{"t":"cancel","reason":"because"}`, `{"t":"auth","mac":"zz"}`,
		`{"t":"hello","v":2,"client":"c","device":"d","pub":"abcd"}`,
	} {
		if _, err := ParseFrame(bad); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
	f, err := ParseFrame(`{"t":"request","id":0,"offset":0,"extra":true}`)
	if err != nil || f != NewRequest(0, 0) {
		t.Fatalf("valid frame with unknown field must parse: %v %v", f, err)
	}
}

func TestEncodeKeepsZeroValues(t *testing.T) {
	if got := Encode(NewRequest(0, 0)); got != `{"t":"request","id":0,"offset":0}` {
		t.Fatal(got)
	}
	if got := Encode(NewAuth(strings.Repeat("a", 64))); !strings.HasPrefix(got, `{"t":"auth","mac":"a`) {
		t.Fatal(got)
	}
}

func TestShareCodes(t *testing.T) {
	s := GenerateSecret()
	if !IsValidSecret(s) {
		t.Fatal(s)
	}
	for in, want := range map[string]Share{
		"4821-" + s:                     {"4821", s},
		"https://x.example/r/4821#" + s: {"4821", s},
		"https://x.example/r/4821":      {"4821", ""},
		"4821":                          {"4821", ""},
	} {
		if got, ok := ParseShare(in); !ok || got != want {
			t.Errorf("ParseShare(%q) = %v %v", in, got, ok)
		}
	}
	for _, in := range []string{"4821-notwords-here-at-all", "hello", ""} {
		if _, ok := ParseShare(in); ok {
			t.Errorf("accepted %q", in)
		}
	}
	if len(words) != 7772 {
		t.Fatalf("wordlist has %d words", len(words))
	}
}
