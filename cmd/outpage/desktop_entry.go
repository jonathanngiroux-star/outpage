//go:build desktop

package main

// desktop_entry.go: `outpage desktop` in the desktop build
// (-tags desktop,production,webkit2_41 + CGO on Linux). The real
// implementation is desktop_wails.go; this file keeps the entry in one
// place per build flavor (the series pattern).

import (
	"fmt"
	"io"

	"github.com/jonathanngiroux-star/outpage/internal/store"
)

func launchDesktop(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("desktop", stderr)
	db := fs.String("db", envDefault("OUTPAGE_DB", "outpage.db"), "path to the SQLite database")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	st, err := store.Open(*db)
	if err != nil {
		fmt.Fprintf(stderr, "outpage desktop: %v\n", err)
		return 1
	}
	defer st.Close()
	return runDesktopWails(st, stdout, stderr)
}
