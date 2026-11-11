package session

import "strings"

// WiFi is the kiosk's own hotspot while a session runs. Phones on it reach
// the kiosk in one hop, the lowest-latency path; the overlay shows QR (a
// standard WIFI: join code) next to the pad QR, and the pad names the SSID
// when a phone is stuck on the slow server fallback.
type WiFi struct {
	SSID     string `json:"ssid"`
	Password string `json:"password"`
	QR       string `json:"qr"`
}

// NewWiFi builds the view for a WPA2 network.
func NewWiFi(ssid, password string) *WiFi {
	return &WiFi{SSID: ssid, Password: password, QR: "WIFI:T:WPA;S:" + wifiEscape(ssid) + ";P:" + wifiEscape(password) + ";;"}
}

// wifiEscape escapes the characters the WIFI: QR format reserves.
func wifiEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `;`, `\;`, `,`, `\,`, `:`, `\:`, `"`, `\"`).Replace(s)
}

// SetWiFi shows (or, with nil, hides) the hotspot on the session view.
func (m *Manager) SetWiFi(w *WiFi) {
	m.mu.Lock()
	m.wifi = w
	m.mu.Unlock()
	m.emit()
}

func (m *Manager) wifiSSIDLocked() string {
	if m.wifi == nil {
		return ""
	}
	return m.wifi.SSID
}
