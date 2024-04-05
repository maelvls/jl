package main

import (
	"bytes"
	"io"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_CLI(t *testing.T) {
	bincli := withBinary(t)

	t.Run("unknown top-level fields are not shown by default", func(t *testing.T) {
		c := exec.Command(bincli)
		c.Stdin = strings.NewReader(`{"level": "info", "msg": "a log message", "somerandomfield": "will not be shown"}`)
		cli := startWith(t, c).Wait()

		assert.Equal(t, "   INFO: a log message\n", contents(cli.Output))
		assert.Equal(t, 0, cli.ProcessState.ExitCode())
	})

	t.Run("nested objects are printed even if the field name is unknown", func(t *testing.T) {
		c := exec.Command(bincli)
		c.Stdin = strings.NewReader(`{"level": "INFO", "nested": {"message": "Hi", "somerandomfield": 611}}`)
		cli := startWith(t, c).Wait()
		assert.Equal(t, "   INFO: Hi [nested.somerandomfield=611]\n", contents(cli.Output))
		assert.Equal(t, 0, cli.ProcessState.ExitCode())
	})

	t.Run("supports Go's slog format", func(t *testing.T) {
		c := exec.Command(bincli)
		c.Stdin = strings.NewReader("" +
			`{"time":"2006-01-02T15:04:05Z","level":"INFO","msg":"hello","count":3}` + "\n" +
			`{"time":"2006-01-02T15:04:05Z","level":"WARN","msg":"failed","err":"EOF"}` + "\n")
		cli := startWith(t, c).Wait()
		assert.Equal(t, ""+
			"[2006-01-02 15:04:05]    INFO: hello [count=3]\n"+
			"[2006-01-02 15:04:05] WARNING: failed [err=EOF]\n", contents(cli.Output))
		assert.Equal(t, 0, cli.ProcessState.ExitCode())
	})

	t.Run("supports journald -xe -ojson format", func(t *testing.T) {
		c := exec.Command(bincli)
		c.Stdin = strings.NewReader("" +
			`{"_HOSTNAME":"example.org","_SYSTEMD_CGROUP":"/system.slice/sshd.service","_EXE":"/usr/sbin/sshd","__MONOTONIC_TIMESTAMP":"4231192657117","_CMDLINE":"sshd: unknown [priv]","_SYSTEMD_UNIT":"sshd.service","_MACHINE_ID":"be3292bb238d21a8de53f89d25ec97c4","_TRANSPORT":"stdout","PRIORITY":"5","__REALTIME_TIMESTAMP":"1686919896987169","_GID":"0","_CAP_EFFECTIVE":"1ffffffffff","__CURSOR":"s=11054c7dc82b4645a45da01c6bf62842","MESSAGE":"Invalid user hacker from 127.106.119.170 port 54520","SYSLOG_IDENTIFIER":"sshd","_UID":"0","_COMM":"sshd","SYSLOG_FACILITY":"3","_SYSTEMD_SLICE":"system.slice","_STREAM_ID":"08acce59fe1b44648b1d054f9a35156f","_PID":"1977203","_SYSTEMD_INVOCATION_ID":"9b199c04cfbe43afb339f73299c02a20","_BOOT_ID":"4cef257cf46b4818a75a0f463024e90d"}` + "\n" +
			`{"_UID":"0","__REALTIME_TIMESTAMP":"1686919897133605","_EXE":"/usr/sbin/sshd","_SYSTEMD_SLICE":"system.slice","_HOSTNAME":"example.org","_PID":"1977203","_STREAM_ID":"08acce59fe1b44648b1d054f9a35156f","_BOOT_ID":"4cef257cf46b4818a75a0f463024e90d","_CMDLINE":"sshd: unknown [priv]","_COMM":"sshd","PRIORITY":"6","_MACHINE_ID":"be3292bb238d21a8de53f89d25ec97c4","MESSAGE":"Disconnected from invalid user hacker 127.106.119.170 port 54520 [preauth]","_CAP_EFFECTIVE":"1ffffffffff","_SYSTEMD_CGROUP":"/system.slice/sshd.service","_SYSTEMD_UNIT":"sshd.service","__MONOTONIC_TIMESTAMP":"4231192803553","_TRANSPORT":"stdout","__CURSOR":"s=11054c7dc82b4645a45da01c6bf62842","_SYSTEMD_INVOCATION_ID":"9b199c04cfbe43afb339f73299c02a20","SYSLOG_IDENTIFIER":"sshd","_GID":"0","SYSLOG_FACILITY":"3"}` + "\n")
		cli := startWith(t, c).Wait()
		assert.Equal(t, ""+
			`[2023-06-16 12:51:36]  NOTICE: Invalid user hacker from 127.106.119.170 port 54520 [SYSLOG_FACILITY=3 SYSLOG_IDENTIFIER=sshd _CAP_EFFECTIVE=1ffffffffff _CMDLINE=sshd: unknown [priv] _COMM=sshd _EXE=/usr/sbin/sshd _GID=0 _HOSTNAME=example.org _PID=1977203 _SYSTEMD_SLICE=system.slice _SYSTEMD_UNIT=sshd.service _TRANSPORT=stdout _UID=0]`+"\n"+
			`[2023-06-16 12:51:37]    INFO: Disconnected from invalid user hacker 127.106.119.170 port 54520 [preauth] [SYSLOG_FACILITY=3 SYSLOG_IDENTIFIER=sshd _CAP_EFFECTIVE=1ffffffffff _CMDLINE=sshd: unknown [priv] _COMM=sshd _EXE=/usr/sbin/sshd _GID=0 _HOSTNAME=example.org _PID=1977203 _SYSTEMD_SLICE=system.slice _SYSTEMD_UNIT=sshd.service _TRANSPORT=stdout _UID=0]`+"\n",
			contents(cli.Output),
		)
		assert.Equal(t, 0, cli.ProcessState.ExitCode())
	})

	t.Run("when an 'error' field is found, 'stacktrace' is automatically shown if it exists", func(t *testing.T) {
		c := exec.Command(bincli)
		c.Stdin = strings.NewReader(`{"level": "info", "msg": "a log message", "somerandomfield": "will not be shown", "stacktrace": "go.uber.org/fx/fxevent.(*ZapLogger).logError\n\t/Users/mvalais/go/pkg/mod/go.uber.org/fx@v1.20.0/fxevent/zap.go:59\ngo.uber.org/fx/fxevent.(*ZapLogger).LogEvent", "error": "something went wrong"}`)
		cli := startWith(t, c).Wait()

		assert.Equal(t, ""+
			"   INFO: a log message [error=something went wrong]\n"+
			"    something went wrong\n"+
			"    go.uber.org/fx/fxevent.(*ZapLogger).logError\n"+
			"      /Users/mvalais/go/pkg/mod/go.uber.org/fx@v1.20.0/fxevent/zap.go:59\n"+
			"    go.uber.org/fx/fxevent.(*ZapLogger).LogEvent\n", contents(cli.Output))
		assert.Equal(t, 0, cli.ProcessState.ExitCode())
	})

	t.Run("when an 'error' field is found, 'stacktrace' cannot be disabled with --exclude-fields=stacktrace", func(t *testing.T) {
		c := exec.Command(bincli, "--exclude-fields", "stacktrace")
		c.Stdin = strings.NewReader(`{"level": "info", "msg": "a log message", "somerandomfield": "will not be shown", "stacktrace": "go.uber.org/fx/fxevent.(*ZapLogger).logError\n\t/Users/mvalais/go/pkg/mod/go.uber.org/fx@v1.20.0/fxevent/zap.go:59\ngo.uber.org/fx/fxevent.(*ZapLogger).LogEvent", "error": "something went wrong"}`)
		cli := startWith(t, c).Wait()

		assert.Equal(t, ""+
			"   INFO: a log message [error=something went wrong]\n"+
			"    something went wrong\n"+
			"    go.uber.org/fx/fxevent.(*ZapLogger).logError\n"+
			"      /Users/mvalais/go/pkg/mod/go.uber.org/fx@v1.20.0/fxevent/zap.go:59\n"+
			"    go.uber.org/fx/fxevent.(*ZapLogger).LogEvent\n", contents(cli.Output))
		assert.Equal(t, 0, cli.ProcessState.ExitCode())
	})

	t.Run("should skip fields when using --exclude-fields", func(t *testing.T) {
		c := exec.Command(bincli)
		c.Stdin = strings.NewReader(`{"level": "info", "msg": "a log message", "tobeignored": "I don't want to see this"}`)
		cli := startWith(t, c).Wait()

		output := contents(cli.Output)
		assert.Equal(t, "   INFO: a log message\n", output)
		assert.Equal(t, 0, cli.ProcessState.ExitCode())
	})
}

// Returns the path to the built CLI. Better call it only once since it needs to
// recompile.
func withBinary(t *testing.T) string {
	start := time.Now()

	cli := goBuild(t, "github.com/koenbollen/jl", nil)
	t.Logf("compiling binaries took %v, path: %s", time.Since(start).Truncate(time.Second), cli)
	return cli
}

type e2ecmd struct {
	*exec.Cmd
	Output *bytes.Buffer // Both stdout and stderr.
	T      *testing.T
}

func (cmd *e2ecmd) Wait() *e2ecmd {
	_ = cmd.Cmd.Wait()
	return cmd
}

// Runs the passed command and make sure SIGTERM is called on cleanup. Also
// dumps stderr and stdout using log.Printf.
func startWith(t *testing.T, cmd *exec.Cmd) *e2ecmd {
	buff := bytes.NewBuffer(nil)
	cmd.Stdout = createWriterLoggerStr("stdout", buff)
	cmd.Stderr = createWriterLoggerStr("stderr", buff)

	err := cmd.Start()
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
	})

	return &e2ecmd{Cmd: cmd, Output: buff, T: t}
}

