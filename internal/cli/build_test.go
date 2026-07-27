package cli

import "testing"

func TestCleanSourcePath(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "plain directory",
			input:    "internal",
			expected: "internal",
		},
		{
			name:     "current directory dot",
			input:    ".",
			expected: ".",
		},
		{
			name:     "go-style recursive wildcard",
			input:    "./...",
			expected: ".",
		},
		{
			name:     "nested go-style wildcard",
			input:    "internal/...",
			expected: "internal",
		},
		{
			name:     "deeply nested wildcard",
			input:    "src/pkg/internal/...",
			expected: "src/pkg/internal",
		},
		{
			name:     "bare ellipsis",
			input:    "...",
			expected: ".",
		},
		{
			name:     "root ellipsis trailing slash",
			input:    "/...",
			expected: ".",
		},
		{
			name:     "absolute path with wildcard",
			input:    "/home/user/project/...",
			expected: "/home/user/project",
		},
		{
			name:     "path without wildcard is unchanged",
			input:    "some/deep/path",
			expected: "some/deep/path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cleanSourcePath(tt.input)
			if got != tt.expected {
				t.Errorf("cleanSourcePath(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}
