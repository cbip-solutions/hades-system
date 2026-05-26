// SPDX-License-Identifier: MIT
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"
)

type Flow struct {
	Request  FlowRequest  `json:"request"`
	Response FlowResponse `json:"response"`
}

type FlowRequest struct {
	Host    string     `json:"host"`
	Method  string     `json:"method"`
	Path    string     `json:"path"`
	Headers [][]string `json:"headers"`
	Body    string     `json:"body"`
}

type FlowResponse struct {
	StatusCode int        `json:"status_code"`
	Headers    [][]string `json:"headers"`
}

type harDump struct {
	Log harLog `json:"log"`
}

type harLog struct {
	Entries []harEntry `json:"entries"`
}

type harEntry struct {
	Request  harRequest  `json:"request"`
	Response harResponse `json:"response"`
}

type harRequest struct {
	Method   string       `json:"method"`
	URL      string       `json:"url"`
	Headers  []harHeader  `json:"headers"`
	PostData *harPostData `json:"postData,omitempty"`
}

type harResponse struct {
	Status  int         `json:"status"`
	Headers []harHeader `json:"headers"`
}

type harHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type harPostData struct {
	MimeType string `json:"mimeType"`
	Text     string `json:"text"`
}

func loadFlowDump(path string) ([]Flow, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var dump harDump
	if err := json.Unmarshal(b, &dump); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	out := make([]Flow, 0, len(dump.Log.Entries))
	for _, e := range dump.Log.Entries {
		host, pathPart := splitHARURL(e.Request.URL)
		if host != "api.anthropic.com" {
			continue
		}
		body := ""
		if e.Request.PostData != nil {
			body = e.Request.PostData.Text
		}
		out = append(out, Flow{
			Request: FlowRequest{
				Host:    host,
				Method:  e.Request.Method,
				Path:    pathPart,
				Headers: harHeadersToPairs(e.Request.Headers),
				Body:    body,
			},
			Response: FlowResponse{
				StatusCode: e.Response.Status,
				Headers:    harHeadersToPairs(e.Response.Headers),
			},
		})
	}
	return out, nil
}

func splitHARURL(raw string) (host, path string) {
	u, err := url.Parse(raw)
	if err != nil || u == nil {
		return "", ""
	}
	host = u.Host
	path = u.Path
	if path == "" {
		path = "/"
	}
	return host, path
}

func harHeadersToPairs(in []harHeader) [][]string {
	out := make([][]string, len(in))
	for i, h := range in {
		out[i] = []string{h.Name, h.Value}
	}
	return out
}

const captureShutdownTimeout = 5 * time.Second

func runCaptureReal(cfg *Config, stdout, stderr io.Writer) error {
	st := CheckMitmStatus()
	fmt.Fprintln(stdout, st.String())
	if !st.Available {
		return fmt.Errorf("mitmproxy required; see install hint above")
	}
	if !st.CertTrusted {
		fmt.Fprintln(stderr, "WARNING: mitmproxy CA cert not at ~/.mitmproxy/mitmproxy-ca-cert.pem")
		fmt.Fprintln(stderr, "  HTTPS interception will fail without trust. Run mitmproxy once + add cert to System keychain.")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd, err := LaunchMitm(ctx, st, cfg.ListenAddr, cfg.OutPath)
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, "")
	fmt.Fprintln(stdout, "Capture session ready. Steps:")
	fmt.Fprintln(stdout, "  1. New terminal: export HTTPS_PROXY=http://"+cfg.ListenAddr)

	fmt.Fprintln(stdout, "                   export NODE_EXTRA_CA_CERTS=$HOME/.mitmproxy/mitmproxy-ca-cert.pem")
	fmt.Fprintln(stdout, "  2. Run:          "+cfg.CCBinary+" --version  # warm")
	fmt.Fprintln(stdout, "  3. Run:          "+cfg.CCBinary+"  (send 'hi' + a tool call + Ctrl-C)")
	fmt.Fprintln(stdout, "  4. Press Ctrl-C HERE to stop capture and write "+cfg.OutPath)
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sig)
	<-sig
	return finalizeCapture(cmd, cancel, cfg.OutPath, stdout)
}

func finalizeCapture(cmd *exec.Cmd, cancel context.CancelFunc, outPath string, stdout io.Writer) error {
	waitErr := shutdownMitmdump(cmd, cancel)
	flows, loadErr := loadFlowDump(outPath)
	if loadErr != nil {
		if waitErr != nil {
			return fmt.Errorf("validate captured dump: %w (mitmdump exit: %v)", loadErr, waitErr)
		}
		return fmt.Errorf("validate captured dump: %w", loadErr)
	}
	if len(flows) == 0 {
		if waitErr != nil {
			return fmt.Errorf("captured 0 anthropic flows; HTTPS_PROXY unset or cert untrusted? (mitmdump exit: %v)", waitErr)
		}
		return fmt.Errorf("captured 0 anthropic flows; HTTPS_PROXY unset or cert untrusted?")
	}
	fmt.Fprintf(stdout, "captured %d anthropic flows -> %s\n", len(flows), outPath)
	return nil
}

func shutdownMitmdump(cmd *exec.Cmd, cancel context.CancelFunc) error {
	if cmd == nil || cmd.Process == nil {

		return nil
	}

	_ = cmd.Process.Signal(syscall.SIGINT)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return filterSignalExit(err, syscall.SIGINT)
	case <-time.After(captureShutdownTimeout):

		cancel()
		return filterSignalExit(<-done, syscall.SIGKILL)
	}
}

func filterSignalExit(err error, sig syscall.Signal) error {
	if err == nil {
		return nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok {
			if ws.Signaled() && ws.Signal() == sig {
				return nil
			}
		}
	}
	return err
}
