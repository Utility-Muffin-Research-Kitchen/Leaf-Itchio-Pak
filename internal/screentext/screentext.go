// Package screentext turns errors into the sentences the screens show.
//
// A screen never shows an error's own text: it names internal steps
// ("fetch game page: ..."), and a request error can carry a signed URL.
// FromError gives every error class one plain sentence and anything else a
// generic one, so the screens show only words written for you. The log
// keeps the full error.
package screentext

import (
	"archive/zip"
	"context"
	"errors"
	"io/fs"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"syscall"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/leaf"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/netlimit"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/roms"
)

// Kind is the class of an error, which decides its sentence. Screens that
// phrase a class their own way, such as the game page, switch on it.
type Kind uint8

const (
	Unknown Kind = iota
	// Message is an error made with New or Wrap: its text is the sentence.
	Message
	Cancelled
	Stalled
	RateLimited
	UploadGone
	GameRemoved
	NotOwned
	NoAccess
	DownloadRefused
	SignInNeeded
	Blocked
	SuspendUnavailable
	Network
	StorageFull
	Storage
	UnreadableArchive
)

var sentences = map[Kind]string{
	Unknown:   "Something went wrong. Try again.",
	Cancelled: "Cancelled.",
	// These three were written for the screen as error texts; they stay
	// exactly as the download screens have shown them.
	Stalled:            itchio.ErrDownloadStalled.Error(),
	RateLimited:        netlimit.ErrRateLimited.Error(),
	UploadGone:         itchio.ErrUploadGone.Error(),
	GameRemoved:        "This game was removed from itch.io.",
	NotOwned:           "Your itch.io account doesn't own this game.",
	NoAccess:           "Your itch.io account doesn't have access to this game's downloads.",
	DownloadRefused:    "Your itch.io account doesn't have access to this download.",
	SignInNeeded:       "This download needs an itch.io account. Sign in from Settings, then try again.",
	Blocked:            "itch.io blocked the request. Try again later.",
	SuspendUnavailable: "Jawaka is unavailable, so Leaf can't prevent suspend. Nothing was changed. Try again.",
	Network:            "Can't reach itch.io. Check the connection and try again.",
	StorageFull:        "The SD card is full. Free up some space, then try again.",
	Storage:            "Couldn't read or write the SD card. Check the card, then try again.",
	UnreadableArchive:  "Leaf can't read this archive. It may be damaged or in an unsupported format.",
}

// FromError returns the sentence a screen shows for err, or "" for nil.
func FromError(err error) string {
	if err == nil {
		return ""
	}
	var message *messageError
	if errors.As(err, &message) {
		return message.text
	}
	return sentences[Classify(err)]
}

// Classify returns the class of err. The first match wins, so a class that
// says more (a stalled download) comes before one it also matches (a
// network timeout).
func Classify(err error) Kind {
	var message *messageError
	switch {
	case err == nil:
		return Unknown
	case errors.As(err, &message):
		return Message
	case errors.Is(err, context.Canceled):
		return Cancelled
	case errors.Is(err, itchio.ErrDownloadStalled):
		return Stalled
	case errors.Is(err, netlimit.ErrRateLimited):
		return RateLimited
	// An upload list that is gone matches both; it reads as the file.
	case errors.Is(err, itchio.ErrUploadGone):
		return UploadGone
	case errors.Is(err, itchio.ErrGameRemoved):
		return GameRemoved
	case errors.Is(err, itchio.ErrNotOwned):
		return NotOwned
	case errors.Is(err, itchio.ErrNoAccess):
		return NoAccess
	case errors.Is(err, itchio.ErrDownloadRefused):
		return DownloadRefused
	case errors.Is(err, itchio.ErrNoWebDownload):
		return SignInNeeded
	case errors.Is(err, itchio.ErrCloudflareBlocked):
		return Blocked
	case errors.Is(err, leaf.ErrDaemonUnavailable):
		return SuspendUnavailable
	case isNetwork(err):
		return Network
	case errors.Is(err, syscall.ENOSPC) || errors.Is(err, leaf.ErrNoSpace):
		return StorageFull
	case isStorage(err):
		return Storage
	case errors.Is(err, roms.ErrUnreadableArchive) || errors.Is(err, zip.ErrFormat) ||
		errors.Is(err, zip.ErrAlgorithm) || errors.Is(err, zip.ErrChecksum):
		return UnreadableArchive
	}
	return Unknown
}

// isNetwork reports a request that got no answer. The itch.io client and the
// archive readers mark theirs with netlimit.ErrNetwork; the other checks
// catch transport errors that reach the screen unwrapped, such as a body
// read on a dropped connection. It does not match net.Error as such:
// syscall.Errno implements it, so a full or read-only card would match.
func isNetwork(err error) bool {
	if errors.Is(err, netlimit.ErrNetwork) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var urlErr *url.Error
	var opErr *net.OpError
	var dnsErr *net.DNSError
	if errors.As(err, &urlErr) || errors.As(err, &opErr) || errors.As(err, &dnsErr) {
		return true
	}
	// The HTTP/2 transport's lost-connection error is not exported.
	return strings.Contains(err.Error(), "http2: client connection lost")
}

func isStorage(err error) bool {
	var pathErr *fs.PathError
	var linkErr *os.LinkError
	var syscallErr *os.SyscallError
	return errors.As(err, &pathErr) || errors.As(err, &linkErr) || errors.As(err, &syscallErr)
}

// messageError is an error whose text was written for the screen. Error()
// is what the log gets: the text, or the cause when there is one.
type messageError struct {
	text string
	err  error
}

func (e *messageError) Error() string {
	if e.err == nil {
		return e.text
	}
	return e.err.Error()
}

func (e *messageError) Unwrap() error { return e.err }

// New returns an error that FromError shows as text. Use it for a failure
// you can act on, such as a name that is taken; a full sentence, ending in
// a period.
func New(text string) error { return &messageError{text: text} }

// Wrap returns an error that FromError shows as text, while errors.Is and
// the log still see err. A nil err gives New(text).
func Wrap(err error, text string) error { return &messageError{text: text, err: err} }

// String names a class in logs and test failures.
func (kind Kind) String() string {
	names := [...]string{"Unknown", "Message", "Cancelled", "Stalled", "RateLimited", "UploadGone",
		"GameRemoved", "NotOwned", "NoAccess", "DownloadRefused", "SignInNeeded", "Blocked",
		"SuspendUnavailable", "Network", "StorageFull", "Storage", "UnreadableArchive"}
	if int(kind) < len(names) {
		return names[kind]
	}
	return "Kind(" + strconv.Itoa(int(kind)) + ")"
}
