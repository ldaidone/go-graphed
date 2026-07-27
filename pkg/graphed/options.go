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
}
