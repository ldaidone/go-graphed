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

	// Exclude is reserved for future glob-based path exclusions.
	Exclude []string

	// Jobs controls how many parser goroutines run in parallel
	// (not yet wired -- reserved for concurrent parsing).
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
