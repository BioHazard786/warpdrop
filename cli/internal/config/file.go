package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Source says where a setting came from, for `warpdrop config get`.
type Source string

const (
	FromFlag    Source = "flag"
	FromEnv     Source = "environment"
	FromFile    Source = "config file"
	FromDefault Source = "default"
)

// File is what `warpdrop config set` saves.
type File struct {
	Server string `json:"server,omitempty"`
}

// Path is $WARPDROP_CONFIG, else <user config dir>/warpdrop/config.json
// (~/.config on Linux, ~/Library/Application Support on macOS, %AppData% on Windows).
func Path() (string, error) {
	if p := os.Getenv("WARPDROP_CONFIG"); p != "" {
		return p, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("cannot find your config directory (set WARPDROP_CONFIG to choose a file): %w", err)
	}
	return filepath.Join(dir, "warpdrop", "config.json"), nil
}

// Load reads the config file; a missing file is an empty File, not an error.
func Load() (File, error) {
	path, err := Path()
	if err != nil {
		return File{}, err
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return File{}, nil
	}
	if err != nil {
		return File{}, err
	}
	var f File
	if err := json.Unmarshal(b, &f); err != nil {
		return File{}, fmt.Errorf("%s is not valid JSON (fix or delete it): %w", path, err)
	}
	return f, nil
}

// Save writes the file atomically (temp file + rename).
func (f File) Save() error {
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Pick chooses the server setting by precedence, highest first: flag, $WARPDROP_SERVER, saved file, default.
// The result still needs Resolve/NormalizeServer.
func Pick(flag string) (string, Source, error) {
	if flag != "" {
		return flag, FromFlag, nil
	}
	if env := os.Getenv("WARPDROP_SERVER"); env != "" {
		return env, FromEnv, nil
	}
	f, err := Load()
	if err != nil {
		return "", "", err
	}
	if f.Server != "" {
		return f.Server, FromFile, nil
	}
	return DefaultServer, FromDefault, nil
}
