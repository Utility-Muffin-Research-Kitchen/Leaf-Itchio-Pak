package logger

import (
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
)

// Level represents a log severity level.
type Level int32

const (
	LevelDebug Level = iota // 0 — most verbose
	LevelInfo               // 1 — default
	LevelWarn               // 2
	LevelError              // 3
)

var currentLevel atomic.Int32

func init() {
	currentLevel.Store(int32(LevelInfo))
}

// SetLevel sets the minimum level written to the log. Safe to call from any goroutine.
func SetLevel(l Level) {
	currentLevel.Store(int32(l))
}

// LevelFromString maps a string name to a Level.
// Recognised values (case-insensitive): "debug", "info", "warn", "error".
// Empty or unknown strings resolve to LevelInfo.
func LevelFromString(s string) Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return LevelDebug
	case "warn":
		return LevelWarn
	case "error":
		return LevelError
	default:
		return LevelInfo
	}
}

type secret struct {
	plain string
	label string
}

var (
	secretsMu sync.RWMutex
	secrets   []secret

	// Query credentials occur in itch.io resolver URLs and CDN-signed URLs.
	// Preserve parameter names for diagnosis, but never their values.
	sensitiveQueryValue = regexp.MustCompile(`(?i)([?&](?:key|csrf|token|signature|x-amz-signature|x-amz-credential|x-amz-security-token|download_key_id)=)[^&\s"'<>]+`)
	// Free-download page URLs carry the download key as a path segment rather
	// than a query parameter.
	signedDownloadPath = regexp.MustCompile(`(?i)(https?://[^\s"'<>]+/download/)[^/?\s"'<>]+`)
)

// RegisterSecret registers a plaintext value to be fully replaced with label in
// all future log output. Calling with an empty value is a no-op.
// If a secret with the same label already exists, its plaintext is updated.
// Safe to call from any goroutine.
func RegisterSecret(value, label string) {
	if value == "" {
		return
	}
	secretsMu.Lock()
	defer secretsMu.Unlock()
	for i, s := range secrets {
		if s.label == label {
			secrets[i].plain = value
			return
		}
	}
	secrets = append(secrets, secret{plain: value, label: label})
}

// RemoveSecret forgets the plaintext registered for label. Use this when a
// persisted credential is removed from application state.
func RemoveSecret(label string) {
	secretsMu.Lock()
	defer secretsMu.Unlock()
	for index, item := range secrets {
		if item.label == label {
			secrets = append(secrets[:index], secrets[index+1:]...)
			return
		}
	}
}

func redact(s string) string {
	secretsMu.RLock()
	defer secretsMu.RUnlock()
	for _, sec := range secrets {
		s = strings.ReplaceAll(s, sec.plain, sec.label)
	}
	s = sensitiveQueryValue.ReplaceAllString(s, `${1}[REDACTED]`)
	s = signedDownloadPath.ReplaceAllString(s, `${1}[REDACTED]`)
	return s
}

func write(l Level, format string, args ...any) {
	if Level(currentLevel.Load()) > l {
		return
	}
	var tag string
	switch l {
	case LevelDebug:
		tag = "[DEBUG] "
	case LevelInfo:
		tag = "[INFO]  "
	case LevelWarn:
		tag = "[WARN]  "
	case LevelError:
		tag = "[ERROR] "
	default:
		tag = "[UNKNOWN] "
	}
	log.Print(tag + redact(fmt.Sprintf(format, args...)))
}

// Debug logs at DEBUG level. Suppressed when level is INFO (the default).
func Debug(format string, args ...any) { write(LevelDebug, format, args...) }

// Info logs at INFO level.
func Info(format string, args ...any) { write(LevelInfo, format, args...) }

// Warn logs at WARN level.
func Warn(format string, args ...any) { write(LevelWarn, format, args...) }

// Error logs at ERROR level.
func Error(format string, args ...any) { write(LevelError, format, args...) }
