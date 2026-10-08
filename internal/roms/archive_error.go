package roms

import "errors"

// ErrUnreadableArchive is matched by an archive that cannot be read as a ZIP
// or 7z file: it is damaged, or not the format its name says.
var ErrUnreadableArchive = errors.New("unreadable archive")

// UnreadableArchive marks err, from opening an archive's directory, as
// ErrUnreadableArchive. Its text stays err's own. A network or storage
// failure underneath still matches as such.
func UnreadableArchive(err error) error {
	if err == nil {
		return nil
	}
	return unreadableArchive{err: err}
}

type unreadableArchive struct{ err error }

func (e unreadableArchive) Error() string   { return e.err.Error() }
func (e unreadableArchive) Unwrap() []error { return []error{ErrUnreadableArchive, e.err} }
