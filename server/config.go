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

type Config struct {
	// List of Units, lexigraphically sorted by [Unit.Name].
	// Immutable after load.
	Units []*UnitDefinition
	// Lookup table from [Unit.Name] to the [Unit] itself.
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
