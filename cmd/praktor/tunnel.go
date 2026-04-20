package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"time"
)

var cloudflaredURLRegex = regexp.MustCompile(`https://[a-z0-9-]+\.trycloudflare\.com`)

// startCloudflaredTunnel launches a `cloudflared tunnel --url http://localhost:<port>`
// subprocess, parses its output for the public URL, and writes
// <dataDir>/data/qr_payload.json with {"tunnel_url":"...","bearer_token":"..."}.
//
// Returns the bearer token (generated if existingAuth is empty). The function
// returns quickly; URL discovery + payload writing happen in a background
// goroutine with a 30s timeout.
//
// If cloudflared is not in PATH, a warning is logged and the bearer token is
// still returned so the gateway can run without a tunnel.
//
// If crypto/rand fails to generate a bearer token, ("", err) is returned so
// the caller can abort startup rather than exposing an unauthenticated server.
func startCloudflaredTunnel(ctx context.Context, port int, existingAuth string, dataDir string) (string, error) {
	bearerToken := existingAuth
	if bearerToken == "" {
		b := make([]byte, 16)
		if _, err := rand.Read(b); err != nil {
			return "", fmt.Errorf("generate bearer token: %w", err)
		}
		bearerToken = hex.EncodeToString(b)
	}

	if _, err := exec.LookPath("cloudflared"); err != nil {
		slog.Warn("cloudflared not found in PATH, skipping tunnel setup")
		return bearerToken, nil
	}

	cmd := exec.CommandContext(ctx, "cloudflared", "tunnel", "--url", fmt.Sprintf("http://localhost:%d", port))
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		slog.Error("cloudflared stdout pipe failed", "error", err)
		return bearerToken, nil
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		slog.Error("cloudflared stderr pipe failed", "error", err)
		return bearerToken, nil
	}

	if err := cmd.Start(); err != nil {
		slog.Error("failed to start cloudflared", "error", err)
		return bearerToken, nil
	}

	slog.Info("cloudflared tunnel starting", "pid", cmd.Process.Pid)

	urlCh := make(chan string, 1)
	doneCh := make(chan struct{})

	// Parse both stdout and stderr — cloudflared writes connection banner to stderr.
	go parseCloudflaredOutput(stdout, urlCh)
	go parseCloudflaredOutput(stderr, urlCh)

	go func() {
		select {
		case url := <-urlCh:
			slog.Info("cloudflared tunnel ready", "url", url)
			if err := writeQRPayload(url, bearerToken, dataDir); err != nil {
				slog.Error("failed to write qr payload", "error", err)
				return
			}
			fmt.Printf("QR PAYLOAD: {\"tunnel_url\":\"%s\",\"bearer_token\":\"%s\"}\n", url, bearerToken)
		case <-doneCh:
			slog.Warn("cloudflared exited before URL discovered")
		case <-time.After(30 * time.Second):
			slog.Error("cloudflared tunnel URL not discovered within 30s")
		case <-ctx.Done():
			return
		}
	}()

	// Reap the process when it exits so we don't leave zombies, and signal
	// any goroutine still waiting for the URL so it can bail out early.
	go func() {
		if err := cmd.Wait(); err != nil && ctx.Err() == nil {
			slog.Warn("cloudflared exited", "error", err)
		}
		close(doneCh)
	}()

	return bearerToken, nil
}

func parseCloudflaredOutput(r io.Reader, urlCh chan<- string) {
	scanner := bufio.NewScanner(r)
	// cloudflared banners can be long — bump the buffer.
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	sent := false
	for scanner.Scan() {
		line := scanner.Text()
		if !sent && containsTrycloudflare(line) {
			if match := cloudflaredURLRegex.FindString(line); match != "" {
				select {
				case urlCh <- match:
					sent = true
				default:
				}
			}
		}
	}
}

func containsTrycloudflare(line string) bool {
	for i := 0; i+len("trycloudflare.com") <= len(line); i++ {
		if line[i:i+len("trycloudflare.com")] == "trycloudflare.com" {
			return true
		}
	}
	return false
}

func writeQRPayload(tunnelURL, bearerToken, dataDir string) error {
	dir := filepath.Join(dataDir, "data")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir data: %w", err)
	}
	payload := map[string]string{
		"tunnel_url":   tunnelURL,
		"bearer_token": bearerToken,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	path := filepath.Join(dir, "qr_payload.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write payload: %w", err)
	}
	return nil
}
