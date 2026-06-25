package memory

import (
	"fmt"
	"os"
	"path/filepath"
)

// VaultDir is the curated tree's sensitive-classification lane (AD-3). It is
// created core-direct — never through the bot WriteFile/AppendFact path, which
// rejects it (ErrOwnerOnly) — with permissions that exclude every non-owner uid.
// Once the worker runs under a separate uid (Privsep-lite, Epic 5) the kernel,
// not a path filter, denies it the read (NFR6/AD-3).
const VaultDir = "vault"

// vaultPerm is the vault directory mode: owner-only (rwx------). With the vault
// owned by the core (parent) uid and the worker dropped to a different uid, 0700
// excludes the worker by plain POSIX rules — OS-enforced isolation, not a filter.
const vaultPerm = 0o700

// EnsureVault creates the curated tree's vault/ directory, owned by the calling
// (core) uid with owner-only 0700 permissions, and returns its absolute path. It
// is core-direct and idempotent: a second call on an existing vault re-asserts the
// mode and succeeds. Creating the directory opens no bot write path to it — the
// WriteFile/AppendFact guards still reject vault/ (ErrOwnerOnly).
//
// Callers gate creation on the worker being uid-separated (AD-3): main calls this
// only under SHELLDON_WORKER=privsep with a configured worker uid. The matching
// OS-enforced read-denial is proven by the Linux property test on the Pi.
func (c *Curated) EnsureVault() (string, error) {
	vault := filepath.Join(c.root, VaultDir)
	if err := os.MkdirAll(vault, vaultPerm); err != nil {
		return "", fmt.Errorf("memory: create vault: %w", err)
	}
	// MkdirAll honors the ambient umask, which can only narrow 0700 (never widen
	// it). Chmod pins the mode to exactly 0700 so the worker-uid exclusion — the
	// whole security property — never silently depends on the inherited umask.
	if err := os.Chmod(vault, vaultPerm); err != nil {
		return "", fmt.Errorf("memory: chmod vault: %w", err)
	}
	return vault, nil
}
