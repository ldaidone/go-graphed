package graphed

// BuildOptions holds every knob the public Build function accepts.
type BuildOptions struct {
	// Root is the directory to scan.  May use Go-style "./..." paths;
	// cleanSourcePath normalises them before Build is called.
	Root string

	// Output is the target file path for the exported graph.
	Output string

	// Format selects the exporter (currently only "json" is supported).
	Format string

	// Exclude carries extra gitignore-style patterns layered on top of any
	// discovered .gitignore files (e.g. "vendor/", "*.gen.go"). Patterns are
	// matched relative to Root; use a leading "!" to re-include.
	Exclude []string

	// IgnoreFiles are paths to additional gitignore-format files, layered after
	// discovered .gitignore files and matched relative to Root.
	IgnoreFiles []string

	// NoGitIgnore disables discovery and application of .gitignore files.
	NoGitIgnore bool

	// Jobs controls how many parser goroutines run in parallel. Zero or a
	// negative value selects runtime.NumCPU().
	Jobs int

	// ModelPath is the path to a local gte embedding model file.
	// When empty, the semantic embedding stage is skipped entirely.
	ModelPath string

	// Dimensions is the embedding vector width of ModelPath (e.g., 384).
	Dimensions int

	// DBRoot is the base config directory for the BadgerDB vector store.
	// When empty, the default under the user's home directory is used.
	DBRoot string
}
