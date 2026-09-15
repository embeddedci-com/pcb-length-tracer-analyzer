package tune

import (
	"crypto/rand"
	"encoding/hex"
)

// newUUID makes a version 4 UUID for a new track.
//
// KiCad identifies every board item by one, and duplicating an existing item's
// uuid makes the file invalid in ways that are awkward to diagnose, so new
// copper always gets a fresh one.
func newUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand does not fail on any supported platform; if it somehow
		// did, a zero uuid would be worse than a panic here.
		panic("tune: cannot read randomness for a uuid: " + err.Error())
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}
