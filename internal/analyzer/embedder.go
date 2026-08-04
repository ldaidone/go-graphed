package analyzer

import (
	"context"
	"fmt"

	"github.com/rcarmo/gte-go/gte"
)

// UnderlyingEmbedder defines a shared contract for both FP32 and Q4 implementations.
type UnderlyingEmbedder interface {
	Embed(text string) ([]float32, error)
	// EmbedBatchParallel embeds texts concurrently using n worker goroutines.
	// Each worker carries its own inference buffers but shares the model
	// weights, so the per-text cost amortises the fixed forward-pass overhead.
	// n <= 0 means runtime.NumCPU(). Both gte implementations provide this
	// through the pure-Go SIMD path, so no CGO is required.
	EmbedBatchParallel(texts []string, n int) ([][]float32, error)
	Close()
}

// NativeEmbedder keeps your public pipeline agnostic to the quantization strategy.
type NativeEmbedder struct {
	model UnderlyingEmbedder
}

// NewNativeEmbedder dynamically evaluates the file header and assigns the correct struct.
func NewNativeEmbedder(ctx context.Context, modelPath string) (*NativeEmbedder, error) {
	// Handle the error returned by IsQ4Model
	isQ4, err := gte.IsQ4Model(modelPath)
	if err != nil {
		return nil, fmt.Errorf("failed checking model format: %w", err)
	}

	var impl UnderlyingEmbedder

	if isQ4 {
		// LoadQ4 returns a *gte.ModelQ4, which fits into our interface
		q4Model, err := gte.LoadQ4(modelPath)
		if err != nil {
			return nil, fmt.Errorf("failed to load native Q4 gte model weights: %w", err)
		}
		impl = q4Model
	} else {
		// Load returns a standard *gte.Model, which also fits into our interface
		fp32Model, err := gte.Load(modelPath)
		if err != nil {
			return nil, fmt.Errorf("failed to load native FP32 gte model weights: %w", err)
		}
		impl = fp32Model
	}

	return &NativeEmbedder{model: impl}, nil
}

// EmbedText channels the request down into whichever model format was loaded.
func (e *NativeEmbedder) EmbedText(ctx context.Context, text string) ([]float32, error) {
	vector, err := e.model.Embed(text)
	if err != nil {
		return nil, fmt.Errorf("native embedding generation failed: %w", err)
	}
	return vector, nil
}

// EmbedTexts embeds a batch of texts concurrently, preserving input order.
// It routes through the underlying model's batch path so each worker reuses
// its own buffers instead of reallocating per call (the hot path during a
// graph build with hundreds of documents and entities).
func (e *NativeEmbedder) EmbedTexts(ctx context.Context, texts []string, workers int) ([][]float32, error) {
	vectors, err := e.model.EmbedBatchParallel(texts, workers)
	if err != nil {
		return nil, fmt.Errorf("native batch embedding generation failed: %w", err)
	}
	return vectors, nil
}

// Close explicitly releases model resources.
func (e *NativeEmbedder) Close(ctx context.Context) error {
	if e.model != nil {
		e.model.Close()
	}
	return nil
}
