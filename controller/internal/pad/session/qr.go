package session

import (
	"fmt"
	"net/url"
)

// JoinURL is the address the kiosk's QR code opens: the session's
// join_base_url (…/play/<CODE>), checked to be absolute. The code alone is
// enough to join, so the QR carries no token and never changes during a
// session.
func JoinURL(joinBaseURL string) (string, error) {
	u, err := url.Parse(joinBaseURL)
	if err != nil {
		return "", fmt.Errorf("pad session: join_base_url: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("pad session: join_base_url %q is not absolute", joinBaseURL)
	}
	return u.String(), nil
}
