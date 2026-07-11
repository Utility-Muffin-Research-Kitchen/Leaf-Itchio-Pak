package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"runtime/pprof"
	"strings"
	"syscall"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/logger"
)

// version is set at build time via -ldflags:
//
//	-X main.version=vX.Y.Z
var version = "dev"

// gitCommit is set at build time via -ldflags:
//
//	-X main.gitCommit=xxxxxxx
var gitCommit = "unknown"

func main() {
	headless := flag.Bool("headless", false, "skip SDL2 init (CI mode)")
	rotateLogOnly := flag.Bool("rotate-log-only", false, "rotate the app log and exit (launcher use)")
	catProof := flag.Bool("cat-proof", false, "run the Catastrophe bridge visual proof")
	catProofFrames := flag.Int("cat-proof-frames", 0, "exit Catastrophe proof after N frames")
	catProofScreenshot := flag.String("cat-proof-screenshot", "", "save the final Catastrophe proof frame as PNG")
	cpuProfile := flag.String("cpuprofile", "", "write CPU profile to `file`")
	memProfile := flag.String("memprofile", "", "write memory profile to `file` on exit")
	pprofAddr := flag.String("pprof", "", "start pprof HTTP server on `addr` (e.g. :6060)")
	flag.Parse()

	logPath := logFilePath()
	_ = os.MkdirAll(filepath.Dir(logPath), 0755)
	if *rotateLogOnly {
		rotateLog(logPath)
		return
	}
	if os.Getenv("ITCHIO_LOG_PREPARED") != "1" {
		rotateLog(logPath)
	}
	logFile, err := os.OpenFile(logPath,
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		log.SetOutput(logFile)
		// Redirect fd 2 (stderr) so Go runtime panics land in the log too.
		redirectStderr(logFile.Fd())
		defer logFile.Close()
	}

	defer func() {
		if r := recover(); r != nil {
			logger.Error("PANIC: %v\n%s", r, debug.Stack())
		}
	}()

	logger.Info("itchio-pak %s starting", version)
	logger.Info("git commit: %s", gitCommit)
	p := readPlatform()
	logger.Info("platform:   %s (%s)", p, platformDescription(p))
	logger.Info("leaf:       %s", readLeafVersion())
	profilingDesc := "off"
	if *cpuProfile != "" || *memProfile != "" || *pprofAddr != "" {
		var parts []string
		if *cpuProfile != "" {
			parts = append(parts, "cpu="+*cpuProfile)
		}
		if *memProfile != "" {
			parts = append(parts, "mem="+*memProfile)
		}
		if *pprofAddr != "" {
			parts = append(parts, "pprof="+*pprofAddr)
		}
		profilingDesc = strings.Join(parts, " ")
	}
	logger.Info("profiling:  %s", profilingDesc)

	if *pprofAddr != "" {
		go func() {
			logger.Info("pprof: listening on %s", *pprofAddr)
			if err := http.ListenAndServe(*pprofAddr, nil); err != nil {
				logger.Error("pprof: %v", err)
			}
		}()
	}

	if *cpuProfile != "" {
		f, err := os.Create(*cpuProfile)
		if err != nil {
			logger.Error("cpuprofile: %v", err)
			os.Exit(1)
		}
		if err := pprof.StartCPUProfile(f); err != nil {
			logger.Error("cpuprofile start: %v", err)
			f.Close()
			os.Exit(1)
		}
		logger.Info("cpuprofile: writing to %s", *cpuProfile)
		defer func() {
			pprof.StopCPUProfile()
			f.Close()
			logger.Info("cpuprofile: written to %s", *cpuProfile)
		}()
	}

	// When profiling to files, install a signal handler so that SIGTERM/SIGINT/
	// SIGHUP (e.g. Ctrl-C in the terminal, adb disconnect, or NextUI killing the
	// process) still flushes profiles before exit. Without this, Go's deferred
	// cleanup is skipped and the profile files are never written.
	if *cpuProfile != "" || *memProfile != "" {
		cpuProf := *cpuProfile
		memProf := *memProfile
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
		go func() {
			sig := <-sigCh
			logger.Info("profiling: caught signal %v — flushing profiles", sig)
			pprof.StopCPUProfile() // no-op if not started; flushes the file
			if cpuProf != "" {
				logger.Info("cpuprofile: written to %s", cpuProf)
			}
			if memProf != "" {
				if f, err := os.Create(memProf); err == nil {
					pprof.WriteHeapProfile(f)
					f.Close()
					logger.Info("memprofile: written to %s", memProf)
				} else {
					logger.Error("memprofile: %v", err)
				}
			}
			os.Exit(0)
		}()
	}

	if *headless {
		logger.Info("headless mode: exiting cleanly")
		os.Exit(0)
	}
	if *catProof {
		_ = os.Setenv("ITCHIO_CAT_PROOF", "1")
		_ = os.Setenv("ITCHIO_CAT_PROOF_FRAMES", fmt.Sprintf("%d", *catProofFrames))
		_ = os.Setenv("ITCHIO_CAT_PROOF_SCREENSHOT", *catProofScreenshot)
	}

	runSDL()

	if *memProfile != "" {
		f, err := os.Create(*memProfile)
		if err != nil {
			logger.Error("memprofile: %v", err)
		} else {
			defer f.Close()
			if err := pprof.WriteHeapProfile(f); err != nil {
				logger.Error("memprofile write: %v", err)
			} else {
				logger.Info("memprofile: written to %s", *memProfile)
			}
		}
	}
}

// logFilePath uses Leaf's public log root. Development/CI falls back to HOME.
func logFilePath() string {
	if logs := os.Getenv("LOGS_PATH"); logs != "" {
		return filepath.Join(logs, "itchio-pak.log")
	}
	if userdata := os.Getenv("USERDATA_PATH"); userdata != "" {
		return filepath.Join(userdata, "logs", "itchio-pak.log")
	}
	return filepath.Join(os.Getenv("HOME"), "itchio-pak.log")
}

// readPlatform returns the PLATFORM env var, or "unknown" if unset.
func readPlatform() string {
	if p := os.Getenv("PLATFORM"); p != "" {
		return p
	}
	return "unknown"
}

// platformDescription returns the supported Leaf device name.
func platformDescription(platform string) string {
	switch platform {
	case "mlp1":
		return "Miniloong Pocket 1"
	default:
		return "unknown device"
	}
}

// readLeafVersion returns a launcher-provided release identifier when present.
func readLeafVersion() string {
	for _, name := range []string{"LEAF_VERSION", "UMRK_RELEASE_ID"} {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value
		}
	}
	return "unknown"
}
