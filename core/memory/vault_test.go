package memory_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/elliotboney/shelldon_go/core/memory"
)

// TestEnsureVault_Perms proves the vault is created as an owner-only (0700)
// directory and that creation is idempotent. The 0700 mode is the OS-enforced
// worker-uid exclusion (NFR6/AD-3); the Pi property test proves the kernel
// actually denies a worker-uid process (see vault_isolation_linux_test.go).
func TestEnsureVault_Perms(t *testing.T) {
	root := t.TempDir()
	c, err := memory.OpenCurated(root)
	if err != nil {
		t.Fatalf("OpenCurated: %v", err)
	}

	vault, err := c.EnsureVault()
	if err != nil {
		t.Fatalf("EnsureVault: %v", err)
	}
	if want := filepath.Join(root, "vault"); vault != want {
		t.Fatalf("vault path = %q, want %q", vault, want)
	}

	fi, err := os.Stat(vault)
	if err != nil {
		t.Fatalf("stat vault: %v", err)
	}
	if !fi.IsDir() {
		t.Fatalf("vault is not a directory")
	}
	if perm := fi.Mode().Perm(); perm != 0o700 {
		t.Fatalf("vault perm = %#o, want 0700", perm)
	}

	// Idempotent: a second call succeeds and the mode is still owner-only.
	if _, err := c.EnsureVault(); err != nil {
		t.Fatalf("EnsureVault (2nd call): %v", err)
	}
	fi2, err := os.Stat(vault)
	if err != nil {
		t.Fatalf("stat vault after 2nd call: %v", err)
	}
	if perm := fi2.Mode().Perm(); perm != 0o700 {
		t.Fatalf("vault perm after 2nd call = %#o, want 0700", perm)
	}
}

// TestEnsureVault_DisjointWriterUnchanged proves that creating the vault opens no
// bot write path: WriteFile and AppendFact to vault/ still return ErrOwnerOnly and
// write nothing. The disjoint-writer invariant (only core, never the LLM, touches
// the vault) must survive the vault now existing.
func TestEnsureVault_DisjointWriterUnchanged(t *testing.T) {
	root := t.TempDir()
	c, err := memory.OpenCurated(root)
	if err != nil {
		t.Fatalf("OpenCurated: %v", err)
	}
	if _, err := c.EnsureVault(); err != nil {
		t.Fatalf("EnsureVault: %v", err)
	}

	if err := c.WriteFile("vault/secret.md", []byte("nope")); !errors.Is(err, memory.ErrOwnerOnly) {
		t.Fatalf("WriteFile(vault/secret.md) = %v, want ErrOwnerOnly", err)
	}
	if err := c.AppendFact("vault/secret.md", "nope"); !errors.Is(err, memory.ErrOwnerOnly) {
		t.Fatalf("AppendFact(vault/secret.md) = %v, want ErrOwnerOnly", err)
	}
	// The bot writes were rejected, so the vault stays empty.
	entries, err := os.ReadDir(filepath.Join(root, "vault"))
	if err != nil {
		t.Fatalf("read vault dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("vault has %d entries after rejected bot writes, want 0", len(entries))
	}
}
