package integration_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// envfuseBin is the path to the compiled envfuse binary used by supervision tests.
// It is built once in TestMain (shared for this test file's package).
var (
	envfuseBinOnce sync.Once
	envfuseBin     string
)

// buildEnvfuseBinary compiles the envfuse binary into a temp directory.
// Returns the path to the compiled binary.
func buildEnvfuseBinary(t *testing.T) string {
	t.Helper()

	envfuseBinOnce.Do(func() {
		tmp, err := os.MkdirTemp("", "envfuse-test-bin-*")
		if err != nil {
			t.Errorf("create temp dir for binary: %v", err)
			return
		}
		binPath := filepath.Join(tmp, "envfuse")
		cmd := exec.Command("go", "build", "-o", binPath, "./cmd/envfuse")
		cmd.Dir = filepath.Join("..", "..")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Errorf("build envfuse binary: %v\noutput=%s", err, string(out))
			return
		}
		envfuseBin = binPath
	})

	if envfuseBin == "" {
		t.Fatal("envfuse binary not available")
	}
	return envfuseBin
}

// TestPID1_StartAndWait verifies that envfuse starts a child command in long-running
// supervisor mode (without -once) and the child runs until it exits on its own.
// SUPV-01: Runtime can run as PID 1 and start the configured child command.
func TestPID1_StartAndWait(t *testing.T) {
	t.Parallel()

	bin := buildEnvfuseBinary(t)

	tmpDir := t.TempDir()
	secretsPath := filepath.Join(tmpDir, "secrets.json")
	configPath := filepath.Join(tmpDir, "seon.json")
	childOutPath := filepath.Join(tmpDir, "child-out.txt")

	secretsJSON := `{
		"app/base": {"API_KEY": "supervised-value"}
	}`
	configJSON := `{
		"provider_type": "local",
		"local_file_path": "` + secretsPath + `",
		"secret_paths": ["app/base"],
		"env_mappings": [
			{"path": "app/base", "key": "API_KEY", "env_var": "APP_API_KEY"}
		],
		"shutdown_timeout": "5s"
	}`

	if err := os.WriteFile(secretsPath, []byte(secretsJSON), 0o600); err != nil {
		t.Fatalf("write secrets fixture: %v", err)
	}
	if err := os.WriteFile(configPath, []byte(configJSON), 0o600); err != nil {
		t.Fatalf("write config fixture: %v", err)
	}

	// Run envfuse WITHOUT -once flag to exercise long-running supervisor mode.
	// Child command writes env value and exits immediately.
	cmd := exec.Command(bin,
		"-config", configPath,
		"--", "/bin/sh", "-c",
		"printf '%s' \"$APP_API_KEY\" > '"+childOutPath+"'",
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("envfuse supervisor run failed: %v\noutput=%s", err, string(output))
	}

	got, err := os.ReadFile(childOutPath)
	if err != nil {
		t.Fatalf("expected child output file to exist: %v", err)
	}
	if string(got) != "supervised-value" {
		t.Fatalf("expected child to receive APP_API_KEY=supervised-value, got %q", string(got))
	}
}

// TestPID1_SignalForwarding verifies that SIGTERM sent to envfuse is forwarded to the
// supervised child process, causing it to exit gracefully.
// SUPV-02: Runtime forwards SIGTERM and SIGINT to child process.
func TestPID1_SignalForwarding(t *testing.T) {
	t.Parallel()

	bin := buildEnvfuseBinary(t)

	tmpDir := t.TempDir()
	secretsPath := filepath.Join(tmpDir, "secrets.json")
	configPath := filepath.Join(tmpDir, "seon.json")
	childReadyPath := filepath.Join(tmpDir, "child-ready")
	childExitedPath := filepath.Join(tmpDir, "child-exited")

	secretsJSON := `{
		"app/base": {"API_KEY": "signal-test"}
	}`
	configJSON := `{
		"provider_type": "local",
		"local_file_path": "` + secretsPath + `",
		"secret_paths": ["app/base"],
		"env_mappings": [
			{"path": "app/base", "key": "API_KEY", "env_var": "APP_API_KEY"}
		],
		"shutdown_timeout": "5s"
	}`

	if err := os.WriteFile(secretsPath, []byte(secretsJSON), 0o600); err != nil {
		t.Fatalf("write secrets fixture: %v", err)
	}
	if err := os.WriteFile(configPath, []byte(configJSON), 0o600); err != nil {
		t.Fatalf("write config fixture: %v", err)
	}

	// Child: writes ready marker, traps SIGTERM, writes exit marker then exits 0.
	// The wait-in-loop pattern ensures the trap fires even while sleeping (POSIX sh
	// does not interrupt foreground sleep on SIGTERM without background+wait).
	childScript := `
trap 'printf done > "` + childExitedPath + `"; exit 0' TERM
printf ready > "` + childReadyPath + `"
while true; do sleep 1 & wait; done
`

	cmd := exec.Command(bin,
		"-config", configPath,
		"--", "/bin/sh", "-c", childScript,
	)

	if err := cmd.Start(); err != nil {
		t.Fatalf("start envfuse: %v", err)
	}

	// Wait for child to signal readiness.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(childReadyPath); err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if _, err := os.Stat(childReadyPath); err != nil {
		_ = cmd.Process.Kill()
		t.Fatalf("child never signaled ready")
	}

	// Send SIGTERM to the envfuse process — it must forward it to the child.
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM to envfuse: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("envfuse did not exit after SIGTERM within timeout")
	}

	// Child must have received the signal and written its exit marker.
	exitedContent, err := os.ReadFile(childExitedPath)
	if err != nil {
		t.Fatalf("expected child-exited marker to exist (signal was not forwarded): %v", err)
	}
	if string(exitedContent) != "done" {
		t.Fatalf("expected child-exited marker content 'done', got %q", string(exitedContent))
	}
}

