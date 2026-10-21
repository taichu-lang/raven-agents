package local

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/taichu-lang/raven-agents/sandbox"
)

// The tests drive the sandbox through /bin/sh, so they only run on unix-like platforms; the windows code path
// is covered by compilation only.
func skipOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("test uses /bin/sh")
	}
}

func newSandbox(t *testing.T, spec *sandbox.Spec) *Sandbox {
	t.Helper()
	skipOnWindows(t)

	provider := NewProvider()
	t.Cleanup(func() { _ = provider.Close(context.Background()) })

	sb, err := provider.Create(context.Background(), spec)
	if err != nil {
		t.Fatalf("create sandbox: %v", err)
	}
	t.Cleanup(func() { _ = sb.Close(context.Background()) })

	return sb.(*Sandbox)
}

func execOne(t *testing.T, sb *Sandbox, req *sandbox.ExecRequest) *sandbox.ExecResult {
	t.Helper()
	req.Shell = "/bin/sh"

	result, err := sb.Exec(context.Background(), req)
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	return result
}

func TestExecStdoutAndExitCode(t *testing.T) {
	sb := newSandbox(t, nil)

	result := execOne(t, sb, &sandbox.ExecRequest{Command: "echo hello"})
	if result.ExitCode != 0 || result.Stdout != "hello\n" || result.Stderr != "" {
		t.Fatalf("unexpected result: %+v", result)
	}

	result = execOne(t, sb, &sandbox.ExecRequest{Command: "echo oops >&2; exit 3"})
	if result.ExitCode != 3 || result.Stderr != "oops\n" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestExecEnvLayers(t *testing.T) {
	sb := newSandbox(t, &sandbox.Spec{Env: map[string]string{"RAVEN_A": "1"}})

	result := execOne(t, sb, &sandbox.ExecRequest{
		Command: `echo "$RAVEN_A$RAVEN_B"`,
		Env:     map[string]string{"RAVEN_B": "2"},
	})
	if result.Stdout != "12\n" {
		t.Fatalf("unexpected stdout: %q", result.Stdout)
	}
}

func TestExecWorkingDir(t *testing.T) {
	defaultDir := t.TempDir()
	overrideDir := t.TempDir()

	sb := newSandbox(t, &sandbox.Spec{WorkingDir: defaultDir})

	expected, err := filepath.EvalSymlinks(defaultDir)
	if err != nil {
		t.Fatalf("eval symlinks: %v", err)
	}
	result := execOne(t, sb, &sandbox.ExecRequest{Command: "pwd"})
	if got := strings.TrimSpace(result.Stdout); got != expected {
		t.Fatalf("working dir = %q, want %q", got, expected)
	}

	expected, err = filepath.EvalSymlinks(overrideDir)
	if err != nil {
		t.Fatalf("eval symlinks: %v", err)
	}
	result = execOne(t, sb, &sandbox.ExecRequest{Command: "pwd", WorkingDir: overrideDir})
	if got := strings.TrimSpace(result.Stdout); got != expected {
		t.Fatalf("override working dir = %q, want %q", got, expected)
	}
}

func TestExecStdin(t *testing.T) {
	sb := newSandbox(t, nil)

	result := execOne(t, sb, &sandbox.ExecRequest{
		Command: "cat",
		Stdin:   strings.NewReader("piped input"),
	})
	if result.Stdout != "piped input" {
		t.Fatalf("unexpected stdout: %q", result.Stdout)
	}
}

func TestExecStreaming(t *testing.T) {
	sb := newSandbox(t, nil)

	output := make(chan sandbox.OutputEvent, 64)
	result := execOne(t, sb, &sandbox.ExecRequest{
		Command: "echo out; echo err >&2",
		Output:  output,
	})
	close(output)

	var stdout, stderr []byte
	for event := range output {
		switch event.Stream {
		case sandbox.StreamStdout:
			stdout = append(stdout, event.Data...)
		case sandbox.StreamStderr:
			stderr = append(stderr, event.Data...)
		}
	}

	if string(stdout) != result.Stdout || string(stderr) != result.Stderr {
		t.Fatalf("streamed %q/%q != captured %q/%q", stdout, stderr, result.Stdout, result.Stderr)
	}
}

func TestExecTruncation(t *testing.T) {
	sb := newSandbox(t, nil)

	result := execOne(t, sb, &sandbox.ExecRequest{
		Command:        "printf '0123456789'",
		MaxOutputBytes: 4,
	})
	if result.Stdout != "0123" || !result.Truncated {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestExecTimeout(t *testing.T) {
	sb := newSandbox(t, nil)

	start := time.Now()
	result := execOne(t, sb, &sandbox.ExecRequest{
		Command: "sleep 30",
		Timeout: 100 * time.Millisecond,
	})
	elapsed := time.Since(start)

	if !result.TimedOut || result.ExitCode != -1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("timeout kill took too long: %v", elapsed)
	}
}

func TestExecTTYNotSupported(t *testing.T) {
	sb := newSandbox(t, nil)

	_, err := sb.Exec(context.Background(), &sandbox.ExecRequest{Command: "true", TTY: true})
	if !errors.Is(err, sandbox.ErrNotSupported) {
		t.Fatalf("err = %v, want ErrNotSupported", err)
	}
}

func TestExecCallerCancellation(t *testing.T) {
	sb := newSandbox(t, nil)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	_, err := sb.Exec(ctx, &sandbox.ExecRequest{Command: "sleep 30", Shell: "/bin/sh"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestCloseKillsRunningCommands(t *testing.T) {
	skipOnWindows(t)

	provider := NewProvider()
	defer func() { _ = provider.Close(context.Background()) }()

	sb, err := provider.Create(context.Background(), nil)
	if err != nil {
		t.Fatalf("create sandbox: %v", err)
	}

	type outcome struct {
		result *sandbox.ExecResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := sb.Exec(context.Background(), &sandbox.ExecRequest{
			Command: "sleep 30",
			Shell:   "/bin/sh",
		})
		done <- outcome{result, err}
	}()

	time.Sleep(200 * time.Millisecond)

	closeCtx, cancelClose := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelClose()
	if err := sb.Close(closeCtx); err != nil {
		t.Fatalf("close sandbox: %v", err)
	}

	select {
	case out := <-done:
		if out.err != nil {
			t.Fatalf("exec after close: %v", out.err)
		}
		if out.result.ExitCode != -1 {
			t.Fatalf("unexpected result: %+v", out.result)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("exec did not return after sandbox close")
	}

	// A closed sandbox refuses new commands.
	_, err = sb.Exec(context.Background(), &sandbox.ExecRequest{Command: "true", Shell: "/bin/sh"})
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("err = %v, want ErrClosed", err)
	}
}

func TestProviderLifecycle(t *testing.T) {
	skipOnWindows(t)

	provider := NewProvider()
	if provider.Kind() != Kind {
		t.Fatalf("kind = %q, want %q", provider.Kind(), Kind)
	}

	sb, err := provider.Create(context.Background(), &sandbox.Spec{ID: "conv-1"})
	if err != nil {
		t.Fatalf("create sandbox: %v", err)
	}
	if sb.ID() != "conv-1" {
		t.Fatalf("id = %q, want %q", sb.ID(), "conv-1")
	}

	if _, err := provider.Create(context.Background(), &sandbox.Spec{ID: "conv-1"}); err == nil {
		t.Fatal("duplicate id should be rejected")
	}

	if err := provider.Destroy(context.Background(), "conv-1"); err != nil {
		t.Fatalf("destroy sandbox: %v", err)
	}
	if err := provider.Destroy(context.Background(), "conv-1"); !errors.Is(err, sandbox.ErrNotFound) {
		t.Fatalf("destroy again: err = %v, want ErrNotFound", err)
	}

	if err := provider.Close(context.Background()); err != nil {
		t.Fatalf("close provider: %v", err)
	}
	if _, err := provider.Create(context.Background(), nil); !errors.Is(err, ErrClosed) {
		t.Fatalf("create after provider close: err = %v, want ErrClosed", err)
	}
}

func TestCreateValidatesWorkingDir(t *testing.T) {
	provider := NewProvider()
	defer func() { _ = provider.Close(context.Background()) }()

	missing := filepath.Join(t.TempDir(), "missing")
	if _, err := provider.Create(context.Background(), &sandbox.Spec{WorkingDir: missing}); err == nil {
		t.Fatal("missing working dir should be rejected")
	}

	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if _, err := provider.Create(context.Background(), &sandbox.Spec{WorkingDir: file}); err == nil {
		t.Fatal("working dir pointing at a file should be rejected")
	}
}

func TestShellArgs(t *testing.T) {
	cases := []struct {
		shell string
		want  []string
	}{
		{"/bin/sh", []string{"-c", "ls"}},
		{"/bin/bash", []string{"-c", "ls"}},
		{"powershell", []string{"-NoProfile", "-Command", "ls"}},
		{`C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`, []string{"-NoProfile", "-Command", "ls"}},
		{"pwsh", []string{"-NoProfile", "-Command", "ls"}},
		{"cmd", []string{"/c", "ls"}},
		{`C:\Windows\System32\cmd.exe`, []string{"/c", "ls"}},
	}
	for _, c := range cases {
		got := shellArgs(c.shell, "ls")
		if strings.Join(got, " ") != strings.Join(c.want, " ") {
			t.Errorf("shellArgs(%q) = %v, want %v", c.shell, got, c.want)
		}
	}
}
