package tailscale

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"runtime"
)

// Path searches and returns the Tailscale CLI executable location
func Path() (bin string, err error) {
	// https://tailscale.com/kb/1080/cli/#using-the-cli
	bin, err = exec.LookPath("tailscale")
	if err != nil {
		if runtime.GOOS != "darwin" {
			return
		}
		if bin, err = exec.LookPath("/Applications/Tailscale.app/Contents/MacOS/Tailscale"); err != nil {
			return
		}
	}
	return
}

// Wrapper of the tailscale command, exposing only a minimal interface
// Set up the command Prefix before using (e.g. {"/path/to/tailscale", "-kwarg=value"})
type TSCli struct {
	Prefix []string
}

func (ts *TSCli) run(ctx context.Context, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, ts.Prefix[0], append(append([]string{}, ts.Prefix[1:]...), args...)...)
}

type TSStatus struct {
	Tun   bool     `json:"TUN"`
	TsIPs []string `json:"TailscaleIPs"`
}

func (ts *TSCli) RunStatus(ctx context.Context) (*TSStatus, error) {
	out, err := ts.run(ctx, "status", "--json", "--peers=false").Output()
	if err != nil {
		return nil, fmt.Errorf("failed to query tailscale status: %w", err)
	}
	var r TSStatus
	err = json.Unmarshal(out, &r)
	if err != nil {
		return nil, fmt.Errorf("failed to parse tailscale status: %w", err)
	}
	return &r, nil
}

func (ts *TSCli) StartCp(ctx context.Context, name string, target string, logE func(string, ...any)) (stdin io.WriteCloser, err error) {
	cmd := ts.run(ctx, "file", "cp", "-name="+name, "-", target+":")
	cmd.Stdout = nil
	stdin, err = cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("error setting up tailscale process's stdin pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("error setting up tailscale process's stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("error starting tailscale file cp: %w", err)
	}
	// make sure that the returned WriteCloser's Close blocks until the subprocess finishes
	stdinb := newWriteBlockingCloser(stdin)
	go func() {
		if errmsg, _ := io.ReadAll(stderr); len(errmsg) > 0 {
			logE("from tailscale file cp: %s", string(errmsg))
		}
		if err := cmd.Wait(); err != nil {
			logE("error running tailscale file cp: %v", err)
		}
		stdinb.NotifyExit()
	}()
	return stdinb, nil
}

func (ts *TSCli) RunGet(ctx context.Context, targetDir string) ([]byte, error) {
	return ts.run(ctx, "file", "get", "-conflict=overwrite", targetDir).CombinedOutput()
}

type writeBlockingCloser struct {
	io.WriteCloser
	chExit chan struct{}
}

func newWriteBlockingCloser(inner io.WriteCloser) *writeBlockingCloser {
	chExit := make(chan struct{})
	return &writeBlockingCloser{inner, chExit}
}

func (w *writeBlockingCloser) NotifyExit() {
	close(w.chExit)
}

func (w *writeBlockingCloser) Close() error {
	err := w.WriteCloser.Close()
	<-w.chExit
	return err
}
