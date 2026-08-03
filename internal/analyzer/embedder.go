package analyzer

import (
	"context"
	"fmt"

	"github.com/rcarmo/gte-go/gte"
)

// UnderlyingEmbedder defines a shared contract for both FP32 and Q4 implementations.
type UnderlyingEmbedder interface {
	Embed(text string) ([]float32, error)
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

// Close explicitly releases model resources.
func (e *NativeEmbedder) Close(ctx context.Context) error {
	if e.model != nil {
		e.model.Close()
	}
	return nil
}
