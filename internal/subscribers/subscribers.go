// Package subscribers delivers incident-update notifications. Email goes
// through a user-supplied SMTP relay (Outpage never operates one);
// webhooks are POSTed with an HMAC-SHA256 signature the receiver can
// verify. Delivery is best-effort and recorded per subscriber — an SMTP
// or webhook failure never rolls back the incident timeline, which is
// the source of truth. There is no SMS path and never will be.
package subscribers

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/smtp"
	"strings"
	"time"

	"github.com/jonathanngiroux-star/outpage/internal/store"
)

// SMTPConfig is the user-supplied relay. An empty Host means "not
// configured": email subscribers are skipped and recorded, never guessed.
type SMTPConfig struct {
	Host string
	Port int
	From string
	User string // optional AUTH
	Pass string // optional AUTH
}

// Result summarizes one notify run.
type Result struct {
	Sent    int `json:"sent"`
	Failed  int `json:"failed"`
	Skipped int `json:"skipped"`
}

// webhookPayload is the signed JSON body every webhook subscriber receives.
type webhookPayload struct {
	Event        string `json:"event"`
	IncidentID   int64  `json:"incident_id"`
	IncidentName string `json:"incident_name"`
	Impact       string `json:"impact"`
	Status       string `json:"status"`
	Body         string `json:"body"`
	UpdatedAt    string `json:"updated_at"`
}

// Sign produces the X-Outpage-Signature header value for a body.
func Sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// Verify checks an X-Outpage-Signature header against a body.
func Verify(secret string, body []byte, header string) error {
	const prefix = "sha256="
	if !strings.HasPrefix(header, prefix) {
		return errors.New("signature header missing sha256= prefix")
	}
	sig, err := hex.DecodeString(strings.TrimPrefix(header, prefix))
	if err != nil {
		return errors.New("signature is not valid hex")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return errors.New("signature mismatch")
	}
	return nil
}

// Notify delivers one incident update to every subscriber. Each attempt is
// recorded (sent / failed / skipped) with its detail; errors never
// propagate to the caller's transaction — the timeline already committed.
func Notify(s *store.Store, inc store.Incident, upd store.Update, cfg SMTPConfig) Result {
	var res Result
	subs, err := s.ListSubscribers()
	if err != nil {
		return res
	}
	for _, sub := range subs {
		switch sub.Kind {
		case "email":
			if cfg.Host == "" || cfg.Port == 0 {
				res.Skipped++
				s.RecordNotification(inc.ID, sub.ID, "email", "skipped",
					"no SMTP relay configured (OUTPAGE_SMTP_HOST/PORT)")
				continue
			}
			if err := sendEmail(cfg, sub.Address, inc, upd); err != nil {
				res.Failed++
				s.RecordNotification(inc.ID, sub.ID, "email", "failed", err.Error())
				continue
			}
			res.Sent++
			s.RecordNotification(inc.ID, sub.ID, "email", "sent", "")
		case "webhook":
			if err := postWebhook(sub, inc, upd); err != nil {
				res.Failed++
				s.RecordNotification(inc.ID, sub.ID, "webhook", "failed", err.Error())
				continue
			}
			res.Sent++
			s.RecordNotification(inc.ID, sub.ID, "webhook", "sent", "")
		}
	}
	return res
}

// sendEmail delivers one plain-text update via the user's relay.
func sendEmail(cfg SMTPConfig, to string, inc store.Incident, upd store.Update) error {
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	subject := fmt.Sprintf("[status] %s: %s — %s", inc.Name, upd.Status, inc.Status)
	msg := bytes.NewBuffer(nil)
	msg.WriteString("To: " + to + "\r\n")
	msg.WriteString("From: " + cfg.From + "\r\n")
	msg.WriteString("Subject: " + subject + "\r\n")
	msg.WriteString("Date: " + time.Now().Format(time.RFC1123Z) + "\r\n")
	msg.WriteString("Content-Type: text/plain; charset=UTF-8\r\n\r\n")
	msg.WriteString(inc.Name + " — " + strings.ToUpper(upd.Status[:1]) + upd.Status[1:] + "\r\n\r\n")
	msg.WriteString(upd.Body + "\r\n\r\n--\r\nSent by Outpage (self-hosted, MIT). Unsubscribe: manage the subscriber list on the operator side.\r\n")

	var auth smtp.Auth
	if cfg.User != "" {
		auth = smtp.PlainAuth("", cfg.User, cfg.Pass, cfg.Host)
	}
	return smtp.SendMail(addr, auth, cfg.From, []string{to}, msg.Bytes())
}

// postWebhook signs and POSTs the update JSON. 2xx = sent; anything else
// (or a transport error) is a recorded failure.
func postWebhook(sub store.Subscriber, inc store.Incident, upd store.Update) error {
	payload, err := json.Marshal(webhookPayload{
		Event:        "incident_update",
		IncidentID:   inc.ID,
		IncidentName: inc.Name,
		Impact:       inc.Impact,
		Status:       upd.Status,
		Body:         upd.Body,
		UpdatedAt:    time.Unix(0, upd.CreatedNS).UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, sub.Address, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Outpage-Signature", Sign(sub.Secret, payload))
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("receiver answered %s", resp.Status)
	}
	return nil
}
