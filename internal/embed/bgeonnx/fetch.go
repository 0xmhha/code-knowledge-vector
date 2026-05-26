package bgeonnx

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

// FetchModel downloads all required files for the named model into destDir.
// progress receives status messages (one per file). If destDir is empty,
// the model's DefaultModelDir is used.
//
// Existing files are skipped (no re-download). Returns the resolved
// destination directory.
func FetchModel(name, destDir string, progress func(string)) (string, error) {
	cfg, err := LookupModel(name)
	if err != nil {
		return "", err
	}
	if cfg.HFRepo == "" {
		return "", fmt.Errorf("model %q has no HuggingFace repo configured", name)
	}

	if destDir == "" {
		d, err := cfg.DefaultModelDir()
		if err != nil {
			return "", fmt.Errorf("resolve default model dir: %w", err)
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

func downloadFile(url, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return err
	}

	resp, err := http.Get(url) //nolint:gosec // URL is constructed from hardcoded HFRepo, not user input
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
