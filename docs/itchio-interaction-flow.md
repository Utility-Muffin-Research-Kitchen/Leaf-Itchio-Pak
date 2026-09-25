# itch.io Interaction Flow

This is a maintained engineering document, not an end-user API promise. The
free-download endpoints are undocumented and may change without notice. Request
URLs, tokens, authorization/cookie values, account names, and runtime roots must
remain inside the redacted local diagnostic path described in the
[user guide](user-guide.md).

This document describes how the Pak interacts with itch.io's website and
undocumented web API. There is no official public API for anonymous free
downloads — everything here was derived by observing browser network traffic
and confirmed through trial and error.

All HTTP logic lives in `internal/itchio/`.

---

## Game list (browse)

**Source:** `feed.go`

itch.io publishes an RSS/XML feed for tag-filtered searches:

```
GET https://itch.io/games/made-with-gb-studio.xml?page=N&q=QUERY
```

Each `<item>` contains title, link, description, image URL, and price. The
`<title>` field may include `[Tag]` brackets (e.g. `[GBC]`) which are stripped
from the display title but parsed as tags. Price is a free-text string; `$0.00`
or an empty/zero value means the game is free.

The total result count is scraped from the HTML browse page
(`https://itch.io/games/made-with-gb-studio`) by matching a
`"N results"` pattern, since the XML feed does not include it.

---

## Game detail (metadata + screenshots)

**Source:** `game.go` — `FetchGameDetail`

```
GET https://{author}.itch.io/{game}
```

The HTML page is scraped with regexes for:

| Field | Source in HTML |
|---|---|
| Game ID | `<meta content="games/NNNN" name="itch:path">` |
| CSRF token | `<meta name="csrf_token" value="...">` or `<input name="csrf_token" value="...">` |
| Screenshots | `<img class="screenshot..." src="...">` |
| Description | `<div class="formatted_description">...</div>` (converted to plain text) |

The CSRF token extracted here is used in the free download flow (Step 2 below).

**Game ID extraction note:** itch.io places `content=` before `name=` in the
`itch:path` meta element. The code uses a two-step approach — match the whole
`<meta>` element containing `itch:path`, then extract the numeric ID from its
`content` attribute — to be resilient to attribute ordering.

---

## Free game download flow

**Source:** `download.go` — `FetchUploads` + `DownloadFree`

With an API key and a known game ID, a free or name-your-own-price game is
listed through `GET api.itch.io/games/{GAME_ID}/uploads` without a
`download_key_id`, and downloads through an install session with no purchase
ID (see the paid flow below). That skips this web flow and its
`download_url` POST. `CatDownloadFlow.fetchFree` falls back to the web flow
at most once, when the API fails or lists nothing. A rate limit or
cancellation is final and never tries the other endpoint. When the API
refused access and the web flow fails too, the access error (`ErrNoAccess`)
is reported. Signed-out users always use the web flow below.

There are six steps. The same `*Client` (and its cookie jar) is used
throughout, so cookies set in early steps are available in later ones.

### Step 1 — GET game page → CSRF token

```
GET https://{author}.itch.io/{game}
```

The page-level CSRF token is extracted from the HTML. It authenticates the
next request.

### Step 2 — POST download_url → signed download page URL

```
POST https://{author}.itch.io/{game}/download_url
Content-Type: application/x-www-form-urlencoded

csrf_token={GAME_PAGE_CSRF}&suggested_amount=0
```

Response (JSON):

```json
{ "url": "https://{author}.itch.io/{game}/download/{DOWNLOAD_KEY_JWT}" }
```

`suggested_amount=0` signals "pay nothing" for pay-what-you-want games.
If the game requires purchase and no key is present, `url` is empty — the
flow stops with an error.

### Step 3 — Extract download key from signed URL

The signed URL's last path segment is the download key. It is a
dot-separated signed token:

```
base64({"id": NNNN, "expires": UNIX_TIMESTAMP}).base64(HMAC_SIGNATURE)
```

Example (decoded):
```
{"id": 2661299, "expires": 1776640021}
```

**Key extraction note:** the key may contain `/` characters (base64 alphabet),
which itch.io percent-encodes as `%2F` in the URL path. To avoid splitting on
them, the code uses `url.EscapedPath()` (not `url.Path`) before splitting on
`/`, then `url.PathUnescape()` on the final segment.

### Step 4 — GET signed download page → upload list + CSRF token

```
GET https://{author}.itch.io/{game}/download/{DOWNLOAD_KEY_JWT}
```

The page lists all available downloads. It is parsed as HTML. For each
`<div class="upload">` block:

- **Filename** — from `<strong class="name" title="filename.gb">` (title
  attribute preferred; text content as fallback)
