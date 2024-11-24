package main

import (
	"os"

	"github.com/pelletier/go-toml/v2"
)

type UnitDefinition struct {
	Name         string
	Description  string
	Color        string
	StartCommand []string
	StopCommand  []string
}

type Unitd struct {
	// List of Units, lexigraphically sorted by [Unit.Name].
	// Immutable after load.
	Units []*UnitDefinition
	// Lookup table from [Unit.Name] to the [Unit] itself.
	// Immutable after load. Generated after unmarshal;.
	unitsLut map[string]*UnitDefinition
	// Max number of units allowed to run at a time
	MaxUnits int
}

func NewUnitd(unitDefsFile string) (*Unitd, error) {
	f, err := os.Open(unitDefsFile)
	if err != nil {
		panic(err)
	}

	var res Unitd
	err = toml.NewDecoder(f).Decode(&res)
	if err != nil {
		panic(err)
	}

	res.unitsLut = make(map[string]*UnitDefinition)
	for _, unit := range res.Units {
		res.unitsLut[unit.Name] = unit
	}

	return &res, nil
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
