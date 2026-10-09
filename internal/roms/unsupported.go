package roms

import "strings"

// unsupportedSystemExts names the system a file extension belongs to when
// the app cannot install that system. A file with one of these is a game, or
// a program, for something else: it must not be offered as a ROM to classify.
//
// The list is short on purpose. Each extension belongs to one system, so
// ".iso", which fits several consoles and is accepted as a PlayStation image,
// and ".bin" are left out. Systems the app installs (see IsSupportedUploadExt)
// are left out too. The computer and phone entries are the ones itch.io
// uploads show most; Linux builds have no one extension.
var unsupportedSystemExts = map[string]string{
	".nds":    "Nintendo DS",
	".3ds":    "Nintendo 3DS",
	".n64":    "Nintendo 64",
	".z64":    "Nintendo 64",
	".v64":    "Nintendo 64",
	".sfc":    "Super Nintendo",
	".smc":    "Super Nintendo",
	".pocket": "Analogue Pocket",
	".exe":    "Windows",
	".dmg":    "macOS",
	".apk":    "Android",
}

// UnsupportedSystem names the system filename's extension shows it is for,
// when the app cannot install that system, or returns "".
func UnsupportedSystem(filename string) string {
	return unsupportedSystemExts[strings.ToLower(ROMExt(filename))]
}

// UnsupportedSystemInName is UnsupportedSystem for a name with a version or a
// note after the extension, such as "Hidden_palace.nds v0.1 (Post-jam bug
// fix)", where the extension is inside the name. It looks for a dot and a
// whole word that is one of the extensions. A word that starts with a digit
// does not count right after a digit, which is a version number: "1.3ds".
func UnsupportedSystemInName(filename string) string {
	lower := strings.ToLower(filename)
	for i := 0; i < len(lower); i++ {
		if lower[i] != '.' {
			continue
		}
		end := i + 1
		for end < len(lower) && isASCIIAlphanumeric(lower[end]) {
			end++
		}
		system, ok := unsupportedSystemExts[lower[i:end]]
		if !ok {
			continue
		}
		if end > i+1 && isASCIIDigit(lower[i+1]) && i > 0 && isASCIIDigit(lower[i-1]) {
			continue
		}
		return system
	}
	return ""
}

func isASCIIDigit(c byte) bool { return c >= '0' && c <= '9' }

func isASCIIAlphanumeric(c byte) bool {
	return isASCIIDigit(c) || c >= 'a' && c <= 'z'
}
