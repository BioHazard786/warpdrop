package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/BioHazard786/Warpdrop/cli/mobile"
)

const (
	settingsFile = "desktop-settings.json"
	historyFile  = "desktop-history.json"
	historyKeep  = 200 // newest finished transfers kept across restarts
)

// Settings is what the Settings page saves. Empty fields use the defaults.
type Settings struct {
	Device string `json:"device"`
	Dir    string `json:"dir"`
	Server string `json:"server"` // "" = the default (WARPDROP_SERVER or the hosted one)
}

// SaveSettings checks, applies and saves settings. A new name or server restarts discovery so others see the change.
func (e *Engine) SaveSettings(s Settings) error {
	s.Device, s.Dir, s.Server = strings.TrimSpace(s.Device), strings.TrimSpace(s.Dir), strings.TrimRight(strings.TrimSpace(s.Server), "/")
	if len([]rune(s.Device)) > 64 {
		return errors.New("keep the device name under 64 characters")
	}
	if s.Server != "" {
		if u, err := url.Parse(s.Server); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return errors.New("the server must be an http:// or https:// address")
		}
	}
	eff, err := e.effective(s)
	if err != nil {
		return err
	}
	_, _, cur := e.conn()
	var sink *mobile.DirSink
	if eff.Dir != cur.Dir() {
		if sink, err = mobile.NewDirSink(eff.Dir); err != nil {
			return fmt.Errorf("cannot save files there: %w", err)
		}
	}
	if err := e.writeJSON(settingsFile, s); err != nil {
		if sink != nil {
			_ = sink.Close()
		}
		return fmt.Errorf("could not save settings: %w", err)
	}
	e.mu.Lock()
	if sink != nil {
		e.oldSinks, e.sink = append(e.oldSinks, e.sink), sink
	}
	restart := e.device != eff.Device || e.server != eff.Server
	e.saved, e.device, e.server = s, eff.Device, eff.Server
	visible := e.visible
	e.mu.Unlock()
	if restart {
		return e.SetVisible(visible)
	}
	return nil
}

// loadHistory brings back the transfers that finished in earlier runs, oldest first, with fresh ids.
func (e *Engine) loadHistory() {
	var ts []Transfer
	e.readJSON(historyFile, &ts)
	for _, t := range ts {
		if !t.Done {
			continue
		}
		e.seq++
		t.ID = fmt.Sprintf("t%d", e.seq)
		e.transfers[t.ID] = &rec{t: t, decide: make(chan bool, 1)}
		e.order = append(e.order, t.ID)
	}
}

// saveHistory writes every finished transfer (the newest historyKeep), minus share codes that no longer work.
func (e *Engine) saveHistory() {
	if e.stateDir == "" {
		return
	}
	e.saveMu.Lock() // held across snapshot and write, so an older snapshot can't land after a newer one
	defer e.saveMu.Unlock()
	e.mu.Lock()
	ts := []Transfer{}
	for _, id := range e.order {
		if t := e.transfers[id].t; t.Done {
			t.Code, t.Link, t.SAS = "", "", ""
			ts = append(ts, t)
		}
	}
	e.mu.Unlock()
	if len(ts) > historyKeep {
		ts = ts[len(ts)-historyKeep:]
	}
	_ = e.writeJSON(historyFile, ts) // best effort: losing history must never break a transfer
}

// readJSON fills v from a state file; a missing or broken file leaves v as it was.
func (e *Engine) readJSON(name string, v any) {
	if e.stateDir == "" {
		return
	}
	if b, err := os.ReadFile(filepath.Join(e.stateDir, name)); err == nil {
		_ = json.Unmarshal(b, v)
	}
}

// writeJSON replaces a state file atomically (temp file + rename).
func (e *Engine) writeJSON(name string, v any) error {
	if e.stateDir == "" {
		return nil
	}
	if err := os.MkdirAll(e.stateDir, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(e.stateDir, name+".*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(e.stateDir, name))
}
