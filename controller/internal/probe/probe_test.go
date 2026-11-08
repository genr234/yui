package probe

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestParseAudioEndpointsArray(t *testing.T) {
	out := []byte(`[
  {"FriendlyName": "Speakers (Realtek(R) Audio)", "Status": "OK"},
  {"FriendlyName": "Headphones", "Status": "Unknown"}
]`)
	got, err := parseAudioEndpoints(out)
	if err != nil {
		t.Fatal(err)
	}
	want := []AudioEndpoint{
		{Name: "Speakers (Realtek(R) Audio)", Status: "OK"},
		{Name: "Headphones", Status: "Unknown"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestParseAudioEndpointsSingleObjectWithBOM(t *testing.T) {
	out := []byte("\xef\xbb\xbf{\r\n  \"FriendlyName\": \"Speakers\",\r\n  \"Status\": \"OK\"\r\n}\r\n")
	got, err := parseAudioEndpoints(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "Speakers" || got[0].Status != "OK" {
		t.Fatalf("unexpected %+v", got)
	}
}

func TestParseAudioEndpointsNumericStatus(t *testing.T) {
	got, err := parseAudioEndpoints([]byte(`{"FriendlyName": "Speakers", "Status": 1}`))
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Status != "1" {
		t.Fatalf("unexpected status %q", got[0].Status)
	}
}

func TestParseEmptyOutput(t *testing.T) {
	for _, out := range []string{"", "  \r\n", "null"} {
		endpoints, err := parseAudioEndpoints([]byte(out))
		if err != nil || len(endpoints) != 0 {
			t.Fatalf("audio %q: got %+v, %v", out, endpoints, err)
		}
		gpus, err := parseGPUs([]byte(out))
		if err != nil || len(gpus) != 0 {
			t.Fatalf("gpus %q: got %+v, %v", out, gpus, err)
		}
	}
}

func TestParseGPUs(t *testing.T) {
	got, err := parseGPUs([]byte(`{"Name": "Intel(R) UHD Graphics 620"}`))
	if err != nil || !reflect.DeepEqual(got, []string{"Intel(R) UHD Graphics 620"}) {
		t.Fatalf("single: %+v, %v", got, err)
	}
	got, err = parseGPUs([]byte(`[{"Name": "Intel"}, {"Name": ""}, {"Name": "NVIDIA GeForce"}]`))
	if err != nil || !reflect.DeepEqual(got, []string{"Intel", "NVIDIA GeForce"}) {
		t.Fatalf("array: %+v, %v", got, err)
	}
}

func TestParseInvalidJSON(t *testing.T) {
	if _, err := parseGPUs([]byte("Get-CimInstance : access denied")); err == nil {
		t.Fatal("expected error")
	}
}

func TestCollectNeverReturnsNilSlices(t *testing.T) {
	report := Collect(context.Background())
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "null") {
		t.Fatalf("report has null fields: %s", data)
	}
	if !strings.HasPrefix(report.Text(), "Hardware\n") {
		t.Fatalf("unexpected text: %q", report.Text())
	}
}

func TestReportTextShowsLoopback(t *testing.T) {
	if !strings.Contains(Report{OS: "windows", WindowsBuild: 22631, ProcessLoopbackSupported: true}.Text(), "Process loopback: yes") {
		t.Fatal("expected loopback yes in text")
	}
}
