//go:build !desktop

package main

// desktop_default.go: `outpage desktop` in the default (cgo-free) build.
// The Wails desktop GUI needs cgo on Linux (webkit2gtk); the default
// binary deliberately omits it so Docker, scratch containers, and CI all
// stay pure-Go. This stub explains the situation instead of crashing.

import (
	"fmt"
	"io"
)

func launchDesktop(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("desktop", stderr)
	db := fs.String("db", envDefault("OUTPAGE_DB", "outpage.db"), "path to the SQLite database")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	_ = db
	fmt.Fprintln(stderr, "outpage: this binary was built without the desktop GUI.")
	fmt.Fprintln(stderr, "")
	fmt.Fprintln(stderr, "The Wails desktop GUI needs the webview backend; the default outpage binary is")
	fmt.Fprintln(stderr, "cgo-free so it runs everywhere (Docker, scratch, CI). Two ways to get the GUI:")
	fmt.Fprintln(stderr, "")
	fmt.Fprintln(stderr, "  1. Build it yourself (Linux):")
	fmt.Fprintln(stderr, "       CGO_ENABLED=1 go build -tags desktop,production,webkit2_41 -o outpage-desktop ./cmd/outpage")
	fmt.Fprintln(stderr, "       ./outpage-desktop desktop")
	fmt.Fprintln(stderr, "")
	fmt.Fprintln(stderr, "  2. Development mode (hot reload):")
	fmt.Fprintln(stderr, "       cd cmd/outpage && wails dev -tags \"desktop,webkit2_41\"")
	fmt.Fprintln(stderr, "")
	fmt.Fprintln(stderr, "System dependencies on Linux: gtk3 + webkit2gtk-4.1 (docs/gui-setup.md).")
	return 1
}
