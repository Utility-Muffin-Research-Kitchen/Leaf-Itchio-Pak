package roms

import (
	"context"
	"errors"
	"fmt"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/logger"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/netlimit"
)

// remoteRequestError prevents signed CDN URLs from escaping through an error
// shown by the Cat UI while keeping the full failure in the redacted debug log.
func remoteRequestError(operation string, err error) error {
	logger.Debug("%s request failed: %v", operation, err)
	if errors.Is(err, context.Canceled) {
		return fmt.Errorf("%s: %w", operation, context.Canceled)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%s: %w", operation, context.DeadlineExceeded)
	}
	if errors.Is(err, netlimit.ErrRateLimited) {
		// No operation prefix: the screens show this text as is.
		var limited *netlimit.RateLimitedError
		if errors.As(err, &limited) {
			return limited
		}
		return netlimit.ErrRateLimited
	}
	return fmt.Errorf("%s: network request failed", operation)
}
