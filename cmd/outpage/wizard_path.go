package main

// wizard_path.go: locating the committed fixture from the wizard.
// The default --input path is repo-root relative; the binary may run from
// anywhere, so walk up from the executable / cwd.

import (
	"os"
	"path/filepath"
)

// repoRootString walks up from cwd to the nearest go.mod (dev runs); if
// none is found, return "." so the CLI's repo-relative default still
// applies.
func repoRootString() string {
	dir, _ := os.Getwd()
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	return "."
}
