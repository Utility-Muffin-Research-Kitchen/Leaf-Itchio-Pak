// Package netlimit holds the HTTP 429 error shared by the itch.io client and
// the remote archive readers in internal/roms. It sits apart from both
// because internal/itchio imports internal/roms.
package netlimit

import (
	"errors"
	"net/http"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/logger"
)

// ErrRateLimited is matched by every error caused by an HTTP 429 that could
// not be waited out within the request's retry, deadline or refresh limits.
// Its text is shown on screen as is.
var ErrRateLimited = errors.New("itch.io is limiting requests. Wait a minute, then try again.")

// RateLimitedError reports that a host asked the app to slow down and the
// request could not be sent, or retried, within its limits. Host is for logs
// only; the text is the on-screen ErrRateLimited text. It matches
// ErrRateLimited with errors.Is.
type RateLimitedError struct {
	Host string
}

func (err *RateLimitedError) Error() string { return ErrRateLimited.Error() }

func (err *RateLimitedError) Is(target error) bool { return target == ErrRateLimited }

// FromResponse turns an HTTP 429 that reached the caller into a
// RateLimitedError. The itch.io client's transport has already waited and
// replayed what it may by then.
func FromResponse(operation string, resp *http.Response) error {
	host := ""
	if resp.Request != nil && resp.Request.URL != nil {
		host = resp.Request.URL.Host
	}
	// Never log the path or query: API paths and signed CDN URLs carry
	// credentials.
	logger.Warn("%s: HTTP 429 from %s", operation, host)
	return &RateLimitedError{Host: host}
}
