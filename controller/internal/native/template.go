package native

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// ErrVirtualPadsNotAttached is what the default VirtualPadResolver returns.
var ErrVirtualPadsNotAttached = errors.New("virtual pads not attached")

// VirtualPadResolver expands {{virtualPad:<n>}} for one session. The input
// hub provides the real implementation (for example the DSU address and slot
// of pad n); until it is attached every lookup fails.
type VirtualPadResolver interface {
	VirtualPad(appID string, n int) (string, error)
}

type noVirtualPads struct{}

func (noVirtualPads) VirtualPad(string, int) (string, error) {
	return "", ErrVirtualPadsNotAttached
}

// MaxVirtualPad is the highest pad index a template may name.
const MaxVirtualPad = 15

// TemplateEnv supplies the values a template may reference.
type TemplateEnv struct {
	// Asset returns the local path of a ready asset.
	Asset func(id string) (string, error)
	// DataDir is the app's writable directory for this session.
	DataDir string
	// VirtualPad expands {{virtualPad:<n>}}.
	VirtualPad func(n int) (string, error)
}

var (
	placeholderPattern = regexp.MustCompile(`\{\{([^{}]*)\}\}`)
	assetIDPattern     = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
)

// TemplateError names the placeholder that failed. Its message never
// includes a resolved path.
type TemplateError struct {
	Placeholder string
	Err         error
}

func (e *TemplateError) Error() string {
	return fmt.Sprintf("template {{%s}}: %v", e.Placeholder, e.Err)
}

func (e *TemplateError) Unwrap() error { return e.Err }

// Render expands every {{...}} placeholder in s. Unknown placeholders are
// errors, so a typo cannot silently reach the program's config.
func Render(s string, env TemplateEnv) (string, error) {
	var firstErr error
	out := placeholderPattern.ReplaceAllStringFunc(s, func(match string) string {
		if firstErr != nil {
			return match
		}
		key := strings.TrimSpace(match[2 : len(match)-2])
		value, err := resolvePlaceholder(key, env)
		if err != nil {
			firstErr = &TemplateError{Placeholder: key, Err: err}
			return match
		}
		return value
	})
	if firstErr != nil {
		return "", firstErr
	}
	return out, nil
}

func resolvePlaceholder(key string, env TemplateEnv) (string, error) {
	name, arg, hasArg := strings.Cut(key, ":")
	name = strings.TrimSpace(name)
	arg = strings.TrimSpace(arg)
	switch {
	case name == "dataDir" && !hasArg:
		if env.DataDir == "" {
			return "", errors.New("no data directory")
		}
		return env.DataDir, nil
	case name == "asset" && hasArg:
		if !assetIDPattern.MatchString(arg) {
			return "", errors.New("invalid asset id")
		}
		if env.Asset == nil {
			return "", errors.New("assets are not available")
		}
		return env.Asset(arg)
	case name == "virtualPad" && hasArg:
		n, err := strconv.Atoi(arg)
		if err != nil || n < 0 || n > MaxVirtualPad {
			return "", fmt.Errorf("virtual pad must be 0-%d", MaxVirtualPad)
		}
		if env.VirtualPad == nil {
			return "", ErrVirtualPadsNotAttached
		}
		return env.VirtualPad(n)
	}
	return "", errors.New("unknown placeholder")
}
