package miauth

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// NewSessionID returns a random RFC 4122 version 4 UUID. MiAuth requires the
// session id to be a UUID and forbids reuse. Generated here rather than pulling
// in a dependency for one function.
func NewSessionID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("random: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	s := hex.EncodeToString(b[:])
	return s[0:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:32], nil
}
