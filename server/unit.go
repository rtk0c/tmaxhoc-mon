package main

type UnitDefinition struct {
	Name         string
	startCommand []string
	stopCommand  []string
}

type Unitd struct {
	// List of units, lexigraphically sorted by [Unit.Name].
	// Immutable after load.
	units []*UnitDefinition
	// Lookup table from [Unit.Name] to the [Unit] itself.
	// Immutable after load.
	unitsLut map[string]*UnitDefinition
}

func NewUnitd() (*Unitd, error) {
	r := &Unitd{
		units:    make([]*UnitDefinition, 0),
		unitsLut: make(map[string]*UnitDefinition),
	}

	// TODO read config file for units
	var u *UnitDefinition
	u = &UnitDefinition{Name: "Baking", startCommand: []string{"./run"}, stopCommand: []string{"stop", "Enter"}}
	r.units = append(r.units, u)
	r.unitsLut["Baking"] = u
	u = &UnitDefinition{Name: "Stoneblock 3", startCommand: []string{"./run"}, stopCommand: []string{"stop", "Enter"}}
	r.unitsLut["Stoneblock 3"] = u
	r.units = append(r.units, u)

	return r, nil
}

// Nullable
func (unitd *Unitd) MatchByName(name string) *UnitDefinition {
	// TODO better fuzzy matching algorithm; ideas:
	// - case insensitive matching
	// - whitespace/-/_ insensitive matching
	// - allow unit names to be supplied as a regex, and return a list of candidates?
	// - some kind of fuzzy scoring algorithm like fzf/sublime text's command palette
	return unitd.unitsLut[name]
}
