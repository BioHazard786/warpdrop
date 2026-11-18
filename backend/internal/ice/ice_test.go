package ice

import (
	"strings"
	"testing"
	"time"
)

func TestTURNCredentialKnownAnswer(t *testing.T) {
	// computed independently: printf '1700000000:abcd' | openssl dgst -sha1 -hmac test-secret -binary | base64
	if got := TURNCredential("test-secret", "1700000000:abcd"); got != "iUBcKn9xU2dwXwt6sDzZtqDfN5k=" {
		t.Fatalf("got %s", got)
	}
}

func TestBuild(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	c := Build([]string{"stun:x"}, nil, "s", true, 10, now)
	if len(c.ICEServers) != 1 {
		t.Fatalf("turn without urls must be omitted: %+v", c)
	}
	c = Build([]string{"stun:x"}, []string{"turn:y"}, "s", true, 10, now)
	if len(c.ICEServers) != 2 || !strings.HasPrefix(c.ICEServers[1].Username, "1700086400:") {
		t.Fatalf("bad turn entry: %+v", c.ICEServers)
	}
	if c = Build([]string{"stun:x"}, []string{"turn:y"}, "", true, 10, now); len(c.ICEServers) != 1 {
		t.Fatal("turn without secret must be omitted")
	}
}
