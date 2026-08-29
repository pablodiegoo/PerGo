package telegram

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestTelegramErrorUnwrapping(t *testing.T) {
	s3Err := errors.New("s3 connection reset by peer")
	err := fmt.Errorf("%w: telegram media download from S3 failed: %v", ErrTelegramMediaRetryable, s3Err)

	// Test 1: errors.Is with ErrTelegramMediaRetryable
	if !errors.Is(err, ErrTelegramMediaRetryable) {
		t.Errorf("errors.Is(err, ErrTelegramMediaRetryable) = false, want true")
	}

	// Test 2: errors.Unwrap(err) returns ErrTelegramMediaRetryable directly
	unwrapped := errors.Unwrap(err)
	if unwrapped != ErrTelegramMediaRetryable {
		t.Errorf("errors.Unwrap(err) = %v, want %v", unwrapped, ErrTelegramMediaRetryable)
	}

	// Test 3: Inner S3 error is included in error string via %v
	if !strings.Contains(err.Error(), s3Err.Error()) {
		t.Errorf("err.Error() %q does not contain inner error %q", err.Error(), s3Err.Error())
	}

	// Test 4: errors.Is with inner S3 error returns false because %v was used
	if errors.Is(err, s3Err) {
		t.Errorf("errors.Is(err, s3Err) = true, want false (formatted with %%v)")
	}
}
