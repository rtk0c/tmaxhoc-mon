package main

import (
	"os"

	"github.com/pelletier/go-toml/v2"
)

type UnitDefinition struct {
	Name        string
	Description string
	Styles      string // Written as HTML styles attribute in the panel.

	// Arguments to `tmux new-window` (see [TmuxSession.spawnProcess]).
	// If empty, this unit is dummy, and will never spawn any process.
	StartCommand []string

	// Arguments to `tmux send-command`
	StopCommand []string

	// If true, this unit is not displayed in the panel.
	Hidden bool

	// Dependencies listed by [UnitDefinition.Name], which are started before this starts, and stopped after this stops.
	Subparts []string
	// Dependencies references.
	// Generated after unmarshal.
	subpartsRef []*UnitDefinition
}

type Config struct {
	// List of [UnitDefinition]s, in the same order as the config file.
	// Will also be displayed on the panel in this order.
	// Immutable after load.
	Units []*UnitDefinition
	// Lookup table from [UnitDefinition.Name] to the [UnitDefinition] itself.
	// Immutable after load. Generated after unmarshal;.
	unitsLut map[string]*UnitDefinition
	// Max number of units allowed to run at a time
	MaxUnits int

	SessionName string

	// Path to the directory holding static files
	StaticFilesDir string

	// Template file for the main page
	FrontpageTemplate string
}

func NewConfig(configFile string) (*Config, error) {
	f, err := os.Open(configFile)
	if err != nil {
		panic(err)
	}

	res := Config{
		SessionName:       "tmaxhoc-managed",
		StaticFilesDir:    "static",
		FrontpageTemplate: "template/frontpage.tmpl",
	}
	err = toml.NewDecoder(f).Decode(&res)
	if err != nil {
		panic(err)
	}

	res.unitsLut = make(map[string]*UnitDefinition)
	for _, unit := range res.Units {
		res.unitsLut[unit.Name] = unit
	}
	for _, unit := range res.Units {
		unit.subpartsRef = make([]*UnitDefinition, len(unit.Subparts))
		for i, subpartName := range unit.Subparts {
			unit.subpartsRef[i] = res.unitsLut[subpartName]
		}
	}

	return &res, nil
}

// Nullable
func (cfg *Config) MatchByName(name string) *UnitDefinition {
	// TODO better fuzzy matching algorithm; ideas:
	// - case insensitive matching
	// - whitespace/-/_ insensitive matching
	// - allow unit names to be supplied as a regex, and return a list of candidates?
	// - some kind of fuzzy scoring algorithm like fzf/sublime text's command palette
	return cfg.unitsLut[name]
}
