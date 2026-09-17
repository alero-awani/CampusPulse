// Package ids generates random identifiers such as "evt-3f9c0a7d51e24b8c9a10".
package ids

import (
	"crypto/rand"
	"encoding/hex"
)

// New returns prefix, a dash, and 20 random hex characters.
func New(prefix string) string {
	b := make([]byte, 10)
	_, _ = rand.Read(b) // crypto/rand.Read never returns an error
	return prefix + "-" + hex.EncodeToString(b)
}
