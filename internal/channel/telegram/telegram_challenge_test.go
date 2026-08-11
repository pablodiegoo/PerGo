package telegram

import (
	"errors"
	"fmt"
	"testing"
)

func TestTelegramErrorUnwrapping_Empirical(t *testing.T) {
	s3Err := errors.New("s3 connection reset by peer")
	err := fmt.Errorf("%w: telegram media download from S3 failed: %w", ErrTelegramMediaRetryable, s3Err)

	// Test 1: errors.Is with ErrTelegramMediaRetryable
	if !errors.Is(err, ErrTelegramMediaRetryable) {
		t.Errorf("errors.Is(err, ErrTelegramMediaRetryable) = false, want true")
	}

	// Test 2: errors.Is with underlying S3 error
	if !errors.Is(err, s3Err) {
		t.Errorf("errors.Is(err, s3Err) = false, want true")
	}

	// Test 3: errors.Unwrap(err) behavior check
	singleUnwrap := errors.Unwrap(err)
	t.Logf("errors.Unwrap(err) returned: %v", singleUnwrap)

	// Note: In Go 1.20+, multi-%w fmt.Errorf implements Unwrap() []error.
	// Single errors.Unwrap(err) returns nil for multi-wrapped errors, while errors.Is() properly unwraps []error.
}
