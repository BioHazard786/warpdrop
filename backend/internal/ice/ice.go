// Package ice builds the /api/config payload, minting coturn REST credentials when configured.
package ice

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // coturn's use-auth-secret scheme mandates HMAC-SHA1
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"time"
)

const CredentialTTL = 24 * time.Hour

type Server struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

type Config struct {
	V            int      `json:"v"`
	ICEServers   []Server `json:"iceServers"`
	Relay        bool     `json:"relay"`
	MaxReceivers int      `json:"maxReceivers"`
	Version      string   `json:"version,omitempty"` // the server build, set by the caller
}

// TURNCredential implements coturn's REST scheme: username = expiry:rand, password = base64(HMAC-SHA1(secret, username)).
func TURNCredential(secret, username string) string {
	m := hmac.New(sha1.New, []byte(secret))
	m.Write([]byte(username))
	return base64.StdEncoding.EncodeToString(m.Sum(nil))
}

// Build returns the config for one request. TURN servers are included only when both urls and secret are set.
func Build(stun, turn []string, secret string, relay bool, maxReceivers int, now time.Time) Config {
	c := Config{V: 2, Relay: relay, MaxReceivers: maxReceivers, ICEServers: []Server{{URLs: stun}}}
	if len(turn) > 0 && secret != "" {
		var r [4]byte
		_, _ = rand.Read(r[:])
		user := fmt.Sprintf("%d:%s", now.Add(CredentialTTL).Unix(), hex.EncodeToString(r[:]))
		c.ICEServers = append(c.ICEServers, Server{URLs: turn, Username: user, Credential: TURNCredential(secret, user)})
	}
	return c
}
