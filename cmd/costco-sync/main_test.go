package main

import (
	"errors"
	"testing"

	"code.sirenko.ca/grocer/internal/costcosync"
)

// Partial success must be saved even though the process exits non-zero:
// a run with 28 created + 1 failed keeps the 28 instead of refetching
// everything next time.
func TestFinalizeSavesBeforeFailureExit(t *testing.T) {
	saved := false
	code := finalize(costcosync.Summary{Created: 28, Failed: []costcosync.FailedBarcode{{Barcode: "b", Err: "x"}}}, false, func() error {
		saved = true
		return nil
	})
	if !saved {
		t.Fatalf("snapshot must be pushed before the failure exit")
	}
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
}

func TestFinalizeCleanRun(t *testing.T) {
	saved := false
	code := finalize(costcosync.Summary{Created: 2}, false, func() error {
		saved = true
		return nil
	})
	if !saved {
		t.Fatalf("clean run must push")
	}
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
}

func TestFinalizeDryRunNeverPushes(t *testing.T) {
	code := finalize(costcosync.Summary{Created: 2}, true, func() error {
		t.Fatalf("dry run must not push")
		return nil
	})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
}

func TestFinalizeSaveError(t *testing.T) {
	code := finalize(costcosync.Summary{Created: 1}, false, func() error {
		return errors.New("boom")
	})
	if code != 1 {
		t.Fatalf("exit code = %d, want 1 on save error", code)
	}
}
