package protocol

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
)

// Ephemeral is a one-shot X25519 key pair.
type Ephemeral struct{ priv *ecdh.PrivateKey }

func NewEphemeral() (*Ephemeral, error) {
	k, err := ecdh.X25519().GenerateKey(rand.Reader)
	return &Ephemeral{k}, err
}

// EphemeralFromSeed builds a key from 32 fixed bytes (test vectors only).
func EphemeralFromSeed(seed []byte) (*Ephemeral, error) {
	k, err := ecdh.X25519().NewPrivateKey(seed)
	return &Ephemeral{k}, err
}

func (e *Ephemeral) Public() []byte { return e.priv.PublicKey().Bytes() }

// Keys are derived from the shared secret words, the room id and the ECDH output.
type Keys struct{ Root, Auth, Session []byte }

func DeriveKeys(secret, roomID string, own *Ephemeral, peerPub []byte) (Keys, error) {
	pub, err := ecdh.X25519().NewPublicKey(peerPub)
	if err != nil {
		return Keys{}, err
	}
	shared, err := own.priv.ECDH(pub)
	if err != nil {
		return Keys{}, err
	}
	root, err := hkdf.Key(sha256.New, []byte(secret), []byte(roomID), "warpdrop/2/root", 32)
	if err != nil {
		return Keys{}, err
	}
	k, err := hkdf.Key(sha256.New, shared, root, "warpdrop/2", 64)
	if err != nil {
		return Keys{}, err
	}
	return Keys{Root: root, Auth: k[:32], Session: k[32:]}, nil
}

var fpRe = regexp.MustCompile(`(?m)^a=fingerprint:sha-256 ([0-9A-Fa-f:]+)`)

// DTLSFingerprint returns the raw sha-256 digest from an SDP a=fingerprint line, or nil.
func DTLSFingerprint(sdp string) []byte {
	m := fpRe.FindStringSubmatch(sdp)
	if m == nil {
		return nil
	}
	b, err := hex.DecodeString(strings.ReplaceAll(m[1], ":", ""))
	if err != nil {
		return nil
	}
	return b
}

// Transcript = "warpdrop/2" ‖ senderPub ‖ receiverPub ‖ senderFp ‖ receiverFp (fingerprints omitted on the relay).
func Transcript(senderPub, receiverPub, senderFp, receiverFp []byte) []byte {
	t := append([]byte("warpdrop/2"), senderPub...)
	t = append(t, receiverPub...)
	t = append(t, senderFp...)
	return append(t, receiverFp...)
}

// MAC returns hex(HMAC-SHA256(authKey, label ‖ transcript)). label is "recv" or "send".
func MAC(authKey []byte, label string, transcript []byte) string {
	m := hmac.New(sha256.New, authKey)
	m.Write([]byte(label))
	m.Write(transcript)
	return hex.EncodeToString(m.Sum(nil))
}

// VerifyMAC compares in constant time.
func VerifyMAC(authKey []byte, label string, transcript []byte, macHex string) bool {
	want, err := hex.DecodeString(MAC(authKey, label, transcript))
	got, err2 := hex.DecodeString(macHex)
	return err == nil && err2 == nil && hmac.Equal(want, got)
}

// Relay frame kinds inside the AES-GCM plaintext.
const (
	KindText      byte = 0
	KindBinary    byte = 1
	KindKeepalive byte = 2
)

// Direction values for the nonce prefix.
const (
	DirSenderToReceiver uint32 = 1
	DirReceiverToSender uint32 = 2
)

// FrameCipher seals or opens relay frames for ONE direction. Nonce = dir(4) ‖ counter(8), counter starts at 0
// and must match exactly, so replay, reorder and drops are all rejected.
type FrameCipher struct {
	mu      sync.Mutex
	aead    cipher.AEAD
	dir     uint32
	counter uint64
}

func NewFrameCipher(sessionKey []byte, dir uint32) (*FrameCipher, error) {
	b, err := aes.NewCipher(sessionKey)
	if err != nil {
		return nil, err
	}
	g, err := cipher.NewGCM(b)
	return &FrameCipher{aead: g, dir: dir}, err
}

func (c *FrameCipher) nonce() []byte {
	n := make([]byte, 12)
	binary.BigEndian.PutUint32(n, c.dir)
	binary.BigEndian.PutUint64(n[4:], c.counter)
	return n
}

func (c *FrameCipher) Seal(kind byte, payload []byte) []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	pt := append([]byte{kind}, payload...)
	out := c.aead.Seal(nil, c.nonce(), pt, nil)
	c.counter++
	return out
}

var ErrDecrypt = errors.New("relay frame failed authentication")

func (c *FrameCipher) Open(wire []byte) (kind byte, payload []byte, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	pt, err := c.aead.Open(nil, c.nonce(), wire, nil)
	if err != nil {
		return 0, nil, ErrDecrypt
	}
	c.counter++
	if len(pt) < 1 || pt[0] > KindKeepalive {
		return 0, nil, fmt.Errorf("%w: bad kind", ErrDecrypt)
	}
	return pt[0], pt[1:], nil
}
