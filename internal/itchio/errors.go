package itchio

import "errors"

// ErrCloudflareBlocked is returned when itch.io responds with HTTP 403,
// indicating Cloudflare bot-protection rejected the request.
var ErrCloudflareBlocked = errors.New("Cloudflare blocked the request (HTTP 403)")

// ErrRateLimited is matched by every error caused by an HTTP 429 that could
// not be waited out within the request's retry, deadline or refresh limits.
// Its text is safe to show in the UI.
var ErrRateLimited = errors.New("itch.io asked the app to slow down; try again later")

// ErrNoAccess is returned when the API refuses a game's upload list: the game
// is not owned, or the key does not grant access. Its text is safe for the UI.
var ErrNoAccess = errors.New("Game not owned or API key does not grant access to this game's downloads")

// ErrGameRemoved is returned when the game page responds with HTTP 404 or 410.
var ErrGameRemoved = errors.New("game removed (HTTP 404/410)")
