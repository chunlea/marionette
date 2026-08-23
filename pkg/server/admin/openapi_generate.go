//go:build ignore

// Command openapi-gen writes the generated admin OpenAPI document to
// pkg/server/admin/openapi.yaml, where it is embedded and served at
// /openapi.yaml.
//
// Run it with `make openapi`.
package main

import (
	"fmt"
	"os"

	"github.com/chunlea/marionette/pkg/server/admin"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: go run pkg/server/admin/openapi_generate.go <output-file>")
		os.Exit(2)
	}

	document, err := admin.BuildOpenAPIDocument()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: build admin openapi document: %v\n", err)
		os.Exit(1)
	}

	if err := os.WriteFile(os.Args[1], document, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "error: write %s: %v\n", os.Args[1], err)
		os.Exit(1)
	}
	fmt.Printf("==> wrote %s\n", os.Args[1])
}