- **Upload ID** — from `data-upload_id="NNNNNN"` on the `<a class="download_btn">` element

Uploads are classified against the maintained GB, GBC, GBA, NES, Mega Drive,
Pico-8, PlayStation, ZIP, and 7z format set. Unknown files remain available to
the explicit format/archive inspection flow rather than being silently treated
as Game Boy Color content.

The page also has its own CSRF token (distinct from the game page token):

```html
<meta name="csrf_token" value="..."/>
```

This CSRF token is required in Step 5.

**Why the download button has `href="javascript:void(0)"`:** itch.io's
frontend resolves the real CDN URL via JavaScript at click time. There is no
pre-baked `href` to a CDN URL in the HTML.

### Step 5 — POST file resolver → CDN URL

For each upload, a resolver URL of the form
`{gameURL}/file/{UPLOAD_ID}?key={JWT}&csrf={PAGE_CSRF}` is stored on the
`Upload` struct (constructed after Step 4). When the user selects a file,
`DownloadFree` makes:

```
POST https://{author}.itch.io/{game}/file/{UPLOAD_ID}
Content-Type: application/x-www-form-urlencoded

csrf_token={SIGNED_PAGE_CSRF}&download_key_id={NUMERIC_ID}
```

Two non-obvious details:

1. **`download_key_id` is the numeric ID** from the JWT payload (e.g. `2661299`),
   **not** the full JWT string. Sending the raw JWT as `key=...` returns
   `{"errors":["invalid key"]}`.

2. **`csrf_token` must be from the signed download page** (Step 4), not the
   game page (Step 1). These tokens differ per request.

Response (JSON):

```json
{ "url": "https://cdn-files.itch.zone/.../{filename}" }
```

### Step 6 — GET CDN URL → stream file

```
GET {CDN_URL}
```

The file is streamed directly to the destination path on disk. The `Content-Length`
header is used to track progress.

---

## Paid game download (API key path)

**Source:** `download_auth.go`, `roms/install_session.go`

For paid games the user already owns, every request goes to itch.io API v2 on
`api.itch.io` with `Authorization: Bearer {API_KEY}`. The key is never placed
in a URL, and the header only reaches `api.itch.io`: download redirects are
read rather than followed, and the CDN request is separate. This path is taken
automatically when all three conditions are true:

- `game.IsFree == false`
- `cfg.APIKey != ""`
- `detail.GameID != ""`

The v1 endpoints (`itch.io/api/1/{API_KEY}/...`) are no longer used. There is
no automatic fallback to them: a v2 failure is reported, and rolling back
means reinstalling the previous package.

### Step 1 — Find the purchase keys for the game

```
GET https://api.itch.io/profile/owned-keys?page={N}&game_ids={GAME_ID}
```

`game_ids` (comma-separated, not `game_id`) asks itch.io to return only that
game's keys. The answer is filtered here as well, so it works whether or not
the server applies the filter.

Normal page response (JSON):

```json
{
  "page": 1,
  "per_page": 50,
  "owned_keys": [
    { "id": 153412711, "game_id": 4228927, "purchase_id": 35928998,
      "downloads": 1, "created_at": "2026-04-26T13:38:12.000000000Z",
      "game": { ... } }
  ]
}
```

**Empty-collection quirk:** when there are no more keys, `owned_keys` is an
**empty object** (`{}`) or absent, not an empty array. The code keeps the raw
value and only unmarshals it when it is an array, so earlier pages survive.

The `id` field is the buyer's **download key ID**, tied to one purchase and
distinct from the API key. A game can have several: one per individual
purchase and one per bundle that includes it.

**Bundle or individual purchase.** Telling them apart needs the number of
distinct games per `purchase_id`, which a filtered answer cannot show. The
startup key validation scans the whole library (no `game_ids`) and caches
those counts in memory. A filtered answer uses them; on a miss it scans the
library once more. The counts belong to the current key: replacing or removing
it clears them, and a scan that started under the old key discards its result.
Nothing account-derived is persisted except the owned-game URL cache, which is
deleted when the key changes.

### Step 2 — List uploads

```
GET https://api.itch.io/games/{GAME_ID}/uploads?download_key_id={KEY_ID}
```

`download_key_id` is omitted for a free or name-your-own-price game. Uploads
are classified through the same maintained format/archive rules as anonymous
downloads, and `size` is kept on each `Upload`. `uploads` may be an array, `{}`,
`null`, or absent; `errors` is reported generically. Unstable fields such as
`traits` are not decoded.

### Step 3 — Begin an install

```
POST https://api.itch.io/games/{GAME_ID}/download-sessions
download_key_id={KEY_ID}
```

