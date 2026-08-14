package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const testVersion = "v0.0.0-clitest"

// buildTestBinary compiles the CLI with an injected version so --version can be
// asserted against a value the test controls rather than against the same
// symbol the test is compiled with.
func buildTestBinary(t *testing.T) string {
	t.Helper()
	binPath := filepath.Join(t.TempDir(), "agytop_test")
	build := exec.Command("go", "build", "-ldflags", "-X main.AppVersion="+testVersion, "-o", binPath, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %v\n%s", err, out)
	}
	return binPath
}

// hermeticCmd pins both leak paths: CWD (--demo writes .agents/sidecars/ there)
// and HOME (~/.gemini discovery scopes and ~/.agytop registry state).
func hermeticCmd(t *testing.T, binPath string, args ...string) *exec.Cmd {
	t.Helper()
	sandbox := t.TempDir()
	cmd := exec.Command(binPath, args...)
	cmd.Dir = sandbox
	cmd.Env = append(os.Environ(), "HOME="+sandbox)
	return cmd
}

func TestCLIVersionReflectsLdflags(t *testing.T) {
	out, err := hermeticCmd(t, buildTestBinary(t), "--version").CombinedOutput()
	if err != nil {
		t.Fatalf("--version failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), testVersion) {
		t.Errorf("expected injected version %q, got: %s", testVersion, out)
	}
}
