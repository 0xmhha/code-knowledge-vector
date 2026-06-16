// Package model manages embedding model files: download, cache
// directory resolution, and format conversion.
package model

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/0xmhha/code-knowledge-vector/internal/embed/registry"
)

// downloadClient bounds the network phases of a model download without
// capping the transfer itself. Model files are large (hundreds of MB), so a
// tight overall Timeout would abort a legitimate slow-but-progressing
// download; instead the transport bounds connect/TLS/first-byte so an
// unresponsive server fails fast, with a generous overall backstop against a
// mid-stream stall.
var downloadClient = &http.Client{
	Timeout: time.Hour,
	Transport: &http.Transport{
		DialContext:           (&net.Dialer{Timeout: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   30 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
	},
}

// FetchModel downloads the ONNX model and tokenizer for the named
// model into destDir. Existing files are skipped. progress receives
// human-readable status messages. Returns the resolved directory.
func FetchModel(name, destDir string, progress func(string)) (string, error) {
	cfg, err := registry.Lookup(name)
	if err != nil {
		return "", err
	}
	if cfg.HFRepo == "" {
		return "", fmt.Errorf("model %q has no download source configured", name)
	}

	if destDir == "" {
		d, err := cfg.DefaultModelDir()
		if err != nil {
			return "", fmt.Errorf("resolve model directory: %w", err)
		}
		destDir = d
	}

	files := cfg.FetchFiles()
	for _, relPath := range files {
		dest := filepath.Join(destDir, relPath)
		if _, err := os.Stat(dest); err == nil {
			progress(fmt.Sprintf("  %s: already exists, skipping", relPath))
			continue
		}

		url := fmt.Sprintf("https://huggingface.co/%s/resolve/main/%s", cfg.HFRepo, relPath)
		progress(fmt.Sprintf("  %s: downloading from %s ...", relPath, cfg.HFRepo))

		if err := downloadFile(url, dest); err != nil {
			return "", fmt.Errorf("download %s: %w", relPath, err)
		}
		fi, _ := os.Stat(dest)
		progress(fmt.Sprintf("  %s: done (%d MB)", relPath, fi.Size()/(1024*1024)))
	}
	return destDir, nil
}

// CacheDir returns the default model cache root: ~/.cache/ckv/models/.
func CacheDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cache", "ckv", "models"), nil
}

func downloadFile(url, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return err
	}

	resp, err := downloadClient.Get(url) //nolint:gosec
	if err != nil {
		return fmt.Errorf("HTTP GET: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}

	tmp := dest + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dest)
}
