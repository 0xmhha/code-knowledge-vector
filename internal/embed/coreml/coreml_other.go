//go:build !darwin || !tokenizers

package coreml

import (
	"context"

	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

// Open returns an error on non-macOS platforms.
func Open(opts Options) (*Adapter, error) {
	return nil, errNotAvailable
}

func (a *Adapter) Name() string        { return "" }
func (a *Adapter) Dimension() int      { return 0 }
func (a *Adapter) MaxInputTokens() int { return 0 }
func (a *Adapter) Identity() types.EmbeddingIdentity {
	return types.EmbeddingIdentity{Provider: "coreml"}
}
func (a *Adapter) Close() error { return nil }
func (a *Adapter) Embed(_ context.Context, _ []string) ([][]float32, error) {
	return nil, errNotAvailable
}
