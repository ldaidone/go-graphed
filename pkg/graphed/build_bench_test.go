package graphed_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ldaidone/go-graphed/pkg/graphed"
)

// makeGoTree writes n distinct Go source files into a temp directory so the
// parse stage has real work to do. Each file declares a few types, methods
// and comments that the golang extractor turns into entities.
func makeGoTree(t testing.TB, n int) string {
	t.Helper()
	root := t.TempDir()
	for i := range n {
		src := fmt.Sprintf(`// Package p%d is a benchmark fixture.
package p%d

// Service%d handles requests for subsystem %d.
type Service%d struct {
	ID int
}

// Handle processes the request for Service%d.
func (s *Service%d) Handle() error {
	return nil
}
`, i, i, i, i, i, i, i)
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("svc_%d.go", i)), []byte(src), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// BenchmarkBuildParseJobs measures end-to-end Build time (scan, parse,
// analyze, export) as the parser worker count grows. Jobs=1 is the serial
// baseline; every other value exercises the worker pool. No model is passed,
// so the embedding stage (which dominates wall time when enabled) does not
// mask the parse-stage gains.
//
// Run it with:
//
//	go test ./pkg/graphed -run '^$' -bench BenchmarkBuildParseJobs -benchmem
//
// Add -count=5 for stable numbers and use -benchtime=10x when file count is
// large. Note: -cpu changes GOMAXPROCS, NOT our --jobs worker count.
func BenchmarkBuildParseJobs(b *testing.B) {
	const files = 500

	// Warm up the extractor caches once outside the timed loop.
	root := makeGoTree(b, files)

	for _, jobs := range []int{1, 2, 4, 8, 16} {
		b.Run(fmt.Sprintf("jobs=%d", jobs), func(b *testing.B) {
			output := filepath.Join(b.TempDir(), "graph.json")
			opts := graphed.BuildOptions{
				Root:   root,
				Output: output,
				Format: "json",
				Jobs:   jobs,
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := graphed.Build(opts); err != nil {
					b.Fatalf("Build failed: %v", err)
				}
			}
		})
	}
}
