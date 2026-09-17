package adapter

import (
	"errors"
	"testing"

	modelErr "mirror/internal/model/errors"
)

// TestParseStatusUnknownSource verifies that a source we have no parser for is
// reported as unsupported instead of as an empty (0 bytes, epoch) status.
func TestParseStatusUnknownSource(t *testing.T) {
	_, err := ParseStatus("rsync://example.invalid/ubuntu", "ubuntu")
	if !errors.Is(err, modelErr.ErrStatusSourceUnsupported) {
		t.Fatalf("ParseStatus() error = %v, want ErrStatusSourceUnsupported", err)
	}
}
