package blocker

import (
	"net/url"
	"strings"
)

type Rule struct {
	HostSuffix string
	PathPrefix string
	Substring  string
	Resource   string
	Exception  bool
}

type Engine struct {
	rulesByResource map[string][]Rule
	genericRules    []Rule
}

func NewEngine(rules []Rule) *Engine {
	engine := &Engine{rulesByResource: make(map[string][]Rule)}
	for _, rule := range rules {
		if rule.Resource == "" {
			engine.genericRules = append(engine.genericRules, rule)
			continue
		}
		engine.rulesByResource[rule.Resource] = append(engine.rulesByResource[rule.Resource], rule)
	}
	return engine
}

func (e *Engine) ShouldBlock(rawURL string, resourceType string) bool {
	if e == nil {
		return false
	}

	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" {
		return false
	}

	host := strings.ToLower(parsed.Hostname())
	normalizedURL := strings.ToLower(rawURL)
	normalizedResource := strings.ToLower(resourceType)
	blocked := false

	for _, rule := range e.genericRules {
		if !rule.matches(host, normalizedURL) {
			continue
		}
		if rule.Exception {
			return false
		}
		blocked = true
	}
	for _, rule := range e.rulesByResource[normalizedResource] {
		if !rule.matches(host, normalizedURL) {
			continue
		}
		if rule.Exception {
			return false
		}
		blocked = true
	}

	return blocked
}

func (r Rule) matches(host string, rawURL string) bool {
	if r.HostSuffix != "" {
		if host != r.HostSuffix && !strings.HasSuffix(host, "."+r.HostSuffix) {
			return false
		}
		return r.PathPrefix == "" || strings.Contains(rawURL, r.PathPrefix)
	}
	if r.Substring != "" {
		return strings.Contains(rawURL, r.Substring)
	}
	return false
}

func DefaultRules() []Rule {
	return parseRules(defaultRuleList)
}

func parseRules(data string) []Rule {
	lines := strings.Split(data, "\n")
	rules := make([]Rule, 0, len(lines))

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "!") || strings.HasPrefix(line, "#") {
			continue
		}

		rule := Rule{}
		if strings.HasPrefix(line, "@@") {
			rule.Exception = true
			line = strings.TrimPrefix(line, "@@")
		}

		filter, options, _ := strings.Cut(line, "$")
		for _, option := range strings.Split(options, ",") {
			option = strings.TrimSpace(strings.ToLower(option))
			switch option {
			case "script":
				rule.Resource = "script"
			case "image":
				rule.Resource = "image"
			case "stylesheet":
				rule.Resource = "stylesheet"
			case "font":
				rule.Resource = "font"
			case "media":
				rule.Resource = "media"
			case "xmlhttprequest":
				rule.Resource = "xhr"
			}
		}

		filter = strings.TrimSpace(strings.ToLower(filter))
		if strings.HasPrefix(filter, "||") {
			value := strings.TrimPrefix(filter, "||")
			host := value
			path := ""
			if cut := strings.Index(value, "^/"); cut >= 0 {
				host = value[:cut]
				path = value[cut+1:]
			} else if cut := strings.Index(value, "/"); cut >= 0 {
				host = value[:cut]
				path = value[cut:]
			} else {
				host = strings.TrimRight(host, "^")
			}
			host = strings.TrimPrefix(host, ".")
			if host != "" {
				rule.HostSuffix = host
				rule.PathPrefix = path
				rules = append(rules, rule)
			}
			continue
		}

		filter = strings.Trim(filter, "|")
		filter = strings.ReplaceAll(filter, "*", "")
		filter = strings.Trim(filter, "^")
		if filter != "" {
			rule.Substring = filter
			rules = append(rules, rule)
		}
	}

	return rules
}
