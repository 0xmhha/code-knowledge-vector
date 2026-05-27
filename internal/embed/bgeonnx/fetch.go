package bgeonnx

import "github.com/0xmhha/code-knowledge-vector/internal/embed/model"

// FetchModel delegates to the model package's downloader.
// Kept as a bridge for callers that currently import bgeonnx.FetchModel.
func FetchModel(name, destDir string, progress func(string)) (string, error) {
	return model.FetchModel(name, destDir, progress)
}
