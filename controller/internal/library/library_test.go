package library

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"kiosk/controller/internal/store"
)

func openCollection(t *testing.T) store.Collection {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "account.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db.Collection(CollectionName)
}

func ptr(f float64) *float64 { return &f }

var t0 = time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)

func ids(titles []Title) string {
	out := make([]string, len(titles))
	for i, t := range titles {
		out[i] = t.AppID + "/" + t.ID
	}
	return strings.Join(out, ",")
}

func TestPublishPlayedAndRecentRanking(t *testing.T) {
	c := openCollection(t)
	if _, err := Publish(c, "dev.genr.arcade", []Input{
		{ID: "RMCE01", Title: "Kart", Players: 4, Progress: ptr(0.4), Resume: json.RawMessage(`{"slot":1}`)},
		{ID: "RSBE01", Title: "Brawl", Players: 4},
	}, false, t0); err != nil {
		t.Fatal(err)
	}
	if _, err := Publish(c, "dev.genr.fruit-ninja", []Input{{ID: "zen", Title: "Zen mode", Art: "data:image/png;base64,iVBORw0KGgo="}}, false, t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}

	// Never played: newest publish first.
	recent, err := Recent(c, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(recent); got != "dev.genr.fruit-ninja/zen,dev.genr.arcade/RMCE01,dev.genr.arcade/RSBE01" {
		t.Fatalf("unplayed order = %s", got)
	}

	if _, err := Played(c, "dev.genr.arcade", "RSBE01", PlayedPatch{}, t0.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	kart, err := Played(c, "dev.genr.arcade", "RMCE01", PlayedPatch{Progress: ptr(0.75), Resume: json.RawMessage(`{"slot":2}`)}, t0.Add(3*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if *kart.Progress != 0.75 || string(kart.Resume) != `{"slot":2}` || kart.Players != 4 {
		t.Fatalf("played title = %+v", kart)
	}

	// Played titles first, by last played; then the never-played.
	recent, _ = Recent(c, 0)
	if got := ids(recent); got != "dev.genr.arcade/RMCE01,dev.genr.arcade/RSBE01,dev.genr.fruit-ninja/zen" {
		t.Fatalf("ranked order = %s", got)
	}
	if recent, _ = Recent(c, 1); len(recent) != 1 {
		t.Fatalf("limit ignored: %d", len(recent))
	}

	// Republishing keeps the played time and so the rank.
	if _, err := Publish(c, "dev.genr.arcade", []Input{{ID: "RSBE01", Title: "Brawl (EU)"}}, false, t0.Add(4*time.Minute)); err != nil {
		t.Fatal(err)
	}
	recent, _ = Recent(c, 0)
	if recent[1].ID != "RSBE01" || recent[1].Title != "Brawl (EU)" || recent[1].PlayedAt == "" {
		t.Fatalf("republish lost played_at: %+v", recent[1])
	}
}

func TestPublishReplaceRemovesMissingTitles(t *testing.T) {
	c := openCollection(t)
	app := "dev.genr.arcade"
	if _, err := Publish(c, app, []Input{{ID: "a", Title: "A"}, {ID: "b", Title: "B"}}, false, t0); err != nil {
		t.Fatal(err)
	}
	if _, err := Publish(c, "dev.other.app", []Input{{ID: "a", Title: "Other A"}}, false, t0); err != nil {
		t.Fatal(err)
	}
	if _, err := Publish(c, app, []Input{{ID: "b", Title: "B"}}, true, t0); err != nil {
		t.Fatal(err)
	}
	list, _ := List(c, app)
	if ids(list) != app+"/b" {
		t.Fatalf("after replace = %s", ids(list))
	}
	other, _ := List(c, "dev.other.app")
	if len(other) != 1 {
		t.Fatal("replace touched another app's titles")
	}
	if err := Remove(c, app, []string{"b", "never"}); err != nil {
		t.Fatal(err)
	}
	if list, _ = List(c, app); len(list) != 0 {
		t.Fatalf("after remove = %s", ids(list))
	}
}

func TestPlayedRequiresPublishedTitle(t *testing.T) {
	c := openCollection(t)
	if _, err := Played(c, "dev.genr.arcade", "ghost", PlayedPatch{}, t0); err != ErrNotPublished {
		t.Fatalf("Played = %v, want ErrNotPublished", err)
	}
}

func TestPublishValidation(t *testing.T) {
	c := openCollection(t)
	bad := map[string]Input{
		"invalid title id":    {ID: "a/b", Title: "x"},
		"needs a name":        {ID: "a", Title: "  "},
		"art must be":         {ID: "a", Title: "x", Art: "https://evil.example/x.png"},
		"data:image":          {ID: "a", Title: "x", Art: "data:text/html;base64,PHNjcmlwdD4="},
		"players must be":     {ID: "a", Title: "x", Players: 99},
		"progress must be":    {ID: "a", Title: "x", Progress: ptr(1.5)},
		"resume must be JSON": {ID: "a", Title: "x", Resume: json.RawMessage(`{`)},
	}
	for want, in := range bad {
		if _, err := Publish(c, "dev.genr.arcade", []Input{in}, false, t0); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: Publish = %v", want, err)
		}
	}
	if _, err := Publish(c, "Not An App", []Input{{ID: "a", Title: "x"}}, false, t0); err == nil {
		t.Error("invalid app id accepted")
	}
	if _, err := Publish(c, "dev.genr.arcade", []Input{{ID: "a", Title: "x"}, {ID: "a", Title: "y"}}, false, t0); err == nil {
		t.Error("duplicate ids accepted")
	}
	titles, err := Publish(c, "dev.genr.arcade", []Input{{ID: "a", Title: "x", Art: "42"}}, false, t0)
	if err != nil || !titles[0].ArtAsset {
		t.Fatalf("asset art = %+v, %v", titles, err)
	}
}

func TestRankIsStableAndCapped(t *testing.T) {
	var titles []Title
	for i := 0; i < MaxRecent+10; i++ {
		titles = append(titles, Title{AppID: "dev.genr.app", ID: string(rune('a'+i%26)) + string(rune('a'+i/26)), PublishedAt: t0.Format(time.RFC3339Nano)})
	}
	got := Rank(titles, 1000)
	if len(got) != MaxRecent {
		t.Fatalf("len = %d, want %d", len(got), MaxRecent)
	}
	for i := 1; i < len(got); i++ {
		if got[i-1].ID > got[i].ID {
			t.Fatalf("ties are not ordered by id: %s before %s", got[i-1].ID, got[i].ID)
		}
	}
}
