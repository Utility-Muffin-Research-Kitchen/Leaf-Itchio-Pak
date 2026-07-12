package logger_test

import (
	"bytes"
	"log"
	"os"
	"strings"
	"testing"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/logger"
)

// captureOutput redirects stdlib log output to a buffer for the test duration.
// Flags are zeroed so assertions don't have to account for the timestamp.
func captureOutput(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(os.Stderr)
		log.SetFlags(log.LstdFlags)
	})
	return &buf
}

// resetLevel restores the logger to INFO after each test.
func resetLevel(t *testing.T) {
	t.Helper()
	t.Cleanup(func() { logger.SetLevel(logger.LevelInfo) })
}

func TestLevelFromString(t *testing.T) {
	cases := []struct {
		input string
		want  logger.Level
	}{
		{"debug", logger.LevelDebug},
		{"DEBUG", logger.LevelDebug},
		{"Debug", logger.LevelDebug},
		{"info", logger.LevelInfo},
		{"INFO", logger.LevelInfo},
		{"", logger.LevelInfo},
		{"verbose", logger.LevelInfo},
		{"warn", logger.LevelWarn},
		{"WARN", logger.LevelWarn},
		{"error", logger.LevelError},
		{"ERROR", logger.LevelError},
	}
	for _, c := range cases {
		got := logger.LevelFromString(c.input)
		if got != c.want {
			t.Errorf("LevelFromString(%q) = %v, want %v", c.input, got, c.want)
		}
	}
}

func TestInfoLevel_SuppressesDebug(t *testing.T) {
	resetLevel(t)
	logger.SetLevel(logger.LevelInfo)
	buf := captureOutput(t)

	logger.Debug("should not appear")
	logger.Info("should appear")

	out := buf.String()
	if strings.Contains(out, "should not appear") {
		t.Error("DEBUG message appeared at INFO level")
	}
	if !strings.Contains(out, "should appear") {
		t.Error("INFO message did not appear at INFO level")
	}
}

func TestDebugLevel_ShowsAll(t *testing.T) {
	resetLevel(t)
	logger.SetLevel(logger.LevelDebug)
	buf := captureOutput(t)

	logger.Debug("debug msg")
	logger.Info("info msg")
	logger.Warn("warn msg")
	logger.Error("error msg")

	out := buf.String()
	for _, want := range []string{"debug msg", "info msg", "warn msg", "error msg"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output, got:\n%s", want, out)
		}
	}
}

func TestLevelTags_AlignedWidth(t *testing.T) {
	resetLevel(t)
	logger.SetLevel(logger.LevelDebug)
	buf := captureOutput(t)

	logger.Debug("d")
	logger.Info("i")
	logger.Warn("w")
	logger.Error("e")

	out := buf.String()
	for _, tag := range []string{"[DEBUG] ", "[INFO]  ", "[WARN]  ", "[ERROR] "} {
		if !strings.Contains(out, tag) {
			t.Errorf("expected tag %q in output:\n%s", tag, out)
		}
	}
}

func TestRegisterSecret_RedactsInOutput(t *testing.T) {
	resetLevel(t)
	logger.SetLevel(logger.LevelInfo)
	buf := captureOutput(t)

	// Use a unique value unlikely to appear in other test output.
	secret := "test-secret-aBcDeF-12345"
	logger.RegisterSecret(secret, "[TEST-SECRET]")
	logger.Info("url=https://example.com/api/%s/game/99", secret)

	out := buf.String()
	if strings.Contains(out, secret) {
		t.Errorf("secret appeared in log output:\n%s", out)
	}
	if !strings.Contains(out, "[TEST-SECRET]") {
		t.Errorf("redaction label not found in output:\n%s", out)
	}
}

func TestRegisterSecret_EmptyValueIsNoop(t *testing.T) {
	resetLevel(t)
	logger.SetLevel(logger.LevelInfo)
	buf := captureOutput(t)

	logger.RegisterSecret("", "[NOOP]")
	logger.Info("plain message no secrets")

	out := buf.String()
	if !strings.Contains(out, "plain message no secrets") {
		t.Errorf("unexpected output:\n%s", out)
	}
}

func TestRegisterSecret_UpdatesExistingLabel(t *testing.T) {
	resetLevel(t)
	logger.SetLevel(logger.LevelInfo)
	buf := captureOutput(t)

	newKey := "test-new-key-xYzAbC-99887"
	logger.RegisterSecret("test-old-key-mNoPqR-11223", "[UPDATE-TEST]")
	logger.RegisterSecret(newKey, "[UPDATE-TEST]") // replaces old entry

	logger.Info("key=%s in message", newKey)

	out := buf.String()
	if strings.Contains(out, newKey) {
		t.Errorf("updated secret still visible in output:\n%s", out)
	}
	if !strings.Contains(out, "[UPDATE-TEST]") {
		t.Errorf("redaction label not found:\n%s", out)
	}
}

