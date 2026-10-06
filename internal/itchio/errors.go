package itchio

import (
	"errors"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/netlimit"
)

// ErrCloudflareBlocked is returned when itch.io responds with HTTP 403,
// indicating Cloudflare bot-protection rejected the request.
var ErrCloudflareBlocked = errors.New("Cloudflare blocked the request (HTTP 403)")

// ErrRateLimited is matched by every error caused by an HTTP 429 that could
// not be waited out within the request's retry, deadline or refresh limits.
// Its text is shown on screen as is. It is netlimit.ErrRateLimited, so
// internal/roms reports the same error.
var ErrRateLimited = netlimit.ErrRateLimited

// ErrUploadGone is returned when the API answers HTTP 404 or 410 for a
// download. Its text is shown on screen as is.
var ErrUploadGone = errors.New("This file is no longer on itch.io.")

// ErrGameRemoved is returned when the game page responds with HTTP 404 or 410.
var ErrGameRemoved = errors.New("game removed (HTTP 404/410)")
