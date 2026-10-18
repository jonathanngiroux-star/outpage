# Desktop GUI setup (Wails v2)

The desktop GUI is the same `outpage` binary compiled with the Wails v2
webview backend. The default build stays cgo-free (Docker, scratch, CI);
the desktop build adds the webview.

## Why build tags

- `desktop` — compiles `desktop_wails.go`/`desktop_entry.go` (the binding
  layer + window entry). Without it, `outpage desktop` prints build
  instructions instead of crashing.
- `production` — Wails' own mandatory runtime tag; without it the app
  builds but exits immediately with "Wails applications will not build
  without the correct build tags."
- `webkit2_41` — selects the webkit2gtk-4.1 pkg-config name. Modern
  distros ship 4.1 only (4.0 is EOL); without this tag the cgo build
  fails at pkg-config.

## Build (Linux)

System dependencies: `gtk3` and `webkit2gtk-4.1` dev packages
(Debian/Ubuntu: `libgtk-3-dev libwebkit2gtk-4.1-dev`).

```bash
CGO_ENABLED=1 go build -tags desktop,production,webkit2_41 \
  -o outpage-desktop ./cmd/outpage
./outpage-desktop desktop          # opens the window
```

## Development mode (hot reload)

`wails dev` compiles with its own `dev` tag (user -tags are not merged)
and expects the main package at the project root, so run it from inside
the package directory:

```bash
wails dev -tags "desktop,webkit2_41"    # from cmd/outpage/
```

(Install the Wails CLI: `go install github.com/wailsapp/wails/v2/cmd/wails@latest`.)

## What the GUI covers

Component editor, incident composer with timeline viewer, maintenance
scheduler, subscriber list (addresses masked), the Statuspage dry-run
viewer, the public-page preview (exact serve HTML), and the donate view
with copy buttons. First launch runs the setup wizard; Help reopens it.
Every mutation goes through the same store methods as the CLI — the GUI
is never a side door around the data model.

## Tests

Binding-layer tests run under the desktop tag:

```bash
CGO_ENABLED=1 go test -tags desktop ./cmd/outpage/ -count=1
```
