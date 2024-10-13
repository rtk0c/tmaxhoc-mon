package main

type UnitStatus int

const (
	US_Stopped UnitStatus = iota
	US_StoppedOnError
	US_Running
)

type Unit struct {
	Name         string
	startCommand string
	stopCommand  string
	Status       UnitStatus
}

type UnitRegistrar struct {
	tmux *TmuxSession
	// List of units, lexigraphically sorted by [Unit.Name]
	units []*Unit
	// Lookup table from [Unit.Name] to the [Unit] itself
	unitsLut map[string]*Unit
}

// TODO read config file for units
func NewUnitRegistrar(tmux *TmuxSession) (*UnitRegistrar, error) {
	r := &UnitRegistrar{
		tmux:     tmux,
		units:    make([]*Unit, 0),
		unitsLut: make(map[string]*Unit),
	}

	var u *Unit
	u = &Unit{Name: "Baking", startCommand: "./run", stopCommand: "stop"}
	r.units = append(r.units, u)
	r.unitsLut["Baking"] = u
	u = &Unit{Name: "Stoneblock 3", startCommand: "./run", stopCommand: "stop"}
	r.unitsLut["Stoneblock 3"] = u
	r.units = append(r.units, u)

	return r, nil
}
