// Package probe collects facts about the kiosk's hardware so the audio
// strategy (plan decision D7) can be chosen remotely.
package probe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// ProcessLoopbackMinBuild is the first Windows build with WASAPI process
// loopback capture.
const ProcessLoopbackMinBuild = 20348

type AudioEndpoint struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

type Report struct {
	OS                       string          `json:"os"`
	WindowsBuild             int             `json:"windows_build"`
	ProcessLoopbackSupported bool            `json:"process_loopback_supported"`
	AudioEndpoints           []AudioEndpoint `json:"audio_endpoints"`
	GPUs                     []string        `json:"gpus"`
	Errors                   []string        `json:"errors"`
}

// Collect gathers the report. It never fails: anything that goes wrong is
// recorded in Report.Errors.
func Collect(ctx context.Context) Report {
	report := collect(ctx)
	report.ProcessLoopbackSupported = report.WindowsBuild >= ProcessLoopbackMinBuild
	if report.AudioEndpoints == nil {
		report.AudioEndpoints = []AudioEndpoint{}
	}
	if report.GPUs == nil {
		report.GPUs = []string{}
	}
	if report.Errors == nil {
		report.Errors = []string{}
	}
	return report
}

// Text renders the report as a readable section for the --check output.
func (r Report) Text() string {
	var b strings.Builder
	b.WriteString("Hardware\n")
	fmt.Fprintf(&b, "  OS: %s\n", r.OS)
	if r.WindowsBuild > 0 {
		fmt.Fprintf(&b, "  Windows build: %d\n", r.WindowsBuild)
	} else {
		b.WriteString("  Windows build: n/a\n")
	}
	fmt.Fprintf(&b, "  Process loopback: %s\n", yesNo(r.ProcessLoopbackSupported))
	if len(r.AudioEndpoints) == 0 {
		b.WriteString("  Audio endpoints: none found\n")
	} else {
		b.WriteString("  Audio endpoints:\n")
		for _, ep := range r.AudioEndpoints {
			fmt.Fprintf(&b, "    - %s (%s)\n", ep.Name, ep.Status)
		}
	}
	if len(r.GPUs) == 0 {
		b.WriteString("  GPUs: none found\n")
	} else {
		fmt.Fprintf(&b, "  GPUs: %s\n", strings.Join(r.GPUs, ", "))
	}
	for _, e := range r.Errors {
		fmt.Fprintf(&b, "  Probe error: %s\n", e)
	}
	return strings.TrimRight(b.String(), "\n")
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

// decodeOneOrMany decodes PowerShell ConvertTo-Json output, which is a single
// object when the pipeline yields one item, an array for several, and empty
// for none.
func decodeOneOrMany[T any](out []byte) ([]T, error) {
	out = bytes.TrimPrefix(out, []byte("\xef\xbb\xbf"))
	out = bytes.TrimSpace(out)
	if len(out) == 0 || bytes.Equal(out, []byte("null")) {
		return nil, nil
	}
	if out[0] == '[' {
		var items []T
		if err := json.Unmarshal(out, &items); err != nil {
			return nil, err
		}
		return items, nil
	}
	var item T
	if err := json.Unmarshal(out, &item); err != nil {
		return nil, err
	}
	return []T{item}, nil
}

type pnpDevice struct {
	FriendlyName string          `json:"FriendlyName"`
	Status       json.RawMessage `json:"Status"`
}

func parseAudioEndpoints(out []byte) ([]AudioEndpoint, error) {
	devices, err := decodeOneOrMany[pnpDevice](out)
	if err != nil {
		return nil, fmt.Errorf("parse audio endpoints: %w", err)
	}
	endpoints := make([]AudioEndpoint, 0, len(devices))
	for _, d := range devices {
		endpoints = append(endpoints, AudioEndpoint{Name: d.FriendlyName, Status: rawToString(d.Status)})
	}
	return endpoints, nil
}

type videoController struct {
	Name string `json:"Name"`
}

func parseGPUs(out []byte) ([]string, error) {
	controllers, err := decodeOneOrMany[videoController](out)
	if err != nil {
		return nil, fmt.Errorf("parse gpus: %w", err)
	}
	gpus := make([]string, 0, len(controllers))
	for _, c := range controllers {
		if name := strings.TrimSpace(c.Name); name != "" {
			gpus = append(gpus, name)
		}
	}
	return gpus, nil
}

// rawToString accepts a status that PowerShell serialised either as a string
// or as a number (enum value).
func rawToString(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return string(raw)
}
