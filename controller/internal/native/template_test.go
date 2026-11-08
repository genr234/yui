package native

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func testEnv(used *[]string) TemplateEnv {
	return TemplateEnv{
		DataDir: "/data/app",
		Asset: func(id string) (string, error) {
			if id == "missing" {
				return "", ErrAssetNotReady
			}
			if used != nil {
				*used = append(*used, id)
			}
			return "/assets/game/" + id + "/disc.rvz", nil
		},
		VirtualPad: func(n int) (string, error) {
			return fmt.Sprintf("127.0.0.1:26760/%d", n), nil
		},
	}
}

func TestRenderExpandsPlaceholders(t *testing.T) {
	var used []string
	got, err := Render("[Paths]\nUser = {{dataDir}}/user\nGame = {{ asset:42 }}\nPad = {{virtualPad:3}}\n", testEnv(&used))
	if err != nil {
		t.Fatal(err)
	}
	want := "[Paths]\nUser = /data/app/user\nGame = /assets/game/42/disc.rvz\nPad = 127.0.0.1:26760/3\n"
	if got != want {
		t.Fatalf("Render =\n%s\nwant\n%s", got, want)
	}
	if len(used) != 1 || used[0] != "42" {
		t.Fatalf("assets used = %v", used)
	}
}

func TestRenderLeavesOtherBracesAlone(t *testing.T) {
	in := `{"a": {"b": 1}} {x} {{dataDir}}`
	got, err := Render(in, testEnv(nil))
	if err != nil {
		t.Fatal(err)
	}
	if got != `{"a": {"b": 1}} {x} /data/app` {
		t.Fatalf("Render = %q", got)
	}
}

func TestRenderErrors(t *testing.T) {
	cases := map[string]string{
		"{{nope}}":            "unknown placeholder",
		"{{dataDir:x}}":       "unknown placeholder",
		"{{asset:}}":          "invalid asset id",
		"{{asset:../etc}}":    "invalid asset id",
		"{{asset:missing}}":   "not available",
		"{{virtualPad:x}}":    "virtual pad must be",
		"{{virtualPad:-1}}":   "virtual pad must be",
		"{{virtualPad:16}}":   "virtual pad must be",
		"ok {{asset:1}} {{}}": "unknown placeholder",
	}
	for in, want := range cases {
		_, err := Render(in, testEnv(nil))
		var te *TemplateError
		if err == nil || !errors.As(err, &te) || !strings.Contains(err.Error(), want) {
			t.Errorf("Render(%q) = %v, want error containing %q", in, err, want)
		}
		if err != nil && strings.Contains(err.Error(), "/assets/") {
			t.Errorf("Render(%q) error leaks a path: %v", in, err)
		}
	}
}

func TestRenderVirtualPadDefaultResolver(t *testing.T) {
	var r VirtualPadResolver = noVirtualPads{}
	env := testEnv(nil)
	env.VirtualPad = func(n int) (string, error) { return r.VirtualPad("dev.example.app", n) }
	_, err := Render("{{virtualPad:0}}", env)
	if !errors.Is(err, ErrVirtualPadsNotAttached) {
		t.Fatalf("Render = %v, want ErrVirtualPadsNotAttached", err)
	}
	if got, err := Render("no placeholders", env); err != nil || got != "no placeholders" {
		t.Fatalf("Render = %q, %v", got, err)
	}
}

func TestRenderSystemPad(t *testing.T) {
	env := testEnv(nil)
	if _, err := Render("{{systemPad}}", env); !errors.Is(err, ErrVirtualPadsNotAttached) {
		t.Fatalf("no resolver: %v", err)
	}
	env.SystemPad = func() (string, error) { return "127.0.0.1:26761/0", nil }
	got, err := Render("hotkeys = {{ systemPad }}", env)
	if err != nil || got != "hotkeys = 127.0.0.1:26761/0" {
		t.Fatalf("Render = %q, %v", got, err)
	}
	if _, err := Render("{{systemPad:1}}", env); err == nil {
		t.Fatal("{{systemPad:1}} accepted")
	}
}
