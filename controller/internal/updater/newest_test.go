package updater

import (
	"testing"
	"time"
)

func TestNewestReleaseUsesPublishDateNotListOrder(t *testing.T) {
	at := func(h int) time.Time { return time.Date(2026, 11, 9, h, 0, 0, 0, time.UTC) }
	asset := []githubAsset{{Name: releaseAssetName, BrowserDownloadURL: "https://example/x.zip"}}
	releases := []githubRelease{
		{TagName: "kiosk-ae6b629", PublishedAt: at(16), Assets: asset},
		{TagName: "kiosk-a9064da", PublishedAt: at(13), Assets: asset},
		{TagName: "kiosk-0da6bdd", PublishedAt: at(17), Assets: asset},
		{TagName: "kiosk-fffffff", PublishedAt: at(18)}, // no installer
		{TagName: "v9", PublishedAt: at(19), Assets: asset},
	}
	got, _, ok := newestRelease(releases)
	if !ok || got.TagName != "kiosk-0da6bdd" {
		t.Fatalf("newest = %q, %v; want kiosk-0da6bdd", got.TagName, ok)
	}
}