Returns `{"uuid": "..."}`. One `InstallSession` covers one install: it
creates the server session lazily on the first resolution and every later
resolution of that install reuses it, so itch.io counts probes, archive
inspection, refreshed URLs, and all files as one download. The POST is never
replayed. If creation fails the install continues without grouping; if the
operation is cancelled, it stops. The UUID is never logged or saved.

Each purchase listing starts one install. Its uploads carry the session by
pointer (`roms.Upload.Install`), which is also the only test for an API
download, so a free API download without a purchase ID still goes through
the API. Every copy made while choosing a format, planning destinations,
sealing the transaction, or inspecting an archive keeps it: the format
probe, archive inspection, the archive's refreshed URL, and every file of a
multi-file download resolve within the same session. Choosing another
purchase lists again and starts a new install.

### Step 4 — Resolve CDN URL

```
GET https://api.itch.io/uploads/{UPLOAD_ID}/download?download_key_id={KEY_ID}&uuid={UUID}
```

The answer is a redirect to the signed CDN URL (some deployments answer
`{"url": ...}` instead). The redirect is not followed: the location must be
an absolute HTTPS URL without credentials, and the caller gets it first for
archive inspection or the magic-byte probe. Missing locations, rejected
access, and malformed bodies fail with sanitized errors.

The signed CDN URL expires quickly (60 seconds). It is resolved immediately
before streaming, not cached.

### Step 5 — Stream file

```
GET {CDN_URL}
```

The file is streamed directly to the destination path on disk, identical to
the free download flow, with no `Authorization` header. The `Content-Length`
header drives the progress bar.

---

## CSRF token format

itch.io CSRF tokens are WTFkit-style signed tokens:

```
base64([nonce, timestamp, session_id]).base64(HMAC_signature)
```

They are short-lived and tied to the current session. The game page and the
signed download page each issue their own token.

---

## Known limitations and fragility

- **HTML scraping is brittle.** itch.io can change its page structure without
  notice. The free download button detection relies on `<div class="upload">`,
  `<strong class="name">`, and `data-upload_id` — any of these changing would
  break file listing for free games.

- **No official free-download API.** Everything in the free download path uses
  itch.io's internal web API. It is undocumented and unsupported.

- **CSRF tokens expire.** If the user leaves the ROM picker open for a long
  time before selecting a file, the CSRF token from Step 4 may expire, causing
  the resolver to return `{"errors":["invalid key"]}`.

- **`suggested_amount=0`** only works for truly free or PWYW games. A game
  with a mandatory minimum price will return an empty URL in Step 2 of the free
  flow — this is expected; those games use the paid API path instead.

- **CDN URLs are short-lived.** The signed URL returned by Step 3 of the paid
  path expires in ~60 seconds. The URL is resolved immediately before streaming
  so this is not normally an issue, but a very slow or stalled connection could
  cause it to expire mid-transfer.

- **API keys are physical secrets.** In-app entry uses the Catastrophe keyboard.
  The saved value is never prefilled or shown in full after saving, but newly
  typed characters are visible. FAT32 cannot protect `config.json` from someone
  with physical access to the SD card; see the user guide before enabling owned
  downloads.

- **Retries are intentionally narrow.** Only idempotent metadata requests retry
  selected transient failures. User-started downloads are not automatically
  repeated, and cancellation remains authoritative.

- **HTTP 429 pauses one host, for every client.** `ratelimit.go` sits under the
  shared transport, so the metadata client and the streaming/range copies see
  the same per-host cooldown; `itch.io`, `api.itch.io`, and each CDN host cool
  down independently. `Retry-After` (seconds or HTTP-date) is honored up to 60
  seconds, otherwise the back-off doubles from 2 seconds to that cap. Only
  bodyless GET/HEAD requests are replayed, at most 3 times; POST handshakes
  wait out a cooldown but are never replayed. A request whose deadline ends
  inside a cooldown fails at once with `ErrRateLimited`. The feed loop does
  not retry 429s again, and a catalogue refresh waits out at most 2 minutes of
  cooldown in total before failing with `ErrRateLimited` and keeping the cache.

- **Identity.** Requests send `User-Agent: Leaf-Itchio-Pak/<version>
  (+https://github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak)` over
  Go's standard TLS stack. There is no browser fingerprint or `Sec-Fetch-*`
  header set.

- **A missing later feed page is the end of the feed.** itch.io can answer
  404/410 past the last page of a long feed; that ends the slug and keeps its
  games. A missing first page is still an error.

- **Logs are local and redacted.** There is no UMRK telemetry endpoint. Both
  Info and Debug logging redact credentials, signed URLs, account names, and
  registered runtime roots.
