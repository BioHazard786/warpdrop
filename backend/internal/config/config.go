// Package config reads all server settings from the environment.
package config

import (
	"net/netip"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Port           string
	AllowedOrigins []string       // extra origins besides same-host
	TrustedProxies []netip.Prefix // X-Forwarded-For is honored only from these
	RelayEnabled   bool
	RelayMaxMbps   int // per connection, 0 = unlimited
	RelayMaxConns  int
	STUNURLs       []string
	TURNURLs       []string
	TURNSecret     string
	// RateScale multiplies the per-IP limits on room creation, joining and relay requests (default 1).
	// Raise it when many users share one address (office or school NAT); lower it to be stricter.
	RateScale float64
	// AndroidCertSHA256 are the signing-certificate fingerprints of the Android app, served at
	// /.well-known/assetlinks.json so share links open the app. Empty = the file is not served.
	AndroidPackage    string
	AndroidCertSHA256 []string
	LogLevel          string // debug|info|warn|error (default info)
	LogFormat         string // json|text (default json)
}

func Load() Config {
	return Config{
		Port:              get("PORT", "8080"),
		AllowedOrigins:    list(os.Getenv("ALLOWED_ORIGINS")),
		TrustedProxies:    prefixes(os.Getenv("TRUSTED_PROXIES")),
		RelayEnabled:      !strings.EqualFold(get("RELAY", "on"), "off"),
		RelayMaxMbps:      atoi(os.Getenv("RELAY_MAX_MBPS"), 0),
		RelayMaxConns:     atoi(os.Getenv("RELAY_MAX_CONNS"), 200),
		STUNURLs:          listOr(os.Getenv("STUN_URLS"), "stun:stun.l.google.com:19302"),
		TURNURLs:          list(os.Getenv("TURN_URLS")),
		TURNSecret:        os.Getenv("TURN_SECRET"),
		RateScale:         scale(os.Getenv("RATE_LIMIT_SCALE")),
		AndroidPackage:    get("ANDROID_PACKAGE", "com.biohazard786.warpdrop"),
		AndroidCertSHA256: list(os.Getenv("ANDROID_CERT_SHA256")),
		LogLevel:          get("LOG_LEVEL", "info"),
		LogFormat:         get("LOG_FORMAT", "json"),
	}
}

func get(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func atoi(s string, def int) int {
	if n, err := strconv.Atoi(s); err == nil && n >= 0 {
		return n
	}
	return def
}

func list(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func listOr(s, def string) []string {
	if l := list(s); len(l) > 0 {
		return l
	}
	return []string{def}
}

// prefixes parses CIDRs or bare IPs; invalid entries are skipped.
func prefixes(s string) []netip.Prefix {
	var out []netip.Prefix
	for _, p := range list(s) {
		if pre, err := netip.ParsePrefix(p); err == nil {
			out = append(out, pre)
		} else if a, err := netip.ParseAddr(p); err == nil {
			out = append(out, netip.PrefixFrom(a, a.BitLen()))
		}
	}
	return out
}

// scale parses RATE_LIMIT_SCALE; anything missing, non-numeric or not positive means 1.
func scale(s string) float64 {
	if f, err := strconv.ParseFloat(s, 64); err == nil && f > 0 && f <= 1000 {
		return f
	}
	return 1
}
