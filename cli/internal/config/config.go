// Package config resolves which WarpDrop server to talk to and fetches its WebRTC settings.
package config

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/pion/webrtc/v4"
)

const DefaultServer = "https://warpdrop.qzz.io"

// Config is what the user chose on the command line.
type Config struct {
	Server     string // base URL, e.g. https://warpdrop.qzz.io or http://localhost:8080
	ForceRelay bool   // skip WebRTC and use the encrypted relay
}

// Resolve applies precedence: flag > WARPDROP_SERVER env > default. A bare domain means https.
func Resolve(flag string, forceRelay bool) (Config, error) {
	s := flag
	if s == "" {
		s = os.Getenv("WARPDROP_SERVER")
	}
	if s == "" {
		s = DefaultServer
	}
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return Config{}, fmt.Errorf("invalid server %q (want https://host or host)", s)
	}
	return Config{Server: strings.TrimRight(u.String(), "/"), ForceRelay: forceRelay}, nil
}

// WSURL returns the websocket URL for a server path, e.g. "/ws" or "/relay/<ticket>".
func (c Config) WSURL(path string) string {
	u, _ := url.Parse(c.Server)
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}
	u.Path = path
	return u.String()
}

// ShareLink is the web link for a room; the secret travels in the fragment, which browsers never send.
func (c Config) ShareLink(roomID, secret string) string {
	return fmt.Sprintf("%s/r/%s#%s", c.Server, roomID, secret)
}

// Server is the server's /api/config response.
type Server struct {
	V          int `json:"v"`
	ICEServers []struct {
		URLs       []string `json:"urls"`
		Username   string   `json:"username"`
		Credential string   `json:"credential"`
	} `json:"iceServers"`
	Relay        bool `json:"relay"`
	MaxReceivers int  `json:"maxReceivers"`
}

func (s Server) ICE() []webrtc.ICEServer {
	out := make([]webrtc.ICEServer, 0, len(s.ICEServers))
	for _, i := range s.ICEServers {
		out = append(out, webrtc.ICEServer{URLs: i.URLs, Username: i.Username, Credential: i.Credential})
	}
	return out
}

// Fetch loads the server's settings. A server too old to have /api/config is an error: v1 servers cannot speak protocol v2.
func (c Config) Fetch(ctx context.Context) (Server, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Server+"/api/config", nil)
	if err != nil {
		return Server{}, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Server{}, fmt.Errorf("cannot reach %s: %w", c.Server, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Server{}, fmt.Errorf("%s answered %d for /api/config (is it a WarpDrop v2 server?)", c.Server, resp.StatusCode)
	}
	var s Server
	if err := json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 1<<20)).Decode(&s); err != nil || s.V != 2 {
		return Server{}, fmt.Errorf("%s is not a WarpDrop v2 server", c.Server)
	}
	return s, nil
}
