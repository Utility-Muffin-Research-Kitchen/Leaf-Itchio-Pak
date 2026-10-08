package itchio

import (
	"context"
	"sync"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/logger"
)

// InstallSession groups the API requests of one install (format probes,
// archive inspection, refreshed signed URLs, and every file) so itch.io counts
// them as one download. The server session is created lazily on the first
// resolution and reused by every resolution the session is handed. It holds
// no credential, is never persisted, and its UUID is never logged.
//
// A new install or a different purchase selection needs a new InstallSession.
type InstallSession struct {
	gameID        string
	downloadKeyID string

	mu      sync.Mutex
	uuid    string
	settled bool // creation succeeded, failed, or is not possible
}

// NewInstallSession starts an install of gameID. downloadKeyID selects the
// purchase that grants access; pass "" for a free download through the API.
// An empty gameID never creates a server session, so resolutions go
// ungrouped.
func NewInstallSession(gameID, downloadKeyID string) *InstallSession {
	return &InstallSession{gameID: gameID, downloadKeyID: downloadKeyID}
}

// DownloadKeyID returns the purchase this install uses, "" for none.
func (s *InstallSession) DownloadKeyID() string { return s.downloadKeyID }

// resolveUUID returns the server session UUID, creating it on first use.
// Only one creation is attempted per install. When it fails the install
// continues without grouping, as the download still works; when ctx itself
// is done the error is returned so the operation stops.
func (s *InstallSession) resolveUUID(ctx context.Context, create func(context.Context, string, string) (string, error)) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.settled {
		return s.uuid, nil
	}
	if s.gameID == "" {
		s.settled = true
		return "", nil
	}
	uuid, err := create(ctx, s.gameID, s.downloadKeyID)
	if err != nil {
		if ctx.Err() != nil {
			return "", err
		}
		logger.Warn("auth: continuing without an install session: %v", err)
		s.settled = true
		return "", nil
	}
	s.uuid, s.settled = uuid, true
	return uuid, nil
}
