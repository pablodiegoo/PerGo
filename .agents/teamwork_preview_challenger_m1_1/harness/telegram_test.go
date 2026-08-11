package harness

import (
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/pablojhp.pergo/internal/channel/telegram"
)

type customS3Err struct {
	Code string
}

func (e *customS3Err) Error() string {
	return "s3 error: " + e.Code
}

func TestTelegramS3ErrorUnwrapping_EmpiricalChallenge(t *testing.T) {
	s3Err := &customS3Err{Code: "NoSuchKey"}
	wrappedErr := fmt.Errorf("%w: telegram media download from S3 failed: %w", telegram.ErrTelegramMediaRetryable, s3Err)

	// 1. Check errors.Is with ErrTelegramMediaRetryable
	if !errors.Is(wrappedErr, telegram.ErrTelegramMediaRetryable) {
		t.Errorf("errors.Is(wrappedErr, ErrTelegramMediaRetryable) = false, want true")
	}

	// 2. Check errors.Is with underlying s3Err
	if !errors.Is(wrappedErr, s3Err) {
		t.Errorf("errors.Is(wrappedErr, s3Err) = false, want true")
	}

	// 3. Check errors.As with custom target
	var target *customS3Err
	if !errors.As(wrappedErr, &target) || target.Code != "NoSuchKey" {
		t.Errorf("errors.As(wrappedErr, &target) failed or got wrong code: %+v", target)
	}

	// 4. Test single errors.Unwrap(wrappedErr)
	// In Go 1.20+, fmt.Errorf with multiple %w produces a joinError which implements Unwrap() []error.
	// Therefore, standard errors.Unwrap(wrappedErr) returns nil!
	unwrapped := errors.Unwrap(wrappedErr)
	if unwrapped != nil {
		t.Errorf("errors.Unwrap(wrappedErr) = %v, want nil for joinError", unwrapped)
	}

	// 5. Test multi-unwrap interface if present
	if jerr, ok := wrappedErr.(interface{ Unwrap() []error }); ok {
		errs := jerr.Unwrap()
		if len(errs) != 2 {
			t.Errorf("joinError.Unwrap() returned %d errors, want 2", len(errs))
		} else {
			if !errors.Is(errs[0], telegram.ErrTelegramMediaRetryable) {
				t.Errorf("errs[0] is not ErrTelegramMediaRetryable")
			}
			if !errors.Is(errs[1], s3Err) {
				t.Errorf("errs[1] is not s3Err")
			}
		}
	} else {
		t.Errorf("wrappedErr does not implement Unwrap() []error interface")
	}
}

func TestTelegramS3ErrorUnwrapping_EOFError(t *testing.T) {
	s3Err := io.EOF
	wrappedErr := fmt.Errorf("%w: telegram media download from S3 failed: %w", telegram.ErrTelegramMediaRetryable, s3Err)

	if !errors.Is(wrappedErr, telegram.ErrTelegramMediaRetryable) {
		t.Errorf("errors.Is for ErrTelegramMediaRetryable failed for io.EOF")
	}
	if !errors.Is(wrappedErr, io.EOF) {
		t.Errorf("errors.Is for io.EOF failed")
	}
}
