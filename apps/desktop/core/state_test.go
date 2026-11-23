package core

import (
	"path/filepath"
	"testing"
)

func TestHistoryAndSettingsSurviveRestart(t *testing.T) {
	state, dl := t.TempDir(), t.TempDir()
	open := func() *Engine {
		e, err := New(Config{Server: "http://127.0.0.1:1", Dir: dl, Device: "Default on Linux", StateDir: state}, func(string, any) {})
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	e := open()
	e.add("receive", []File{{Name: "a.txt", Size: 3}}, nil) // still running: not history
	done := e.add("send", []File{{Name: "b.txt", Size: 5}}, nil)
	e.update(done, func(r *rec) { r.t.Status, r.t.Done, r.t.Code = "done", true, "1234-secret" })
	gone := e.add("send", nil, nil)
	e.update(gone, func(r *rec) { r.t.Status, r.t.Done = "cancelled", true })
	e.Dismiss(gone)

	for _, bad := range []Settings{{Server: "ftp://x"}, {Server: "not a url"}, {Device: string(make([]rune, 65))}} {
		if err := e.SaveSettings(bad); err == nil {
			t.Errorf("SaveSettings(%+v) accepted", bad)
		}
	}
	other := filepath.Join(t.TempDir(), "in")
	if err := e.SaveSettings(Settings{Device: " Desk ", Dir: other, Server: "https://drop.example/"}); err != nil {
		t.Fatal(err)
	}
	e.Shutdown()

	e = open()
	defer e.Shutdown()
	st := e.State()
	if len(st.Transfers) != 1 || st.Transfers[0].Files[0].Name != "b.txt" || st.Transfers[0].Code != "" {
		t.Fatalf("history = %+v, want only the finished send, without its code", st.Transfers)
	}
	if want := (Settings{Device: "Desk", Dir: other, Server: "https://drop.example"}); st.Settings != want || st.Dir != other {
		t.Fatalf("settings = %+v dir %q, want %+v", st.Settings, st.Dir, want)
	}
	// clearing a field goes back to its default
	if err := e.SaveSettings(Settings{}); err != nil {
		t.Fatal(err)
	}
	if st := e.State(); st.Device != "Default on Linux" || st.Dir != dl || st.Settings.Server != "" {
		t.Fatalf("after reset: %+v", st.Settings)
	}
}
