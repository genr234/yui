package blocker

import (
	"net/url"
	"strings"
	"sync"
)

type Manager struct {
	mu             sync.RWMutex
	origins        map[string]map[string]bool
	enabledOrigins map[string]int
	engine         *Engine
}

var DefaultManager = NewManager(NewEngine(DefaultRules()))

func NewManager(engine *Engine) *Manager {
	return &Manager{
		origins:        make(map[string]map[string]bool),
		enabledOrigins: make(map[string]int),
		engine:         engine,
	}
}

func (m *Manager) SetEmbed(appID string, origin string, enabled bool) bool {
	normalized, ok := normalizeOrigin(origin)
	if !ok || appID == "" {
		return false
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if enabled {
		if m.origins[appID] == nil {
			m.origins[appID] = make(map[string]bool)
		}
		if !m.origins[appID][normalized] {
			m.enabledOrigins[normalized]++
		}
		m.origins[appID][normalized] = true
		return true
	}

	if m.origins[appID][normalized] {
		m.enabledOrigins[normalized]--
		if m.enabledOrigins[normalized] <= 0 {
			delete(m.enabledOrigins, normalized)
		}
	}
	delete(m.origins[appID], normalized)
	if len(m.origins[appID]) == 0 {
		delete(m.origins, appID)
	}
	return true
}

func (m *Manager) IsOriginEnabled(origin string) bool {
	normalized, ok := normalizeOrigin(origin)
	if !ok {
		return false
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.enabledOrigins[normalized] > 0
}

func (m *Manager) ShouldBlock(frameOrigin string, requestURL string, resourceType string) bool {
	if !m.IsOriginEnabled(frameOrigin) {
		return false
	}
	return m.engine.ShouldBlock(requestURL, resourceType)
}

func normalizeOrigin(value string) (string, bool) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", false
	}
	return strings.ToLower(parsed.Scheme + "://" + parsed.Host), true
}
