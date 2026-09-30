//go:build interop_emulated

package harness

import (
	"github.com/CaffeinatedTech/go-imap/interop/definition"
	"github.com/CaffeinatedTech/go-imap/interop/servers/james"
)

var emulatedProfiles = []definition.Profile{james.Profile}
