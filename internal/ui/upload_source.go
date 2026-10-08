//go:build !headless

package ui

import (
	"context"
	"errors"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/logger"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/roms"
)

// resolveUploadURL returns the signed CDN URL for upload: through the API
// for an API upload, else through the web resolver. A free API upload that
// itch.io refuses to resolve is retried once through the web flow.
func resolveUploadURL(ctx context.Context, client *itchio.Client, apiKey, gameURL string, upload roms.Upload) (string, error) {
	if !upload.ViaAPI() {
		return client.ResolveFreeURLContext(ctx, itchio.Upload{Filename: upload.Filename, URL: upload.URL})
	}
	cdnURL, err := client.ResolveUploadURLContext(ctx, apiKey, upload.UploadID, upload.Install)
	if !freeResolveRefused(upload, err) {
		return cdnURL, err
	}
	web, err := webFallbackUpload(ctx, client, gameURL, upload, err)
	if err != nil {
		return "", err
	}
	return client.ResolveFreeURLContext(ctx, web)
}

// downloadUpload streams upload to dest with the same sources and fallback
// as resolveUploadURL.
func downloadUpload(ctx context.Context, client *itchio.Client, apiKey, gameURL string, upload roms.Upload,
	dest string, progress func(int64, int64)) error {
	if !upload.ViaAPI() {
		return client.DownloadFreeContext(ctx, itchio.Upload{Filename: upload.Filename, URL: upload.URL}, dest, progress)
	}
	err := client.DownloadUploadContext(ctx, apiKey, upload.UploadID, upload.Install, dest, progress)
	if !freeResolveRefused(upload, err) {
		return err
	}
	web, err := webFallbackUpload(ctx, client, gameURL, upload, err)
	if err != nil {
		return err
	}
	return client.DownloadFreeContext(ctx, web, dest, progress)
}

// freeResolveRefused reports whether itch.io refused to resolve an upload
// listed through the API for a free install (no purchase ID). Listing a free
// game without a download key works today; this keeps downloads working if
// itch.io ever starts refusing the resolve. A purchase is never retried
// anonymously.
func freeResolveRefused(upload roms.Upload, err error) bool {
	return upload.ViaAPI() && upload.Install.DownloadKeyID() == "" && errors.Is(err, itchio.ErrDownloadRefused)
}

// webFallbackUpload finds upload in the anonymous web listing of gameURL, by
// upload ID first and then by filename. It runs the web flow once. When the
// web flow cannot offer the file, the API's refusal is the error reported,
// unless the operation was cancelled or itch.io is limiting requests.
func webFallbackUpload(ctx context.Context, client *itchio.Client, gameURL string, upload roms.Upload, refused error) (itchio.Upload, error) {
	logger.Warn("download: itch.io refused free upload id=%s through the API, trying the web download page", upload.UploadID)
	uploads, err := client.FetchWebUploadsContext(ctx, gameURL)
	if err != nil {
		if ctx.Err() != nil || errors.Is(err, itchio.ErrRateLimited) {
			return itchio.Upload{}, err
		}
		logger.Warn("download: web download page failed too: %v", err)
		return itchio.Upload{}, refused
	}
	for _, candidate := range uploads {
		if upload.UploadID != "" && candidate.UploadID == upload.UploadID {
			return candidate, nil
		}
	}
	for _, candidate := range uploads {
		if candidate.Filename == upload.Filename {
			return candidate, nil
		}
	}
	logger.Warn("download: the web download page does not list upload id=%s", upload.UploadID)
	return itchio.Upload{}, refused
}
