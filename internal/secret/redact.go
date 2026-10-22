package secret

import (
	"bytes"
	"io"
)

const mask = "[redacted]"

// Redactor wraps a writer and replaces every occurrence of the secret with a
// mask, including one split across writes. Call Flush when done.
type Redactor struct {
	w      io.Writer
	secret []byte
	carry  []byte
}

// NewRedactor wraps w and passes writes through unchanged when secret is empty.
func NewRedactor(w io.Writer, secret string) *Redactor {
	return &Redactor{w: w, secret: []byte(secret)}
}

func (r *Redactor) Write(p []byte) (int, error) {
	if len(r.secret) == 0 {
		return r.w.Write(p)
	}
	buf := append(r.carry, p...)
	buf = bytes.ReplaceAll(buf, r.secret, []byte(mask))
	// Hold back a tail that could start a secret split across writes.
	keep := 0
	for n := min(len(r.secret)-1, len(buf)); n > 0; n-- {
		if bytes.HasPrefix(r.secret, buf[len(buf)-n:]) {
			keep = n
			break
		}
	}
	r.carry = append([]byte(nil), buf[len(buf)-keep:]...)
	if _, err := r.w.Write(buf[:len(buf)-keep]); err != nil {
		return 0, err
	}
	return len(p), nil
}

// Flush writes any held-back bytes.
func (r *Redactor) Flush() error {
	if len(r.carry) == 0 {
		return nil
	}
	_, err := r.w.Write(r.carry)
	r.carry = nil
	return err
}
