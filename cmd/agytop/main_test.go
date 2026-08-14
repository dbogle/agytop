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

// TestCLIListDemoSidecars asserts --demo --list enumerates all four bundled
// demo sidecars. hermeticCmd pins cmd.Dir and HOME so setupDemoConfigs
// writes into a sandbox rather than the repo, and discovery doesn't pick up
// the developer's real ~/.gemini sidecars.
func TestCLIListDemoSidecars(t *testing.T) {
	out, err := hermeticCmd(t, buildTestBinary(t), "--demo", "--list").CombinedOutput()
	if err != nil {
		t.Fatalf("--demo --list failed: %v\n%s", err, out)
	}
	for _, want := range []string{"data-indexer", "cron-nightly-report", "telemetry-bridge", "flaky-service"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("expected %q in --list output, got: %s", want, out)
		}
	}
}

// TestCLIDryRunExitCodes is the exit-code contract for the headless
// diagnostic workflow: 0 on a passing dry run (main.go's os.Exit(0) after a
// successful DryRun), 1 for an unknown sidecar id (the "not found" error
// path) or a failing dry run.
func TestCLIDryRunExitCodes(t *testing.T) {
	binPath := buildTestBinary(t)

	tests := []struct {
		name     string
		id       string
		wantExit int
	}{
		{"healthy demo sidecar passes", "data-indexer", 0},
		{"unknown id fails", "no-such-sidecar", 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := hermeticCmd(t, binPath, "--demo", "--dry-run", tt.id)
			out, err := cmd.CombinedOutput()

			got := 0
			if exitErr, ok := err.(*exec.ExitError); ok {
				got = exitErr.ExitCode()
			} else if err != nil {
				t.Fatalf("unexpected error: %v\n%s", err, out)
			}
			if got != tt.wantExit {
				t.Errorf("exit code = %d, want %d\noutput: %s", got, tt.wantExit, out)
			}
		})
	}
}

// TestCLIDryRunOutputContainsDiagnosticMarkers asserts the dry-run report
// carries the specific diagnostics a user relies on when reading it
// headlessly: the working-directory check and the resolved executable path
// (see DryRun's addValidation calls in internal/supervisor/supervisor.go).
func TestCLIDryRunOutputContainsDiagnosticMarkers(t *testing.T) {
	out, err := hermeticCmd(t, buildTestBinary(t), "--demo", "--dry-run", "data-indexer").CombinedOutput()
	if err != nil {
		t.Fatalf("--demo --dry-run data-indexer failed: %v\n%s", err, out)
	}

	got := string(out)
	for _, want := range []string{
		"Working Directory",
		"Executable resolved at",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("expected dry-run output to contain %q, got:\n%s", want, got)
		}
	}
}

// TestCLIUnknownFlagExitsNonZero asserts the standard library flag package's
// failure behavior (it calls os.Exit(2) on ExitOnError, the default error
// handling for flag.Parse in main.go) is actually reachable and non-zero
// through the built binary, not just assumed from flag's documentation.
func TestCLIUnknownFlagExitsNonZero(t *testing.T) {
	cmd := hermeticCmd(t, buildTestBinary(t), "--this-flag-does-not-exist")
	out, err := cmd.CombinedOutput()

	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected an *exec.ExitError for an unknown flag, got err=%v\noutput: %s", err, out)
	}
	if exitErr.ExitCode() == 0 {
		t.Errorf("exit code = 0, want non-zero for an unknown flag\noutput: %s", out)
	}
}
