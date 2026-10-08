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

// ErrNoAccess is returned when the API refuses a game's upload list: the game
// is not owned, or the key does not grant access. Its text is safe for the UI.
var ErrNoAccess = errors.New("Game not owned or API key does not grant access to this game's downloads")

// ErrDownloadRefused is returned when itch.io refuses to resolve an upload
// with HTTP 401 or 403. Its text is safe for the UI.
var ErrDownloadRefused = errors.New("Game not owned or API key does not grant access to this download")

// ErrNoWebDownload is returned by the anonymous web flow when itch.io offers
// no download link: the game is paid or needs a signed-in account.
var ErrNoWebDownload = errors.New("download_url returned empty url (game may be paid or require login)")

// ErrGameRemoved is returned when the game page responds with HTTP 404 or 410.
var ErrGameRemoved = errors.New("game removed (HTTP 404/410)")

// uploadListGone is an HTTP 404 or 410 on a game's upload list: the game was
// removed or is hidden. It reads as ErrUploadGone on screen and matches both
// ErrUploadGone and ErrGameRemoved.
type uploadListGone struct{}

func (uploadListGone) Error() string { return ErrUploadGone.Error() }

func (uploadListGone) Is(target error) bool {
	return target == ErrUploadGone || target == ErrGameRemoved
}
