package main

import (
	"errors"
	"os"
	"regexp"

	"github.com/pelletier/go-toml/v2"
)

type configServiceUnit struct {
	TmuxWindowName string

	/* union */
	StartCommand []string
	StartScript  []string

	StopInput  []string
	StopScript []string
}

type configGroupUnit struct {
	Requires []string

	linkedGroupUnit *GroupUnit
}

type configUnit struct {
	Name        string
	Description string
	Styles      string

	Hidden bool

	/* union */
	Service *configServiceUnit `toml:",omitempty"`
	Target  *configGroupUnit   `toml:",omitempty"`
}

type configWebServer struct {
	StaticFilesDir string
}

type configTmux struct {
	SessionName string
}

type config struct {
	Web  configWebServer
	Tmux configTmux

	Units []configUnit

	MaxRunningUnits int
}

var sanitizer = regexp.MustCompile("[^a-zA-Z0-9-_ ]")

func sanitizeTmuxName(s string) string {
	return sanitizer.ReplaceAllLiteralString(s, "_")
}

func NewUnitSystemFromConfig(configFile string) (*UnitSystem, error) {
	f, err := os.Open(configFile)
	if err != nil {
		return nil, err
	}

	cfg := config{
		Tmux: configTmux{
			SessionName: "tmaxhoc-managed",
		},
		Web: configWebServer{
			StaticFilesDir: "static",
		},
		MaxRunningUnits: 0,
	}
	err = toml.NewDecoder(f).Decode(&cfg)
	if err != nil {
		return nil, err
	}

	res := &UnitSystem{
		units:       []*Unit{},
		unitsLut:    make(map[string]*Unit),
		tmuxNameLut: make(map[string]*ServiceUnit),

		MaxUnits: cfg.MaxRunningUnits,

		SessionName: cfg.Tmux.SessionName,

		StaticFilesDir: cfg.Web.StaticFilesDir,
	}

	for _, cu := range cfg.Units {
		u := &Unit{
			Name:        cu.Name,
			Description: cu.Description,
			Styles:      cu.Styles,
			Hidden:      cu.Hidden,
		}

		if cu.Service != nil {
			serv := &ServiceUnit{
				TmuxName: cu.Service.TmuxWindowName,
			}

			if len(cu.Service.TmuxWindowName) == 0 {
				serv.TmuxName = sanitizeTmuxName(cu.Name)
			}
			_, exists := res.tmuxNameLut[serv.TmuxName]
			if exists {
				return nil, errors.New("Duplicate tmux window name '" + serv.TmuxName + "'! Possibly caused by generated from unit names that differ only in special non-alphanumeric characters.")
			}
			res.tmuxNameLut[serv.TmuxName] = serv

			if len(cu.Service.StartScript) > 0 {
				serv.Start = cu.Service.StartScript
				serv.StartMode = ServiceScriptedStart
			} else {
				serv.Start = cu.Service.StartCommand
				serv.StartMode = ServiceDirectStart
			}

			if len(cu.Service.StopScript) > 0 {
				serv.Stop = cu.Service.StopScript
				serv.StopMode = ServiceScriptStop
			} else {
				serv.Stop = cu.Service.StopInput
				serv.StopMode = ServiceInputStop
			}

			u.driver = serv
		} else if cu.Target != nil {
			grp := &GroupUnit{}
			// requirements filled afterwards when the name LUT is fully built
			cu.Target.linkedGroupUnit = grp
			u.driver = grp
		}

		res.units = append(res.units, u)
		res.unitsLut[u.Name] = u
	}

	for _, cu := range cfg.Units {
		if cu.Target == nil {
			continue
		}

		d := cu.Target.linkedGroupUnit
		d.requirements = make([]*Unit, len(cu.Target.Requires))
		for i, subpartName := range cu.Target.Requires {
			d.requirements[i] = res.unitsLut[subpartName]
		}
	}

	return res, nil
}
