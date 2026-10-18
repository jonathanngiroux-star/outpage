// Package serve publishes the public status page over HTTP. Two routes
// exist: "/" (the page) and "/donate" — there is no admin HTTP surface
// and no auth surface to get wrong. The page is re-rendered from the
// store on every request, so a running server always reflects the CLI.
package serve

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/jonathanngiroux-star/outpage/internal/page"
	"github.com/jonathanngiroux-star/outpage/internal/store"
)

// Server is the HTTP publisher.
type Server struct {
	srv *http.Server
	ln  net.Listener
	st  *store.Store
}

// New binds addr and returns the server (not yet serving). Rendering is
// store-backed per request; nothing is cached across requests.
func New(st *store.Store, addr string) (*Server, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listen %s: %w", addr, err)
	}
	mux := http.NewServeMux()
	s := &Server{
		st: st,
		ln: ln,
		srv: &http.Server{
			Handler:           mux,
			ReadHeaderTimeout: 10 * time.Second,
		},
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		html, err := page.Render(st)
		if err != nil {
			http.Error(w, "render error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, html)
	})
	mux.HandleFunc("/donate", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, page.RenderDonate())
	})
	return s, nil
}

// Addr returns the bound address (useful when :0 was requested).
func (s *Server) Addr() net.Addr { return s.ln.Addr() }

// Start begins serving on the bound listener.
func (s *Server) Start() error {
	err := s.srv.Serve(s.ln)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.srv.Shutdown(ctx)
}