func contents(f io.Reader) string {
	bytes, err := io.ReadAll(f)
	if err != nil {
		panic(err)
	}
	return string(bytes)
}

// createWriterLoggerStr returns a writer that behaves like w except that it
// logs (using log.Printf) each write to standard error, printing the
// prefix and the data written as a string.
//
// Pretty much the same as iotest.NewWriterLogger except it logs strings,
// not hexadecimal jibberish.
func createWriterLoggerStr(prefix string, w io.Writer) io.Writer {
	return &writeLogger{prefix, w}
}

type writeLogger struct {
	prefix string
	w      io.Writer
}

func (l *writeLogger) Write(p []byte) (n int, err error) {
	n, err = l.w.Write(p)
	if err != nil {
		log.Printf("%s %s: %v", l.prefix, string(p[0:n]), err)
	} else {
		log.Printf("%s %s", l.prefix, string(p[0:n]))
	}
	return
}

// Inspired by gomega's gexec package.
func goBuild(t *testing.T, packagePath string, env []string, args ...string) (compiledPath string) {
	tmpDir, err := os.MkdirTemp("", "gobuild_artifacts")
	if err != nil {
		t.Fatalf("failed to create temporary directory: %v", err)
	}
	t.Cleanup(func() {
		os.RemoveAll(tmpDir)
	})

	executable := filepath.Join(tmpDir, path.Base(packagePath))
	if runtime.GOOS == "windows" {
		executable += ".exe"
	}

	cmdArgs := append([]string{"build"}, args...)
	cmdArgs = append(cmdArgs, "-o", executable, packagePath)
	build := exec.Command("go", cmdArgs...)
	build.Env = append(build.Env, env...)

	output, err := build.CombinedOutput()
	if err != nil {
		t.Fatalf("Failed to build %s:\n\nError:\n%s\n\nOutput:\n%s\n\nCommand run: go %v", packagePath, err, string(output), strings.Join(cmdArgs, " "))
	}

	return executable
}
