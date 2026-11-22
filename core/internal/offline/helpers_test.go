package offline

import (
	"github.com/Nijjea1/fleetline/core/internal/state"
	"github.com/Nijjea1/fleetline/core/internal/world"
)

func newPool() *state.Mem { return state.NewMem() }

func newWorld(c Config, start int64) *world.World {
	return world.New(world.Config{Travel: c.Travel, Patience: c.Patience}, start, nil)
}
