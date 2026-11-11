// Package hotspot runs the kiosk's own Wi-Fi network (Windows Mobile
// Hotspot) while a play session is open. Phones that join it reach the
// kiosk directly, so their pad link is one hop instead of a trip through
// the venue's router or the server fallback. Starting and stopping need no
// admin rights; the network shares the kiosk's internet connection, so the
// pad page still loads from the server.
package hotspot

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
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

// Original is the kiosk's own hotspot configuration from before the first
// session changed it.
type Original struct {
	SSID       string `json:"ssid"`
	Passphrase string `json:"passphrase"`
	Band       string `json:"band"`
}

// parseStart reads the start script's output: an optional "orig {json}"
// line, then "on <band>".
func parseStart(out string) (string, *Original) {
	var band string
	var orig *Original
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(line, "orig "); ok {
			var o Original
			if json.Unmarshal([]byte(rest), &o) == nil && o.SSID != "" {
				orig = &o
			}
		} else if rest, ok := strings.CutPrefix(line, "on "); ok {
			band = rest
		}
	}
	return band, orig
}

// SaveOriginal keeps orig at path unless an earlier save is there: the
// first one is the kiosk's real configuration. The file holds a Wi-Fi
// password, so only the kiosk user can read it.
func SaveOriginal(path string, orig *Original) error {
	if orig == nil {
		return nil
	}
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	b, err := json.Marshal(orig)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

// LoadOriginal reads a saved configuration; nil when there is none.
func LoadOriginal(path string) (*Original, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var o Original
	if err := json.Unmarshal(b, &o); err != nil || o.SSID == "" {
		return nil, errors.New("hotspot: unreadable saved configuration")
	}
	return &o, nil
}
