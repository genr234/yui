package handlers

import (
	"encoding/json"

	"kiosk/controller/internal/blocker"
)

func EmbedBlockerCommands() []Command {
	return []Command{
		EmbedBlockerSetCommand{},
	}
}

type EmbedBlockerSetCommand struct{}

type embedBlockerSetParams struct {
	AppID   string `json:"appId"`
	Origin  string `json:"origin"`
	Enabled bool   `json:"enabled"`
}

func (EmbedBlockerSetCommand) Name() string { return "embedBlocker.set" }

func (EmbedBlockerSetCommand) Handle(_ *Registry, params json.RawMessage) (any, error) {
	var request embedBlockerSetParams
	if err := json.Unmarshal(params, &request); err != nil {
		return nil, err
	}
	ok := blocker.DefaultManager.SetEmbed(request.AppID, request.Origin, request.Enabled)
	return map[string]bool{"ok": ok}, nil
}
