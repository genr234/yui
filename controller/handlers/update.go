package handlers

import (
	"context"
	"encoding/json"
	"time"

	"kiosk/controller/internal/updater"
)

func UpdateCommands() []Command {
	return []Command{
		UpdateCheckCommand{},
		UpdateApplyCommand{},
	}
}

type UpdateCheckCommand struct{}

func (UpdateCheckCommand) Name() string { return "update.check" }

func (UpdateCheckCommand) Handle(r *Registry, _ json.RawMessage) (any, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return updater.Check(ctx, r.cfg)
}

type UpdateApplyCommand struct{}

func (UpdateApplyCommand) Name() string { return "update.apply" }

func (UpdateApplyCommand) Handle(r *Registry, _ json.RawMessage) (any, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	return updater.Apply(ctx, r.cfg)
}
