package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"kiosk/controller/internal/native"
)

// Web bundles of installed apps (contracts §6, plan 11 W3): `web.zip` next to
// the entry URL, checked against the manifest's web sha256, extracted to
// <ConfigDir>/apps-web/<app id>/.

const (
	installedWebDir     = "apps-web"
	webStagingDir       = ".staging"
	webBundleMaxBytes   = 64 << 20
	webBundleMaxFiles   = 2000
	webBundleFetchLimit = 64 << 20
	webBundleTimeout    = 5 * time.Minute
)

// The manifest declares the archive's hash as `web: { sha256: "<hex>" }`, or
// as `webSha256: "<hex>"`. A dev app's `web: "./web/"` has neither.
var (
	appWebObjectSha = regexp.MustCompile(`\bweb\s*:\s*\{[^{}]*?\bsha256\s*:\s*["']([0-9A-Fa-f]{64})["']`)
	appWebFlatSha   = regexp.MustCompile(`\bwebSha256\s*:\s*["']([0-9A-Fa-f]{64})["']`)
)

func webSha256FromSource(source string) string {
	if m := appWebObjectSha.FindStringSubmatch(source); len(m) > 1 {
		return strings.ToLower(m[1])
	}
	if m := appWebFlatSha.FindStringSubmatch(source); len(m) > 1 {
		return strings.ToLower(m[1])
	}
	return ""
}

// webBundleURL is web.zip next to the entry URL.
func webBundleURL(entryURL string) (string, error) {
	base, err := url.Parse(entryURL)
	if err != nil {
		return "", err
	}
	ref, _ := url.Parse("web.zip")
	return base.ResolveReference(ref).String(), nil
}

func (r *Registry) appsWebRoot() string {
	dir := r.snapshotConfig().ConfigDir
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, installedWebDir)
}

// installWebBundle downloads, checks and extracts the app's web bundle. It
// does nothing when the app declares none. The old bundle is replaced only
// after the new one extracted completely.
func (r *Registry) installWebBundle(appID, entryURL, wantSha string) error {
	if wantSha == "" {
		return nil
	}
	root := r.appsWebRoot()
	if root == "" {
		return fmt.Errorf("no config directory for web bundles")
	}
	if !appIDPattern.MatchString(appID) {
		return fmt.Errorf("invalid app id")
	}
	zipURL, err := webBundleURL(entryURL)
	if err != nil {
		return err
	}
	if err := requireHTTPS(zipURL); err != nil {
		return err
	}
	staging := filepath.Join(root, webStagingDir)
	if err := os.MkdirAll(staging, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(staging, "web-*.zip")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	sum, err := downloadTo(tmp, zipURL, webBundleFetchLimit)
	tmp.Close()
	if err != nil {
		return err
	}
	if sum != wantSha {
		return fmt.Errorf("web bundle hash mismatch")
	}

	dest := filepath.Join(staging, "extract-"+appID)
	_ = os.RemoveAll(dest)
	if err := native.Extract(tmpPath, dest, native.Limits{MaxBytes: webBundleMaxBytes, MaxFiles: webBundleMaxFiles}); err != nil {
		return fmt.Errorf("web bundle: %w", err)
	}
	final := filepath.Join(root, appID)
	if err := os.RemoveAll(final); err != nil {
		_ = os.RemoveAll(dest)
		return err
	}
	if err := os.Rename(dest, final); err != nil {
		_ = os.RemoveAll(dest)
		return err
	}
	return nil
}

// removeWebBundle deletes the app's extracted bundle, if any.
func (r *Registry) removeWebBundle(appID string) {
	root := r.appsWebRoot()
	if root == "" || !appIDPattern.MatchString(appID) {
		return
	}
	_ = os.RemoveAll(filepath.Join(root, appID))
}

// downloadTo streams an https url into w, returning its sha256 (hex).
func downloadTo(w io.Writer, rawURL string, limit int64) (string, error) {
	client := *appHTTPClient
	client.Timeout = webBundleTimeout
	resp, err := client.Get(rawURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("web bundle download returned %s", resp.Status)
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(w, h), io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return "", err
	}
	if n > limit {
		return "", fmt.Errorf("web bundle exceeded %d bytes", limit)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
