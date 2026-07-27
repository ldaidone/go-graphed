// Package main is the CLI entry point for the kg tool.
// It delegates entirely to the cli package so that command
// wiring and business logic stay testable and separated.
package main

import (
	"log"

	"github.com/ldaidone/go-graphed/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		// Exit with a non-zero code so callers (scripts, CI) can detect failures.
		log.Fatal(err)
	}
}
