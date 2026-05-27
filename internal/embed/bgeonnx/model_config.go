// Model configuration bridge: re-exports registry types so existing
// bgeonnx internal code (session, pooling, tokenizer) can reference
// them without a package-wide import change. New code outside bgeonnx
// should import internal/embed/registry directly.
package bgeonnx

import "github.com/0xmhha/code-knowledge-vector/internal/embed/registry"

// Type aliases for backward compatibility within the bgeonnx package.
type ModelConfig = registry.ModelConfig
type PoolingMode = registry.PoolingMode
type ExtraInputFn = registry.ExtraInputFn

const (
	PoolingCLS       = registry.PoolingCLS
	PoolingMean      = registry.PoolingMean
	PoolingLastToken = registry.PoolingLastToken
)

var (
	ZeroExtraInput        = registry.ZeroExtraInput
	PositionIDsExtraInput = registry.PositionIDsExtraInput
)

const DefaultModelName = registry.DefaultModelName

func LookupModel(name string) (ModelConfig, error) {
	return registry.Lookup(name)
}

func RegisteredModels() []ModelConfig {
	return registry.List()
}

// EstimatedRAMMB resolves model options and returns the registered
// memory estimate. Used by the build pipeline's pre-flight check.
func EstimatedRAMMB(opts Options) uint64 {
	cfg, _, err := resolveModel(opts)
	if err != nil {
		return 0
	}
	return cfg.EstimatedRAMMB
}
