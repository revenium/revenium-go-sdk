package metering

import (
	"crypto/rand"
	"fmt"
	"time"
)

func GenerateTransactionID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		b = fallbackRandom()
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func fallbackRandom() []byte {
	b := make([]byte, 16)
	ts := time.Now().UnixNano()
	for i := range b {
		ts = ts*6364136223846793005 + 1442695040888963407
		b[i] = byte(ts >> 33)
	}
	return b
}
