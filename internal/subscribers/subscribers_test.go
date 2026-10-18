package subscribers

import (
	"bufio"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jonathanngiroux-star/outpage/internal/store"
)

func seedStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "subs.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// fakeSMTP is a minimal in-process SMTP server: enough dialogue for
// net/smtp.SendMail (EHLO, MAIL, RCPT, DATA, QUIT) plus capture.
type fakeSMTP struct {
	ln       net.Listener
	messages chan string
}

func newFakeSMTP(t *testing.T) *fakeSMTP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	f := &fakeSMTP{ln: ln, messages: make(chan string, 8)}
	go f.serve()
	t.Cleanup(func() { ln.Close() })
	return f
}

func (f *fakeSMTP) port() int { return f.ln.Addr().(*net.TCPAddr).Port }

func (f *fakeSMTP) serve() {
	for {
		conn, err := f.ln.Accept()
		if err != nil {
			return
		}
		go f.handle(conn)
	}
}

func (f *fakeSMTP) handle(conn net.Conn) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	w := conn
	write := func(s string) { w.Write([]byte(s + "\r\n")) }
	write("220 outpage-test ESMTP")
	var msg strings.Builder
	inData := false
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		if inData {
			if strings.HasPrefix(line, ".\r\n") || line == ".\n" {
				inData = false
				write("250 OK")
				select {
				case f.messages <- msg.String():
				default:
				}
				continue
			}
			msg.WriteString(line)
			continue
		}
		switch {
		case strings.HasPrefix(strings.ToUpper(line), "EHLO"), strings.HasPrefix(strings.ToUpper(line), "HELO"):
			write("250-outpage-test")
			write("250 OK")
		case strings.HasPrefix(strings.ToUpper(line), "MAIL"), strings.HasPrefix(strings.ToUpper(line), "RCPT"):
			write("250 OK")
		case strings.HasPrefix(strings.ToUpper(line), "DATA"):
			write("354 go")
			inData = true
		case strings.HasPrefix(strings.ToUpper(line), "QUIT"):
			write("221 bye")
			return
		default:
			write("250 OK")
		}
	}
}

