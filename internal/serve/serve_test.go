package serve

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jonathanngiroux-star/outpage/internal/store"
)

func testStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "serve.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// The server serves the public page and /donate on the bound address and
// shuts down cleanly.
func TestServePageAndDonate(t *testing.T) {
	st := testStore(t)
	if _, err := st.AddComponent("API", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := st.OpenIncident("Elevated errors", "minor", []string{"API"}); err != nil {
		t.Fatal(err)
	}
	srv, err := New(st, "127.0.0.1:0")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	go srv.Start()
	t.Cleanup(func() { srv.Shutdown(context.Background()) })

	base := "http://" + srv.Addr().String()
	client := &http.Client{Timeout: 3 * time.Second}

	resp, err := client.Get(base + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "API") || !strings.Contains(string(body), "Elevated errors") {
		t.Fatalf("page missing component/incident:\n%s", firstN(string(body), 300))
	}
	if !strings.Contains(string(body), "0x85ee7E71f762d772599cbF1EC20E651B30657521") {
		t.Fatal("footer donate address missing from page")
	}

	resp, err = client.Get(base + "/donate")
	if err != nil {
		t.Fatalf("GET /donate: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "bc1qxe2zx5tv3hdreaej6s2x4p7han85uey828rrhg") {
		t.Fatal("/donate missing BTC address")
	}

	// No admin surface: anything else is 404.
	resp, err = client.Get(base + "/admin")
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("/admin = %d, want 404", resp.StatusCode)
	}
}

// A live page must reflect CLI-side writes made after boot — the store is
// the only state.
func TestServeReflectsLaterWrites(t *testing.T) {
	st := testStore(t)
	srv, err := New(st, "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go srv.Start()
	t.Cleanup(func() { srv.Shutdown(context.Background()) })

	base := "http://" + srv.Addr().String()
	client := &http.Client{Timeout: 3 * time.Second}
	get := func() string {
		resp, err := client.Get(base + "/")
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return string(b)
	}
	if strings.Contains(get(), "Late Component") {
		t.Fatal("component leaked before creation")
	}
	if _, err := st.AddComponent("Late Component", ""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(get(), "Late Component") {
		t.Fatal("write after boot not reflected on the page")
	}
}

func TestNewBadAddr(t *testing.T) {
	st := testStore(t)
	if _, err := New(st, "bogus-addr"); err == nil {
		t.Fatal("New(bogus) must fail")
	}
}

func firstN(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
