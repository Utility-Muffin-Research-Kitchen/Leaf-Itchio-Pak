package leaf_test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/leaf"
)

func TestAvailableBytesUsesExistingAncestor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "future", "download")
	available, err := leaf.AvailableBytes(path)
	if err != nil {
		t.Fatal(err)
	}
	if available == 0 {
		t.Fatal("available storage was reported as zero")
	}
}

func TestRequireFreeSpaceRejectsImpossibleTransfer(t *testing.T) {
	if err := leaf.RequireFreeSpace(t.TempDir(), int64(^uint64(0)>>1)); err == nil {
		t.Fatal("impossible transfer passed free-space preflight")
	}
}

// The screens tell a full card from other storage failures by ErrNoSpace.
func TestRequireFreeSpaceReportsErrNoSpace(t *testing.T) {
	err := leaf.RequireFreeSpace(t.TempDir(), int64(^uint64(0)>>2))
	if !errors.Is(err, leaf.ErrNoSpace) {
		t.Fatalf("error = %v, want ErrNoSpace", err)
	}
}