// Email goes through the user-supplied SMTP relay and lands as a recorded
// sent notification.
func TestNotifyEmailSendsViaUserSMTP(t *testing.T) {
	s := seedStore(t)
	f := newFakeSMTP(t)
	inc, err := s.OpenIncident("Elevated errors", "minor", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddSubscriber("email", "ops@example.net", ""); err != nil {
		t.Fatal(err)
	}
	upd, err := s.AddUpdate(inc.ID, "identified", "Found it.")
	if err != nil {
		t.Fatal(err)
	}
	cfg := SMTPConfig{Host: "127.0.0.1", Port: f.port(), From: "status@example.net"}
	res := Notify(s, inc, upd, cfg)
	if res.Sent != 1 || res.Failed != 0 {
		t.Fatalf("result = %+v, want 1 sent", res)
	}
	select {
	case msg := <-f.messages:
		if !strings.Contains(msg, "To: ops@example.net") {
			t.Fatalf("missing To header:\n%s", msg)
		}
		if !strings.Contains(msg, "Elevated errors") || !strings.Contains(msg, "Found it.") {
			t.Fatalf("message body incomplete:\n%s", msg)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no SMTP message arrived")
	}
	logs, err := s.Notifications(inc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 || logs[0].Status != "sent" || logs[0].Channel != "email" {
		t.Fatalf("notification log: %+v", logs)
	}
}

// SMTP failure is recorded, never propagated — the timeline update is the
// source of truth and must never roll back.
func TestSMTPFailureDoesNotRollbackIncident(t *testing.T) {
	s := seedStore(t)
	inc, err := s.OpenIncident("Errors", "minor", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddSubscriber("email", "ops@example.net", ""); err != nil {
		t.Fatal(err)
	}
	upd, err := s.AddUpdate(inc.ID, "identified", "Found it.")
	if err != nil {
		t.Fatal(err)
	}
	// Point at a port with nothing listening.
	res := Notify(s, inc, upd, SMTPConfig{Host: "127.0.0.1", Port: 1, From: "status@example.net"})
	if res.Sent != 0 || res.Failed != 1 {
		t.Fatalf("result = %+v, want 1 failed", res)
	}
	got, err := s.Incident(inc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "identified" {
		t.Fatalf("incident status rolled back: %q", got.Status)
	}
	logs, _ := s.Notifications(inc.ID)
	if len(logs) != 1 || logs[0].Status != "failed" {
		t.Fatalf("failure not recorded: %+v", logs)
	}
}

// Without SMTP config, email subscribers are skipped (recorded), not sent.
func TestNotifySkipsEmailWithoutSMTPConfig(t *testing.T) {
	s := seedStore(t)
	inc, _ := s.OpenIncident("Errors", "minor", nil)
	if _, err := s.AddSubscriber("email", "ops@example.net", ""); err != nil {
		t.Fatal(err)
	}
	upd, err := s.AddUpdate(inc.ID, "identified", "Found it.")
	if err != nil {
		t.Fatal(err)
	}
	res := Notify(s, inc, upd, SMTPConfig{})
	if res.Skipped != 1 || res.Sent != 0 {
		t.Fatalf("result = %+v, want 1 skipped", res)
	}
	logs, _ := s.Notifications(inc.ID)
	if len(logs) != 1 || logs[0].Status != "skipped" {
		t.Fatalf("skip not recorded: %+v", logs)
	}
}

// Webhooks receive a signed JSON payload the receiver can verify.
func TestNotifyWebhookSignedPayload(t *testing.T) {
	s := seedStore(t)
	inc, _ := s.OpenIncident("Elevated errors", "major", nil)
	if _, err := s.AddSubscriber("webhook", "https://hooks.example.net/x", "s3cr3t"); err != nil {
		t.Fatal(err)
	}
	upd, err := s.AddUpdate(inc.ID, "identified", "Found it.")
	if err != nil {
		t.Fatal(err)
	}

	type capture struct {
		Header string
		Body   []byte
	}
	got := make(chan capture, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got <- capture{Header: r.Header.Get("X-Outpage-Signature"), Body: b}
	}))
	defer srv.Close()
	// The store requires https for webhooks; swap in the loopback test URL
	// directly (validation is covered in store tests).
	subs, _ := s.ListSubscribers()
	if err := s.SetSubscriberAddress(subs[0].ID, srv.URL); err != nil {
		t.Fatalf("SetSubscriberAddress: %v", err)
	}

	res := Notify(s, inc, upd, SMTPConfig{})
	if res.Sent != 1 || res.Failed != 0 {
		t.Fatalf("result = %+v, want 1 sent webhook", res)
	}
	select {
	case c := <-got:
		if !strings.HasPrefix(c.Header, "sha256=") {
			t.Fatalf("signature header %q, want sha256= prefix", c.Header)
		}
		if err := Verify("s3cr3t", c.Body, c.Header); err != nil {
			t.Fatalf("signature does not verify: %v", err)
		}
		if err := Verify("wrong-secret", c.Body, c.Header); err == nil {
			t.Fatal("wrong secret must not verify")
		}
		var payload map[string]any
		if err := json.Unmarshal(c.Body, &payload); err != nil {
			t.Fatalf("payload not JSON: %v", err)
		}
		if payload["event"] != "incident_update" || payload["incident_name"] != "Elevated errors" ||
			payload["status"] != "identified" || payload["body"] != "Found it." {
			t.Fatalf("payload incomplete: %+v", payload)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no webhook arrived")
	}
}

func TestVerifyRejectsTampering(t *testing.T) {
	sig := Sign("k", []byte("hello"))
	if err := Verify("k", []byte("hello"), sig); err != nil {
		t.Fatalf("valid: %v", err)
	}
	if err := Verify("k", []byte("hello!"), sig); err == nil {
		t.Fatal("tampered payload must fail")
	}
	if err := Verify("k", []byte("hello"), "sha256=deadbeef"); err == nil {
		t.Fatal("wrong signature must fail")
	}
	if err := Verify("k", []byte("hello"), "garbage"); err == nil {
		t.Fatal("malformed header must fail")
	}
}

// A dead webhook URL is recorded failed; the rest of the notify run
// continues.
func TestWebhookFailureRecorded(t *testing.T) {
	s := seedStore(t)
	inc, _ := s.OpenIncident("Errors", "minor", nil)
	if _, err := s.AddSubscriber("webhook", "https://hooks.example.net/x", "k"); err != nil {
		t.Fatal(err)
	}
	upd, _ := s.AddUpdate(inc.ID, "identified", "Found it.")
	subs, _ := s.ListSubscribers()
	if err := s.SetSubscriberAddress(subs[0].ID, "http://127.0.0.1:1/nope"); err != nil {
		t.Fatal(err)
	}
	res := Notify(s, inc, upd, SMTPConfig{})
	if res.Failed != 1 || res.Sent != 0 {
		t.Fatalf("result = %+v, want 1 failed webhook", res)
	}
	logs, _ := s.Notifications(inc.ID)
	if len(logs) != 1 || logs[0].Status != "failed" {
		t.Fatalf("failure not recorded: %+v", logs)
	}
}
