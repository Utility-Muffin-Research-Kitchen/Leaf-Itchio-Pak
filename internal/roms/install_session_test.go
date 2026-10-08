package roms

import (
	"context"
	"fmt"
	"testing"
)

// A refused session is replaced once per install. A resolution that saw the
// old UUID after another one replaced it retries with the replacement.
func TestRenewUUIDReplacesARefusedSessionOnce(t *testing.T) {
	creates := 0
	create := func(context.Context, string, string) (string, error) {
		creates++
		return fmt.Sprint("uuid-", creates), nil
	}
	ctx := context.Background()
	session := NewInstallSession("42", "777")
	if uuid, _ := session.ResolveUUID(ctx, create); uuid != "uuid-1" {
		t.Fatalf("first uuid = %q", uuid)
	}
	steps := []struct {
		rejected, want string
		retry          bool
	}{
		{"uuid-1", "uuid-2", true}, // replaced
		{"uuid-1", "uuid-2", true}, // a late caller gets the replacement
		{"uuid-2", "", false},      // no second replacement
	}
	for _, step := range steps {
		uuid, retry, err := session.RenewUUID(ctx, step.rejected, create)
		if err != nil || uuid != step.want || retry != step.retry {
			t.Fatalf("RenewUUID(%q) = %q, %v, %v; want %q, %v", step.rejected, uuid, retry, err, step.want, step.retry)
		}
	}
	if creates != 2 {
		t.Fatalf("creates = %d, want 2", creates)
	}
}
