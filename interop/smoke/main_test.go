//go:build interop

package smoke

import (
	"os"
	"testing"

	"github.com/CaffeinatedTech/go-imap/interop/harness"
)

func TestMain(m *testing.M) {
	os.Exit(harness.Run(m, harness.Profiles()))
}
