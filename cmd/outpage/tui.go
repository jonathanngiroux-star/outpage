package main

// tui.go: `outpage tui` — the terminal UI front door. Pure Go (bubbletea
// + tcell-free), ships in the default cgo-free binary. Non-TTY contexts
// (tests, pipes, CI) print a hint and exit — never a hang.

import (
	"fmt"
	"io"
	"os"

	"github.com/jonathanngiroux-star/outpage/internal/store"
)

// osFile aliases os.File for the isTerminal test hook.
type osFile = os.File

// isTerminal reports whether f is an interactive terminal (swappable in
// tests).
var isTerminal = func(f *os.File) bool {
	st, err := f.Stat()
	if err != nil {
		return false
	}
	return st.Mode()&os.ModeCharDevice != 0
}

// launchTUI opens the TUI against the shared store. Non-TTY → hint + 0.
func launchTUI(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("tui", stderr)
	db := fs.String("db", envDefault("OUTPAGE_DB", "outpage.db"), "path to the SQLite database (same file as the GUI and CLI)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "outpage: unexpected argument %q — flags only, not both\n", fs.Arg(0))
		return 2
	}
	st, err := store.Open(*db)
	if err != nil {
		fmt.Fprintf(stderr, "outpage tui: %v\n", err)
		return 1
	}
	defer st.Close()
	if !isTerminal(os.Stdout) {
		fmt.Fprintln(stdout, "outpage tui: this command opens an interactive terminal UI.")
		fmt.Fprintln(stdout, "Run it from a terminal (same SQLite file as the GUI and CLI).")
		fmt.Fprintln(stdout, "Non-interactive contexts: use the CLI commands (component, incident, migrate…).")
		return 0
	}
	return runTea(st)
}