func TestRemoveSecretForgetsLabel(t *testing.T) {
	resetLevel(t)
	logger.SetLevel(logger.LevelInfo)
	buf := captureOutput(t)

	secret := "test-removed-key-qRsTuV-55443"
	logger.RegisterSecret(secret, "[REMOVE-TEST]")
	logger.RemoveSecret("[REMOVE-TEST]")
	logger.Info("removed=%s", secret)

	out := buf.String()
	if !strings.Contains(out, secret) || strings.Contains(out, "[REMOVE-TEST]") {
		t.Fatalf("removed label still active: %s", out)
	}
}

func TestSignedURLCredentialsAreRedacted(t *testing.T) {
	resetLevel(t)
	logger.SetLevel(logger.LevelDebug)
	buf := captureOutput(t)

	logger.Debug("cdn=https://cdn.example/game.zip?X-Amz-Signature=sig-secret&token=token-secret")
	logger.Debug("page=https://author.itch.io/game/download/path-secret")

	out := buf.String()
	for _, secret := range []string{"sig-secret", "token-secret", "path-secret"} {
		if strings.Contains(out, secret) {
			t.Errorf("signed URL credential %q appeared in output:\n%s", secret, out)
		}
	}
	if strings.Count(out, "[REDACTED]") != 3 {
		t.Errorf("redaction count = %d, want 3:\n%s", strings.Count(out, "[REDACTED]"), out)
	}
}

func TestCredentialAndPathRedactionAtInfoAndDebugLevels(t *testing.T) {
	levels := []struct {
		name  string
		level logger.Level
		write func(string, ...any)
	}{
		{name: "info", level: logger.LevelInfo, write: logger.Info},
		{name: "debug", level: logger.LevelDebug, write: logger.Debug},
	}
	for _, test := range levels {
		t.Run(test.name, func(t *testing.T) {
			resetLevel(t)
			logger.SetLevel(test.level)
			buf := captureOutput(t)

			const (
				apiKey        = "phase94-api-key-7d3f"
				authToken     = "phase94-bearer-a62c"
				cookie        = "phase94-session-b193"
				downloadKey   = "phase94-download-e825"
				purchaseToken = "phase94-purchase-f714"
				signature     = "phase94-signature-c361"
				root          = "/Users/private-user/cards/secondary"
			)
			logger.RegisterSecret(apiKey, "[PHASE94-API]")
			logger.RegisterPrivatePath(root, "[SD:secondary_sd]")
			t.Cleanup(func() {
				logger.RemoveSecret("[PHASE94-API]")
				logger.RemovePrivatePath("[SD:secondary_sd]")
			})

			test.write("api=%s Authorization: Bearer %s", apiKey, authToken)
			test.write("Cookie: session=%s; theme=dark", cookie)
			test.write("Set-Cookie: session=%s-response; HttpOnly", cookie)
			test.write(`body={"download_key":"%s","purchase_token":"%s"}`, downloadKey, purchaseToken)
			test.write("url=https://cdn.example/game.zip?X-Amz-Signature=%s&download_key_id=%s", signature, downloadKey)
			test.write("transaction source path=%s", root+"/Roms/GBC/Leafbound.gbc")

			out := buf.String()
			for _, forbidden := range []string{apiKey, authToken, cookie, downloadKey, purchaseToken, signature, root, "private-user"} {
				if strings.Contains(out, forbidden) {
					t.Errorf("%s log leaked %q:\n%s", test.name, forbidden, out)
				}
			}
			if !strings.Contains(out, "[SD:secondary_sd]/Roms/GBC/Leafbound.gbc") {
				t.Errorf("%s log lost source-relative diagnostic:\n%s", test.name, out)
			}
		})
	}
}

func TestPrivatePathLongestRootWinsWithoutPrefixCollision(t *testing.T) {
	resetLevel(t)
	logger.SetLevel(logger.LevelInfo)
	buf := captureOutput(t)
	logger.RegisterPrivatePath("/mnt/sdcard", "[SD:primary]")
	logger.RegisterPrivatePath("/mnt/sdcard/.userdata/mlp1/Itch-io", "[APP-DATA]")
	t.Cleanup(func() {
		logger.RemovePrivatePath("[SD:primary]")
		logger.RemovePrivatePath("[APP-DATA]")
	})

	logger.Info("config=%s sibling=%s", "/mnt/sdcard/.userdata/mlp1/Itch-io/config.json", "/mnt/sdcard2/game.gb")
	out := buf.String()
	if !strings.Contains(out, "[APP-DATA]/config.json") {
		t.Fatalf("specific app-data root was not preserved: %s", out)
	}
	if !strings.Contains(out, "/mnt/sdcard2/game.gb") {
		t.Fatalf("path prefix collision was incorrectly redacted: %s", out)
	}
}
