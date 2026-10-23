package fal

import (
	"os"
	"testing"

	"github.com/revenium/revenium-go-sdk/core/testutil/sandbox"
)

func TestMain(m *testing.M) {
	os.Exit(sandbox.Run(m))
}
