package roms

import (
	"bytes"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// markdownTextWindow is how much of a ".md" archive member is checked for
// text before it is treated as Markdown rather than a Mega Drive ROM.
const markdownTextWindow = 4096

// MDIsROM decides whether a file named "*.md", an archive member or a
// top-level upload, is a Mega Drive ROM or Markdown, from its first bytes. "SEGA" at 0x100 or 0x101
// marks a ROM header; TMSS also accepts the leading space some images use.
// Otherwise the member is Markdown only when its first 4 KB read as text:
// no NUL bytes and valid UTF-8. Anything else is a ROM, such as an image
// without a header.
func MDIsROM(header []byte) bool {
	if len(header) >= 0x104 && string(header[0x100:0x104]) == "SEGA" {
		return true
	}
	if len(header) >= 0x105 && string(header[0x101:0x105]) == "SEGA" {
		return true
	}
	window := header
	if len(window) > markdownTextWindow {
		window = window[:markdownTextWindow]
	}
	return !looksLikeText(window)
}

func looksLikeText(data []byte) bool {
	if bytes.IndexByte(data, 0) >= 0 {
		return false
	}
	// The window can end inside a multi-byte character; ignore that tail.
	for back := 1; back <= utf8.UTFMax && back <= len(data); back++ {
		if start := len(data) - back; utf8.RuneStart(data[start]) {
			if !utf8.FullRune(data[start:]) {
				data = data[:start]
			}
			break
		}
	}
	return utf8.Valid(data)
}

// ClassifyArchiveMember classifies an archive member by its name and, when
// the name does not decide it, by its first DetectBufSize bytes read through
// open. It returns the kind and the name, whose extension the first bytes
// may correct. A ".md" member is a Mega Drive ROM or Markdown by
// MDIsROM. Image names are never promoted to ROMs. A member that
// cannot be read stays KindOther, and for a ".md" name the read error is
// returned as well.
func ClassifyArchiveMember(name string, open func() (io.ReadCloser, error)) (FileKind, string, error) {
	kind := ClassifyEntry(name)
	base := filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	if kind != KindOther || strings.HasPrefix(base, "._") || IsImageExt(strings.ToLower(filepath.Ext(name))) {
		return kind, name, nil
	}
	header, err := readMemberHeader(open)
	if strings.EqualFold(ROMExt(name), ".md") {
		if err != nil {
			return KindOther, name, err
		}
		if MDIsROM(header) {
			return KindROM, name, nil
		}
		return KindOther, name, nil
	}
	if err != nil {
		return KindOther, name, nil
	}
	detected := DetectPlayableROMExt(header)
	if detected == "" {
		return KindOther, name, nil
	}
	if strings.EqualFold(filepath.Ext(name), detected) {
		return KindROM, name, nil
	}
	return KindROM, strings.TrimSuffix(name, filepath.Ext(name)) + detected, nil
}

func readMemberHeader(open func() (io.ReadCloser, error)) ([]byte, error) {
	rc, err := open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	header := make([]byte, DetectBufSize)
	n, err := io.ReadFull(rc, header)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, err
	}
	return header[:n], nil
}
