package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestNoCallHome is the telemetry gate: the binary must never dial out
// except to (a) the user's own SMTP relay and (b) user-configured webhook
// URLs — both inside internal/subscribers — and the inbound listener in
// internal/serve. No telemetry, no update checks, no "phone home" of any
// kind, ever.
func TestNoCallHome(t *testing.T) {
	root := "."
	for {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			break
		}
		root = filepath.Join(root, "..")
	}
	// Packages allowed to import net/http at all.
	allowedDirs := map[string]bool{
		"internal/serve":       true, // inbound public page
		"internal/subscribers": true, // delivery to user-configured targets
		"cmd/outpage":          true, // serve wiring / tests
	}
	bannedCalls := []string{
		"http.Get(", "http.Post(", "http.PostForm(", "http.Head(",
		"http.DefaultClient", "http.DefaultTransport",
	}
	var violations []string
	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			if d != nil && d.IsDir() && (d.Name() == ".git" || d.Name() == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		dir := filepath.Dir(rel)
		b, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		src := string(b)
		if strings.Contains(src, "\"net/http\"") && !allowedDirs[dir] {
			violations = append(violations, rel+": imports net/http outside serve/subscribers")
		}
		for _, call := range bannedCalls {
			if strings.Contains(src, call) {
				violations = append(violations, rel+": contains "+call)
			}
		}
		// Hardcoded remote hosts are a smell even in allowed packages:
		// match real URL tokens (scheme + host-like run) and allow only
		// example/test/loopback hosts and format directives. Prose like
		// "webhooks must be https:// URLs" is not a dial target.
		urlRe := regexp.MustCompile(`https?://[A-Za-z0-9.-]+[A-Za-z0-9.-]*`)
		for _, ln := range strings.Split(src, "\n") {
			trimmed := strings.TrimSpace(ln)
			if strings.HasPrefix(trimmed, "//") {
				continue // comment
			}
			for _, u := range urlRe.FindAllString(ln, -1) {
				host := strings.TrimPrefix(strings.TrimPrefix(u, "https://"), "http://")
				if strings.Contains(host, "example") ||
					strings.HasPrefix(host, "127.0.0.1") ||
					strings.HasPrefix(host, "localhost") {
					continue
				}
				violations = append(violations, rel+": hardcoded remote URL "+u)
			}
		}
		return nil
	})
	if len(violations) > 0 {
		t.Fatalf("call-home violations:\n%s", strings.Join(violations, "\n"))
	}
}

func firstSnippet(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
