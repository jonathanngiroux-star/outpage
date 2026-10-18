package subscribers

import "testing"

// FuzzSignVerify exercises the signing pair at its contract: for any
// secret and body, Sign→Verify roundtrips; a corrupted signature must
// never verify (no panic, no false accept).
func FuzzSignVerify(f *testing.F) {
	f.Add("k", []byte("hello"))
	f.Add("", []byte(""))
	f.Add("multi\nline\nsecret", []byte{0, 1, 2, 255})

	f.Fuzz(func(t *testing.T, secret string, body []byte) {
		sig := Sign(secret, body)
		if err := Verify(secret, body, sig); err != nil {
			t.Fatalf("roundtrip failed: %v", err)
		}
		// Wrong secret must fail.
		if err := Verify(secret+"x", body, sig); err == nil {
			t.Fatal("wrong secret verified")
		}
		// Tampered body must fail.
		if len(body) > 0 {
			tampered := append([]byte{}, body...)
			tampered[0] ^= 0xFF
			if err := Verify(secret, tampered, sig); err == nil {
				t.Fatal("tampered body verified")
			}
		}
	})
}
