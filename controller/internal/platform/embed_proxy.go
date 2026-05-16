package platform

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func (r *Runtime) handleEmbedProxy(w http.ResponseWriter, req *http.Request) {
	if !r.validPlatformHTTPToken(req) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	upstream, ok := embedProxyUpstream(req.URL.Path, req.URL.RawQuery)
	if !ok {
		http.Error(w, "invalid embed proxy url", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(req.Context(), 30*time.Second)
	defer cancel()

	proxyReq, err := http.NewRequestWithContext(ctx, req.Method, upstream, nil)
	if err != nil {
		http.Error(w, "invalid upstream request", http.StatusBadRequest)
		return
	}
	copyEmbedProxyRequestHeaders(proxyReq.Header, req.Header)

	client := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(proxyReq)
	if err != nil {
		log.Printf("embed proxy fetch failed url=%q: %v", upstream, err)
		http.Error(w, "embed proxy fetch failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if location := resp.Header.Get("Location"); location != "" && resp.StatusCode >= 300 && resp.StatusCode < 400 {
		redirectURL := resolveEmbedProxyURL(upstream, location)
		if redirectURL == "" {
			http.Error(w, "invalid upstream redirect", http.StatusBadGateway)
			return
		}
		http.Redirect(w, req, r.embedProxyURLFor(redirectURL), resp.StatusCode)
		return
	}

	copyEmbedProxyResponseHeaders(w.Header(), resp.Header)
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cross-Origin-Resource-Policy", "cross-origin")
	w.Header().Del("Content-Security-Policy")
	w.Header().Del("Content-Security-Policy-Report-Only")
	w.Header().Del("X-Frame-Options")

	body := resp.Body
	if shouldRewriteEmbedProxyHTML(resp) {
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			http.Error(w, "embed proxy read failed", http.StatusBadGateway)
			return
		}
		w.Header().Del("Content-Length")
		body = io.NopCloser(bytes.NewReader(rewriteEmbedProxyHTML(upstream, data, r.cfg.PlatformBridgeToken)))
	}

	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, body)
}

func embedProxyUpstream(path string, rawQuery string) (string, bool) {
	const prefix = "/embed-proxy/"
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}

	rest := strings.TrimPrefix(path, prefix)
	token, suffix, _ := strings.Cut(rest, "/")
	if token == "" {
		return "", false
	}

	data, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return "", false
	}
	baseURL, err := url.Parse(string(data))
	if err != nil || !isEmbeddableProxyURL(baseURL) {
		return "", false
	}

	upstream := baseURL
	if suffix != "" {
		relative, err := url.Parse(suffix)
		if err != nil {
			return "", false
		}
		upstream = baseURL.ResolveReference(relative)
	}
	if rawQuery != "" {
		query, err := url.ParseQuery(rawQuery)
		if err != nil {
			return "", false
		}
		query.Del("token")
		if len(query) > 0 {
			upstream.RawQuery = query.Encode()
		}
	}
	if !isEmbeddableProxyURL(upstream) {
		return "", false
	}
	return upstream.String(), true
}

func (r *Runtime) validPlatformHTTPToken(req *http.Request) bool {
	expected := r.cfg.PlatformBridgeToken
	if expected == "" {
		return false
	}
	actual := req.URL.Query().Get("token")
	if actual == "" {
		actual = req.Header.Get("X-Yui-Platform-Token")
	}
	return subtle.ConstantTimeCompare([]byte(actual), []byte(expected)) == 1
}

func encodeEmbedProxyURL(value string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(value))
}

func resolveEmbedProxyURL(baseValue string, location string) string {
	baseURL, err := url.Parse(baseValue)
	if err != nil {
		return ""
	}
	nextURL, err := url.Parse(location)
	if err != nil {
		return ""
	}
	resolved := baseURL.ResolveReference(nextURL)
	if !isEmbeddableProxyURL(resolved) {
		return ""
	}
	return resolved.String()
}

func isEmbeddableProxyURL(value *url.URL) bool {
	return value != nil && (value.Scheme == "http" || value.Scheme == "https") && value.Host != "" && !isBlockedProxyHost(value.Hostname())
}

func isBlockedProxyHost(host string) bool {
	host = strings.TrimSpace(strings.Trim(host, "[]"))
	if host == "" {
		return true
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return isBlockedProxyIP(ip)
	}
	ips, err := net.LookupIP(host)
	if err != nil || len(ips) == 0 {
		return true
	}
	for _, ip := range ips {
		if isBlockedProxyIP(ip) {
			return true
		}
	}
	return false
}

func isBlockedProxyIP(ip net.IP) bool {
	return ip.IsUnspecified() ||
		ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsMulticast()
}

func copyEmbedProxyRequestHeaders(dst http.Header, src http.Header) {
	for _, name := range []string{"Accept", "Accept-Language", "Range", "User-Agent"} {
		if value := src.Values(name); len(value) > 0 {
			dst[name] = append([]string(nil), value...)
		}
	}
}

func copyEmbedProxyResponseHeaders(dst http.Header, src http.Header) {
	for name, values := range src {
		switch strings.ToLower(name) {
		case "content-security-policy", "content-security-policy-report-only", "x-frame-options", "set-cookie", "transfer-encoding", "content-length":
			continue
		default:
			dst[name] = append([]string(nil), values...)
		}
	}
}

func shouldRewriteEmbedProxyHTML(resp *http.Response) bool {
	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	return resp.StatusCode >= 200 && resp.StatusCode < 300 && strings.Contains(contentType, "text/html")
}

func rewriteEmbedProxyHTML(upstream string, data []byte, token string) []byte {
	baseHref := fmt.Sprintf("/embed-proxy/%s/", encodeEmbedProxyURL(upstream))
	if token != "" {
		baseHref += "?token=" + url.QueryEscape(token)
	}
	base := []byte(`<base href="` + baseHref + `">`)
	lower := bytes.ToLower(data)
	if idx := bytes.Index(lower, []byte("<head>")); idx >= 0 {
		insertAt := idx + len("<head>")
		result := make([]byte, 0, len(data)+len(base))
		result = append(result, data[:insertAt]...)
		result = append(result, base...)
		result = append(result, data[insertAt:]...)
		return result
	}
	return append(base, data...)
}

func (r *Runtime) embedProxyURLFor(rawURL string) string {
	return embedProxyURLFor(rawURL, r.cfg.PlatformBridgeToken)
}

func embedProxyURLFor(rawURL string, token string) string {
	proxyURL := fmt.Sprintf("/embed-proxy/%s", encodeEmbedProxyURL(rawURL))
	if token != "" {
		proxyURL += "?token=" + url.QueryEscape(token)
	}
	return proxyURL
}