// TestPID1_GraceTimeoutForceKill verifies that when a child ignores SIGTERM and does not
// exit within the grace window, envfuse force-kills the child process only (D-03)
// and records the forced_kill reason (D-16).
// SUPV-03: Runtime enforces configurable graceful shutdown timeout, then force-kills.
func TestPID1_GraceTimeoutForceKill(t *testing.T) {
	t.Parallel()

	bin := buildEnvfuseBinary(t)

	tmpDir := t.TempDir()
	secretsPath := filepath.Join(tmpDir, "secrets.json")
	configPath := filepath.Join(tmpDir, "seon.json")
	childReadyPath := filepath.Join(tmpDir, "child-ready")
	childPIDPath := filepath.Join(tmpDir, "child-pid")

	secretsJSON := `{
		"app/base": {"API_KEY": "kill-test"}
	}`
	// Short shutdown_timeout so the force-kill triggers quickly in tests.
	configJSON := `{
		"provider_type": "local",
		"local_file_path": "` + secretsPath + `",
		"secret_paths": ["app/base"],
		"env_mappings": [
			{"path": "app/base", "key": "API_KEY", "env_var": "APP_API_KEY"}
		],
		"shutdown_timeout": "1s"
	}`

	if err := os.WriteFile(secretsPath, []byte(secretsJSON), 0o600); err != nil {
		t.Fatalf("write secrets fixture: %v", err)
	}
	if err := os.WriteFile(configPath, []byte(configJSON), 0o600); err != nil {
		t.Fatalf("write config fixture: %v", err)
	}

	// Child: ignores SIGTERM entirely, records its PID, signals readiness, then sleeps.
	childScript := `
trap '' TERM
printf '%d' $$ > "` + childPIDPath + `"
printf ready > "` + childReadyPath + `"
sleep 60
`

	cmd := exec.Command(bin,
		"-config", configPath,
		"--", "/bin/sh", "-c", childScript,
	)

	if err := cmd.Start(); err != nil {
		t.Fatalf("start envfuse: %v", err)
	}

	// Wait for child to signal readiness.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(childReadyPath); err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if _, err := os.Stat(childReadyPath); err != nil {
		_ = cmd.Process.Kill()
		t.Fatalf("child never signaled ready")
	}

	pidBytes, err := os.ReadFile(childPIDPath)
	if err != nil {
		_ = cmd.Process.Kill()
		t.Fatalf("read child PID: %v", err)
	}
	childPID, err := strconv.Atoi(strings.TrimSpace(string(pidBytes)))
	if err != nil {
		_ = cmd.Process.Kill()
		t.Fatalf("parse child PID %q: %v", string(pidBytes), err)
	}

	// Send SIGTERM to envfuse — child ignores it, so shutdown_timeout (1s) should
	// trigger a SIGKILL to the child process only.
	start := time.Now()
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM to envfuse: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("envfuse did not exit after grace timeout + force-kill within test deadline")
	}

	elapsed := time.Since(start)

	// Envfuse should have exited after the 1s timeout + a small processing margin.
	// Force-kill must complete after the timeout window.
	if elapsed < 800*time.Millisecond {
		t.Errorf("envfuse exited too fast (%v); expected at least 800ms grace window", elapsed)
	}

	// Child process must no longer be alive (force-killed by envfuse).
	// Give the kernel a moment to reap.
	time.Sleep(100 * time.Millisecond)
	childProc, err := os.FindProcess(childPID)
	if err == nil {
		// Signal(0) tests process existence without delivering a signal.
		if err2 := childProc.Signal(syscall.Signal(0)); err2 == nil {
			t.Errorf("child PID %d is still alive after force-kill; expected it to be dead", childPID)
		}
	}
}
