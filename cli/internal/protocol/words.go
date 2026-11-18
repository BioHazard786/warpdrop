package protocol

import (
	"crypto/rand"
	_ "embed"
	"math/big"
	"regexp"
	"strings"
)

//go:embed words.txt
var wordsTxt string

// words is the EFF large wordlist minus hyphenated entries (same list as the web app), ~51.8 bits per 4 words.
var words = strings.Fields(wordsTxt)

var wordSet = func() map[string]bool {
	m := make(map[string]bool, len(words))
	for _, w := range words {
		m[w] = true
	}
	return m
}()

// GenerateSecret returns 4 uniformly random words joined by "-".
func GenerateSecret() string {
	out := make([]string, 4)
	max := big.NewInt(int64(len(words)))
	for i := range out {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			panic(err)
		}
		out[i] = words[n.Int64()]
	}
	return strings.Join(out, "-")
}

func IsValidSecret(s string) bool {
	p := strings.Split(s, "-")
	if len(p) != 4 {
		return false
	}
	for _, w := range p {
		if !wordSet[w] {
			return false
		}
	}
	return true
}

var (
	linkRe = regexp.MustCompile(`/r/(\d{4,6})(?:#([a-z-]+))?\s*$`)
	codeRe = regexp.MustCompile(`^(\d{4,6})-([a-z-]+)$`)
	roomRe = regexp.MustCompile(`^\d{4,6}$`)
)

// Share is a parsed share code or link. Secret is empty when the input had none.
type Share struct{ RoomID, Secret string }

// ParseShare accepts "4821-amber-otter-kite-moth", a full link, or a bare room number.
func ParseShare(input string) (Share, bool) {
	s := strings.TrimSpace(input)
	if m := linkRe.FindStringSubmatch(s); m != nil {
		sec := ""
		if IsValidSecret(m[2]) {
			sec = m[2]
		}
		return Share{m[1], sec}, true
	}
	if m := codeRe.FindStringSubmatch(s); m != nil && IsValidSecret(m[2]) {
		return Share{m[1], m[2]}, true
	}
	if roomRe.MatchString(s) {
		return Share{s, ""}, true
	}
	return Share{}, false
}
