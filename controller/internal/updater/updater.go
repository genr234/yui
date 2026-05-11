package updater

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"kiosk/controller/internal/config"
	"kiosk/controller/internal/version"
)

const (
	releaseAssetName = "yui-kiosk-installer.zip"
	installerName    = "yui-kiosk-installer.exe"
	maxDownloadBytes = 80 << 20
)

type Status struct {
	Enabled         bool   `json:"enabled"`
	Repo            string `json:"repo"`
	CurrentCommit   string `json:"current_commit"`
	LatestCommit    string `json:"latest_commit"`
	LatestTag       string `json:"latest_tag"`
	LatestURL       string `json:"latest_url"`
	AssetURL        string `json:"asset_url"`
	UpdateAvailable bool   `json:"update_available"`
	CheckedAt       string `json:"checked_at"`
	Error           string `json:"error,omitempty"`
}

type githubRelease struct {
	TagName         string        `json:"tag_name"`
	TargetCommitish string        `json:"target_commitish"`
	HTMLURL         string        `json:"html_url"`
	Draft           bool          `json:"draft"`
	Assets          []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

func Start(ctx context.Context, cfg config.Config) {
	interval := time.Duration(cfg.AutoUpdateInterval) * time.Minute
	if interval <= 0 {
		interval = 30 * time.Minute
	}

	go func() {
		timer := time.NewTimer(90 * time.Second)
		defer timer.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				currentCfg, err := config.Load()
				if err != nil {
					log.Printf("auto update config reload failed: %v", err)
					currentCfg = cfg
				}
				if !currentCfg.AutoUpdateEnabled {
					timer.Reset(interval)
					continue
				}
				if currentCfg.AutoUpdateInterval > 0 {
					interval = time.Duration(currentCfg.AutoUpdateInterval) * time.Minute
				}
				if err := CheckAndApply(ctx, currentCfg); err != nil {
					log.Printf("auto update check failed: %v", err)
				}
				timer.Reset(interval)
			}
		}
	}()
}

func Check(ctx context.Context, cfg config.Config) (Status, error) {
	status := Status{
		Enabled:       cfg.AutoUpdateEnabled,
		Repo:          cfg.AutoUpdateRepo,
		CurrentCommit: version.Commit,
		CheckedAt:     time.Now().Format(time.RFC3339),
	}

	release, asset, err := latestRelease(ctx, cfg.AutoUpdateRepo)
	if err != nil {
		status.Error = err.Error()
		return status, err
	}

	latestCommit := release.TargetCommitish
	if strings.HasPrefix(release.TagName, "kiosk-") {
		latestCommit = strings.TrimPrefix(release.TagName, "kiosk-")
	}

	status.LatestCommit = latestCommit
	status.LatestTag = release.TagName
	status.LatestURL = release.HTMLURL
	status.AssetURL = asset.BrowserDownloadURL
	status.UpdateAvailable = updateAvailable(version.Commit, latestCommit)

	return status, nil
}

func CheckAndApply(ctx context.Context, cfg config.Config) error {
	status, err := Check(ctx, cfg)
	if err != nil {
		return err
	}
	if !status.UpdateAvailable {
		return nil
	}
	_, err = Apply(ctx, cfg)
	return err
}

func Apply(ctx context.Context, cfg config.Config) (Status, error) {
	status, err := Check(ctx, cfg)
	if err != nil {
		return status, err
	}
	if !status.UpdateAvailable {
		return status, nil
	}
	if runtime.GOOS != "windows" {
		return status, fmt.Errorf("auto update install is only supported on Windows")
	}

	installerPath, err := downloadInstaller(ctx, status.AssetURL)
	if err != nil {
		return status, err
	}

	target, err := installedBatchPath()
	if err != nil {
		return status, err
	}

	cmd := exec.Command(installerPath, "--auto-update", "--parent-pid", strconv.Itoa(os.Getpid()), target)
	cmd.Dir = filepath.Dir(installerPath)
	if err := cmd.Start(); err != nil {
		return status, fmt.Errorf("start updater installer: %w", err)
	}

	log.Printf("started updater installer pid=%d target=%s", cmd.Process.Pid, target)
	go func() {
		time.Sleep(1200 * time.Millisecond)
		log.Printf("controller exiting for auto update")
		os.Exit(0)
	}()

	return status, nil
}

