package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxNetworkFetchBodyBytes = 5 * 1024 * 1024

func NetworkCommands() []Command {
	return []Command{
		NetworkFetchCommand{},
	}
}

type NetworkFetchCommand struct{}

type networkFetchParams struct {
	URL     string            `json:"url"`
	Method  string            `json:"method"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
}

type networkFetchResult struct {
	URL     string              `json:"url"`
	Status  int                 `json:"status"`
	Headers map[string][]string `json:"headers"`
	Body    string              `json:"body"`
}

func (NetworkFetchCommand) Name() string { return "network.fetch" }

func (NetworkFetchCommand) Handle(_ *Registry, params json.RawMessage) (any, error) {
	var reqParams networkFetchParams
	if err := json.Unmarshal(params, &reqParams); err != nil {
		return nil, err
	}

	parsedURL, err := url.Parse(reqParams.URL)
	if err != nil {
		return nil, err
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return nil, fmt.Errorf("network.fetch only supports http and https urls")
	}

	method := strings.ToUpper(strings.TrimSpace(reqParams.Method))
	if method == "" {
		method = http.MethodGet
	}

	request, err := http.NewRequest(method, parsedURL.String(), strings.NewReader(reqParams.Body))
	if err != nil {
		return nil, err
	}
	for key, value := range reqParams.Headers {
		request.Header.Set(key, value)
	}
	if request.Header.Get("User-Agent") == "" {
		request.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/145.0 Safari/537.36")
	}
	if request.Header.Get("Accept") == "" {
		request.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	}
	if request.Header.Get("Accept-Language") == "" {
		request.Header.Set("Accept-Language", "en-US,en;q=0.9")
	}

	client := http.Client{Timeout: 15 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, maxNetworkFetchBodyBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxNetworkFetchBodyBytes {
		return nil, fmt.Errorf("network.fetch response exceeded %d bytes", maxNetworkFetchBodyBytes)
	}

	return networkFetchResult{
		URL:     response.Request.URL.String(),
		Status:  response.StatusCode,
		Headers: response.Header,
		Body:    string(body),
	}, nil
}
