package main

type UnitStatus int

const (
	US_Stopped UnitStatus = iota
	US_Running
)

type Unit struct {
	procGroup    *TmuxProcGroup
	Name         string
	startCommand []string
	stopCommand  []string
	Status       UnitStatus
}

type UnitRegistrar struct {
	tmux *TmuxSession
	// List of units, lexigraphically sorted by [Unit.Name].
	// Immutable after load.
	units []*Unit
	// Lookup table from [Unit.Name] to the [Unit] itself.
	// Immutable after load.
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
	u = &Unit{Name: "Baking", startCommand: []string{"./run"}, stopCommand: []string{"stop", "Enter"}}
	r.units = append(r.units, u)
	r.unitsLut["Baking"] = u
	u = &Unit{Name: "Stoneblock 3", startCommand: []string{"./run"}, stopCommand: []string{"stop", "Enter"}}
	r.unitsLut["Stoneblock 3"] = u
	r.units = append(r.units, u)

	return r, nil
}

func (reg *UnitRegistrar) StartUnit(unit *Unit) {
	if unit.Status != US_Stopped {
		return
	}

	reg.tmux.SpawnProcesses(unit.Name, unit.startCommand...)
}

func updateUnitFromTmux(unit *Unit) {
	if unit.procGroup.Dead {
		unit.Status = US_Stopped
		unit.procGroup = nil
	}
}

func (reg *UnitRegistrar) StopUnit(unit *Unit) {
	if unit.Status != US_Running {
		return
	}

	reg.tmux.SendKeys(unit.procGroup, unit.stopCommand...)
	updateUnitFromTmux(unit)
}

func (reg *UnitRegistrar) MapFromTmux() {
	for _, unit := range reg.units {
		if unit.procGroup == nil {
			continue
		}

		updateUnitFromTmux(unit)
	}
	// TODO map from "wild" tmux windows/process groups
}