func latestRelease(ctx context.Context, repo string) (githubRelease, githubAsset, error) {
	repo = strings.TrimSpace(repo)
	if !validRepo(repo) {
		return githubRelease{}, githubAsset{}, fmt.Errorf("invalid GitHub repo %q; expected owner/name", repo)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/"+repo+"/releases?per_page=20", nil)
	if err != nil {
		return githubRelease{}, githubAsset{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "YuiKioskUpdater/"+version.Version)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return githubRelease{}, githubAsset{}, fmt.Errorf("query GitHub releases: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return githubRelease{}, githubAsset{}, fmt.Errorf("GitHub releases returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var releases []githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return githubRelease{}, githubAsset{}, fmt.Errorf("decode GitHub releases: %w", err)
	}

	for _, release := range releases {
		if release.Draft || !strings.HasPrefix(release.TagName, "kiosk-") {
			continue
		}
		for _, asset := range release.Assets {
			if asset.Name == releaseAssetName && asset.BrowserDownloadURL != "" {
				return release, asset, nil
			}
		}
	}

	return githubRelease{}, githubAsset{}, fmt.Errorf("no %s asset found in recent kiosk releases", releaseAssetName)
}

func downloadInstaller(ctx context.Context, assetURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, assetURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "YuiKioskUpdater/"+version.Version)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("download update asset: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("download update asset returned %s", resp.Status)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxDownloadBytes+1))
	if err != nil {
		return "", fmt.Errorf("read update asset: %w", err)
	}
	if len(data) > maxDownloadBytes {
		return "", fmt.Errorf("update asset exceeded %d bytes", maxDownloadBytes)
	}

	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("open update zip: %w", err)
	}

	tempDir, err := os.MkdirTemp("", "yui-update-*")
	if err != nil {
		return "", fmt.Errorf("create update temp dir: %w", err)
	}

	for _, file := range reader.File {
		if filepath.Base(file.Name) != installerName {
			continue
		}
		installerPath := filepath.Join(tempDir, installerName)
		if err := extractZipFile(file, installerPath); err != nil {
			return "", err
		}
		return installerPath, nil
	}

	return "", fmt.Errorf("update zip did not contain %s", installerName)
}

func extractZipFile(file *zip.File, targetPath string) error {
	source, err := file.Open()
	if err != nil {
		return fmt.Errorf("open %s in update zip: %w", file.Name, err)
	}
	defer source.Close()

	target, err := os.OpenFile(targetPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0755)
	if err != nil {
		return fmt.Errorf("create extracted installer: %w", err)
	}
	defer target.Close()

	if _, err := io.Copy(target, source); err != nil {
		return fmt.Errorf("extract installer: %w", err)
	}
	return nil
}

func installedBatchPath() (string, error) {
	exePath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("find controller executable: %w", err)
	}
	target := filepath.Join(filepath.Dir(exePath), "chrome.bat")
	if _, err := os.Stat(target); err != nil {
		return "", fmt.Errorf("find installed kiosk batch %s: %w", target, err)
	}
	return target, nil
}

func updateAvailable(current string, latest string) bool {
	current = normalizeCommit(current)
	latest = normalizeCommit(latest)
	if latest == "" || current == "" || current == "dev" {
		return false
	}
	if current == latest {
		return false
	}
	if isHexCommit(current) && isHexCommit(latest) {
		return !strings.HasPrefix(current, latest) && !strings.HasPrefix(latest, current)
	}
	return true
}

func validRepo(repo string) bool {
	parts := strings.Split(repo, "/")
	return len(parts) == 2 && parts[0] != "" && parts[1] != "" && !strings.Contains(repo, "..")
}

func normalizeCommit(commit string) string {
	commit = strings.TrimSpace(strings.ToLower(commit))
	return strings.TrimPrefix(commit, "kiosk-")
}

func isHexCommit(commit string) bool {
	if len(commit) < 7 {
		return false
	}
	for _, r := range commit {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') {
			continue
		}
		return false
	}
	return true
}
