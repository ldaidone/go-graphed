package analyzer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeEmbedder is a lightweight UnderlyingEmbedder used to exercise
// NativeEmbedder's delegation logic without loading a real model.
type fakeEmbedder struct {
	vector []float32
	err    error
	closed bool
}

func (f *fakeEmbedder) Embed(text string) ([]float32, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.vector, nil
}

func (f *fakeEmbedder) EmbedBatchParallel(texts []string, n int) ([][]float32, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = f.vector
	}
	return out, nil
}

func (f *fakeEmbedder) Close() { f.closed = true }

func TestEmbedText(t *testing.T) {
	tests := []struct {
		name    string
		model   UnderlyingEmbedder
		wantVec []float32
		wantErr string
	}{
		{
			name:    "success delegates to underlying model",
			model:   &fakeEmbedder{vector: []float32{1, 0, 0}},
			wantVec: []float32{1, 0, 0},
		},
		{
			name:    "underlying error is wrapped",
			model:   &fakeEmbedder{err: errors.New("boom")},
			wantErr: "native embedding generation failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &NativeEmbedder{model: tt.model}
			got, err := e.EmbedText(context.Background(), "hello")
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error = %q, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tt.wantVec) {
				t.Fatalf("vector = %v, want %v", got, tt.wantVec)
			}
			for i := range got {
				if got[i] != tt.wantVec[i] {
					t.Errorf("vector[%d] = %v, want %v", i, got[i], tt.wantVec[i])
				}
			}
		})
	}
}

func TestEmbedTexts(t *testing.T) {
	vec := []float32{1, 2, 3}
	e := &NativeEmbedder{model: &fakeEmbedder{vector: vec}}

	got, err := e.EmbedTexts(context.Background(), []string{"a", "b", "c"}, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d vectors, want 3", len(got))
	}
	for i, v := range got {
		if len(v) != len(vec) || v[0] != vec[0] {
			t.Errorf("vector[%d] = %v, want %v", i, v, vec)
		}
	}

	eErr := &NativeEmbedder{model: &fakeEmbedder{err: errors.New("boom")}}
	if _, err := eErr.EmbedTexts(context.Background(), []string{"a"}, 1); err == nil || !strings.Contains(err.Error(), "batch embedding") {
		t.Errorf("expected batch embedding error, got %v", err)
	}
}

func TestClose(t *testing.T) {
	t.Run("releases underlying model", func(t *testing.T) {
		f := &fakeEmbedder{}
		e := &NativeEmbedder{model: f}
		if err := e.Close(context.Background()); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !f.closed {
			t.Error("underlying model was not closed")
		}
	})

	t.Run("nil model is a no-op", func(t *testing.T) {
		e := &NativeEmbedder{model: nil}
		if err := e.Close(context.Background()); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestNewNativeEmbedder_ErrorPaths(t *testing.T) {
	tests := []struct {
		name      string
		writeFile bool
		content   []byte
		wantErr   string
	}{
		{
			name:    "missing model file",
			wantErr: "failed checking model format",
		},
		{
			name:      "Q4 magic but corrupt payload",
			writeFile: true,
			content:   []byte("GTE4"),
			wantErr:   "failed to load native Q4 gte model weights",
		},
		{
			name:      "FP32 magic but corrupt payload",
			writeFile: true,
			content:   []byte("GTE1"),
			wantErr:   "failed to load native FP32 gte model weights",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "model.gtemodel")
			if tt.writeFile {
				if err := os.WriteFile(path, tt.content, 0644); err != nil {
					t.Fatal(err)
				}
			}

			_, err := NewNativeEmbedder(context.Background(), path)
			if err == nil {
				t.Fatal("expected an error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}
