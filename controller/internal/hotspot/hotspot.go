// Package hotspot runs the kiosk's own Wi-Fi network (Windows Mobile
// Hotspot) while a play session is open. Phones that join it reach the
// kiosk directly, so their pad link is one hop instead of a trip through
// the venue's router or the server fallback. Starting and stopping need no
// admin rights; the network shares the kiosk's internet connection, so the
// pad page still loads from the server.
package hotspot

import (
	"crypto/rand"
	"strings"
)

// SSIDPrefix marks networks this package made: Stop only turns off a
// hotspot whose name starts with it, never one someone else set up.
const SSIDPrefix = "Yui "

// SSID is the network name for a session code, e.g. "Yui KKTC". Plain
// ASCII: QR libraries and some phones disagree on other encodings.
func SSID(code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" {
		return SSIDPrefix + "Play"
	}
	return SSIDPrefix + code
}

// Password returns a fresh WPA2 passphrase. Players never type it (the
// overlay shows a Wi-Fi QR), so it can be long; it skips look-alike letters
// for the rare phone that can't scan.
func Password() string {
	const alphabet = "abcdefghjkmnpqrstuvwxyz23456789"
	b := make([]byte, 10)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b)
}
