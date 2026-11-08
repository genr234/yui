package session

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	qrWindowSec   = 30
	qrTokenLength = 8
)

// QRToken is the 8-character presence token for a 30-second window.
// qrKeyB64 is the base64-encoded key from the kiosk session API (the HMAC key
// is the decoded bytes). unix is seconds since the epoch; the counter is
// floor(unix/30) as an 8-byte big-endian integer. The digest is RFC 4648
// base32, uppercase, no padding, first 8 characters.
func QRToken(qrKeyB64 string, unix int64) (string, error) {
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(qrKeyB64))
	if err != nil {
		return "", fmt.Errorf("pad session: qr_key: %w", err)
	}
	if len(key) == 0 {
		return "", fmt.Errorf("pad session: qr_key is empty")
	}
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(unix/qrWindowSec))
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(counter[:])
	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(mac.Sum(nil))
	if len(encoded) < qrTokenLength {
		return "", fmt.Errorf("pad session: qr token encoding is short")
	}
	return encoded[:qrTokenLength], nil
}

// JoinURL is joinBaseURL plus the current token as query t.
func JoinURL(joinBaseURL, token string) (string, error) {
	u, err := url.Parse(joinBaseURL)
	if err != nil {
		return "", fmt.Errorf("pad session: join_base_url: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("pad session: join_base_url %q is not absolute", joinBaseURL)
	}
	q := u.Query()
	q.Set("t", token)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// nextQRDeadline is the start of the next 30-second window.
func nextQRDeadline(now time.Time) time.Time {
	unix := now.Unix()
	next := (unix/qrWindowSec + 1) * qrWindowSec
	return time.Unix(next, 0).UTC()
}
