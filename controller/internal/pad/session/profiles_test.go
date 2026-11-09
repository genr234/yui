package session

import (
	"encoding/json"
	"testing"
)

func TestPlayerCarriesAccountLinked(t *testing.T) {
	var rec SessionRecord
	if err := json.Unmarshal([]byte(`{"players":[
		{"id":"a","slot":1,"name":"A","color":"#fff","host":true,"connected":true,"account_linked":true},
		{"id":"b","slot":2,"name":"B","color":"#000","host":false,"connected":true}]}`), &rec); err != nil {
		t.Fatal(err)
	}
	if !rec.Players[0].AccountLinked || rec.Players[1].AccountLinked {
		t.Fatalf("players %+v", rec.Players)
	}
	out, _ := json.Marshal(rec.Players[0])
	var back map[string]any
	_ = json.Unmarshal(out, &back)
	if back["account_linked"] != true {
		t.Fatalf("pad.players JSON %s", out)
	}
}
