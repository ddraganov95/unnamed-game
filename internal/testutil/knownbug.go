// Package testutil holds helpers shared by tests across packages.
package testutil

import (
	"os"
	"testing"
)

// KnownBug marks a test that reproduces a bug that has not been fixed yet.
// The test is skipped (so the suite stays green) unless RUN_KNOWN_BUGS=1,
// which runs it so the failure can be seen.
func KnownBug(t testing.TB, description string) {
	t.Helper()
	if os.Getenv("RUN_KNOWN_BUGS") == "1" {
		t.Logf("known bug (running): %s", description)
		return
	}
	t.Skip("BUG: " + description)
}
