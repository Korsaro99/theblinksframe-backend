// Package tasktest marks acceptance tests written ahead of their implementation (see
// docs/tasks/README.md).
package tasktest

import (
	"os"
	"testing"
)

// Pending skips the test unless it is being run for its task: `make task TASK=T03` (or
// TASK=all). When the task is done, delete the Pending line so CI runs the test from then on.
func Pending(t *testing.T, id string) {
	t.Helper()
	if task := os.Getenv("TASK"); task != id && task != "all" {
		t.Skipf("task %s is not implemented yet; run `make task TASK=%s`", id, id)
	}
}
