//go:build linux

package memory_test

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/elliotboney/shelldon_go/core/memory"
)

// The vault-isolation property test (AD-3/NFR6) is OS-enforced and therefore
// Linux + root only (the Pi). It re-execs a copy of this test binary dropped to a
// distinct unprivileged worker uid (the Story 5.1 SysProcAttr.Credential
// mechanism) and proves the kernel denies it the vault read. The child entry is
// driven by TestMain via the probe sentinel below; off Linux this whole file is
// excluded by the build tag, so TestMain (and the normal m.Run path) is unaffected
// on darwin.

const (
	vaultProbeEnv = "SHELLDON_VAULT_PROBE" // set in the child: absolute path to the vault secret

	// Child exit codes the parent interprets.
	probeDenied   = 0 // kernel denied the worker-uid read — isolation holds (PASS)
	probeLeaked   = 3 // read SUCCEEDED, bytes returned — isolation BROKEN (FAIL)
	probeOtherErr = 4 // a non-permission error — inconclusive

	workerUID = 65534 // nobody: a distinct, unprivileged uid the worker is dropped to
	workerGID = 65534
)

// TestMain doubles as the worker-uid child entry: when re-exec'd with the probe
// sentinel set, it attempts the vault read and exits with a probe code instead of
// running the suite. Without the sentinel it runs tests normally.
func TestMain(m *testing.M) {
	if secret := os.Getenv(vaultProbeEnv); secret != "" {
		os.Exit(runVaultProbe(secret))
	}
	os.Exit(m.Run())
}

// runVaultProbe runs in the re-exec'd child, already dropped to the worker uid. A
// permission error on both the file read and the dir listing is the expected
// (correct) outcome; any returned bytes are a leak.
func runVaultProbe(secret string) int {
	data, err := os.ReadFile(secret)
	switch {
	case err == nil:
		_ = data
		return probeLeaked
	case errors.Is(err, fs.ErrPermission):
		if _, derr := os.ReadDir(filepath.Dir(secret)); !errors.Is(derr, fs.ErrPermission) {
			os.Stderr.WriteString("vault file read denied but dir listing was not: " + errString(derr) + "\n")
			return probeOtherErr
		}
		return probeDenied
	default:
		os.Stderr.WriteString("unexpected vault read error: " + err.Error() + "\n")
		return probeOtherErr
	}
}

func errString(err error) string {
	if err == nil {
		return "<nil>"
	}
	return err.Error()
}

// TestVaultIsolation_WorkerUIDDenied is the AD-3/NFR6 property: a process running
// as the worker uid cannot read the vault. It runs for real only as root on Linux
// (the Pi); elsewhere it skips with a reason — the OS cannot enforce the uid drop,
// so the property is unprovable there (the structural 0700 guard runs everywhere).
func TestVaultIsolation_WorkerUIDDenied(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("vault-isolation is OS-enforced only as root on Linux (the Pi); skipping — run on the Pi to prove it")
	}

	// A single tmp dir under the world-traversable /tmp, opened to 0755 so the only
	// barrier between the worker uid and the secret is the vault's own 0700 mode —
	// not an incidental parent-dir permission that would mask the real assertion.
	root, err := os.MkdirTemp("", "vaultiso-")
	if err != nil {
		t.Fatalf("mkdir temp root: %v", err)
	}
	defer func() { _ = os.RemoveAll(root) }()
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatalf("chmod root traversable: %v", err)
	}

	c, err := memory.OpenCurated(root)
	if err != nil {
		t.Fatalf("OpenCurated: %v", err)
	}
	vault, err := c.EnsureVault()
	if err != nil {
		t.Fatalf("EnsureVault: %v", err)
	}
	secret := filepath.Join(vault, "secret.md")
	if err := os.WriteFile(secret, []byte("TOP-SECRET-VAULT-BYTES"), 0o600); err != nil {
		t.Fatalf("seed vault secret: %v", err)
	}

	// Re-exec a copy of this test binary (placed in the 0755 root, so the worker uid
	// can exec it — the go-build temp dir holding os.Args[0] is typically 0700 and
	// unreachable to nobody).
	probeBin := copyExecutable(t, filepath.Join(root, "probe.bin"))

	cmd := exec.Command(probeBin)
	// Minimal env — only the probe sentinel. The child is dropped to an unprivileged
	// uid, so never hand it the parent's full environment (which may carry model/tool
	// secrets). The probe only reads files; it needs nothing else.
	cmd.Env = []string{vaultProbeEnv + "=" + secret}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{Uid: workerUID, Gid: workerGID},
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	switch code := exitCode(runErr); code {
	case probeDenied:
		// PASS: the kernel denied the worker-uid read.
	case probeLeaked:
		t.Fatalf("ISOLATION BROKEN: worker uid %d read vault contents", workerUID)
	case probeOtherErr:
		t.Fatalf("inconclusive vault probe: %s", stderr.String())
	default:
		// The child could not even start as the worker uid (e.g. binary still not
		// traversable). The environment cannot exercise the property — skip rather
		// than claim a pass we did not earn.
		t.Skipf("could not run worker-uid probe (err=%v): %s", runErr, stderr.String())
	}
}

// copyExecutable copies the running test binary to dst with 0755 perms so a
// dropped-uid child can exec it, returning dst.
func copyExecutable(t *testing.T, dst string) string {
	t.Helper()
	in, err := os.Open(os.Args[0])
	if err != nil {
		t.Fatalf("open test binary: %v", err)
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		t.Fatalf("create probe binary: %v", err)
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		t.Fatalf("copy test binary: %v", err)
	}
	if err := out.Close(); err != nil {
		t.Fatalf("close probe binary: %v", err)
	}
	if err := os.Chmod(dst, 0o755); err != nil {
		t.Fatalf("chmod probe binary: %v", err)
	}
	return dst
}

// exitCode extracts a child's exit code; -1 means it failed to start or was
// signalled (i.e. never reported a probe code).
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}
