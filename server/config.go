package main

import (
	"os"
	"regexp"
	"time"

	"github.com/pelletier/go-toml/v2"
)

type UnitStatus int

const (
	Stopped UnitStatus = iota
	Stopping
	Running
)

type UnitDriver interface {
	start(ts *TmuxSession) error
	stop(ts *TmuxSession)
	status() UnitStatus

	forceStopAllowed() bool
	forceStop(ts *TmuxSession)
}

type UnitProcess struct {
	// Name of the tmux window hosting this unit process.
	TmuxName string `toml:"TmuxWindowName"`

	// Arguments to `tmux new-window` (see [TmuxSession.spawnProcess]).
	StartCommand []string

	// Arguments to `tmux send-command`
	StopCommand []string

	proc *TmuxProcess
}

func (up *UnitProcess) start(ts *TmuxSession) error {
	if up.proc != nil {
		return nil
	}

	proc, err := ts.spawnProcess(up.TmuxName, up.StartCommand...)
	if err != nil {
		return err
	}
	up.proc = proc
	return nil
}

func (up *UnitProcess) stop(ts *TmuxSession) {
	if up.proc == nil {
		return
	}

	ts.SendKeys(up.proc, up.StopCommand...)
	ts.markSuspectDead(up.proc)
}

func (up *UnitProcess) status() UnitStatus {
	if up.proc != nil {
		if up.proc.StoppingAttempt.IsZero() {
			return Running
		} else {
			return Stopping
		}
	} else {
		return Stopped
	}
}

func (up *UnitProcess) forceStopAllowed() bool {
	return up.status() == Stopping && time.Since(up.proc.StoppingAttempt) > 10*time.Second
}

func (up *UnitProcess) forceStop(ts *TmuxSession) {
	ts.ForceKillProcGroup(up.proc)
}

type UnitGroup struct {
	// Dependencies listed by [Unit.Name], which are started before this starts, and stopped after this stops.
	Requires []string
	// Dependencies references.
	// Generated after unmarshal.
	requirements []*Unit
}

func (ug *UnitGroup) start(ts *TmuxSession) error {
	for _, req := range ug.requirements {
		err := req.driver.start(ts)
		if err != nil {
			return err
		}
	}
	return nil
}

func (ug *UnitGroup) stop(ts *TmuxSession) {
	for _, req := range ug.requirements {
		req.driver.stop(ts)
	}
}

func (ug *UnitGroup) status() UnitStatus {
	allStopping := false
	for _, req := range ug.requirements {
		status := req.driver.status()
		allStopping = allStopping || status == Stopping
		if status == Running {
			return Running
		}
	}
	if allStopping {
		return Stopping
	}
	return Stopped
}

func (*UnitGroup) forceStopAllowed() bool   { return false }
func (*UnitGroup) forceStop(_ *TmuxSession) {}

func (ug *UnitGroup) numReqsRunning() int {
	n := 0
	for _, req := range ug.requirements {
		if req.driver.status() == Running {
			n++
		}
	}
	return n
}

type Unit struct {
	Name        string
	Description string
	Styles      string // Written as HTML styles attribute in the panel.

	// If true, this unit is not displayed in the panel.
	Hidden bool

	Service *UnitProcess `toml:",omitempty"`
	Target  *UnitGroup   `toml:",omitempty"`
	driver  UnitDriver
}

var sanitizer = regexp.MustCompile("[^a-zA-Z0-9-_ ]")

func sanitizeTmuxName(s string) string {
	return sanitizer.ReplaceAllLiteralString(s, "_")
}

type Config struct {
	// List of units in the same order as the config file.
	// Will also be displayed on the panel in this order.
	// Immutable after load.
	Units []*Unit
	// Lookup table from [Unit.Name] to the [Unit] itself.
	// Immutable after load. Generated after unmarshal;.
	unitsLut    map[string]*Unit
	tmuxNameLut map[string]*UnitProcess

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

	res.unitsLut = make(map[string]*Unit)
	res.tmuxNameLut = make(map[string]*UnitProcess)
	for _, unit := range res.Units {
		res.unitsLut[unit.Name] = unit

		// TODO move this sum type over ("Service" UnitProcess | "Target" UnitGroup) hack into a MarshalTOML method
		if unit.Service != nil {
			unit.driver = unit.Service
		} else if unit.Target != nil {
			unit.driver = unit.Target
		}
	}
	for _, unit := range res.Units {
		switch unit.driver.(type) {
		case *UnitProcess:
			d := unit.driver.(*UnitProcess)
			if len(d.TmuxName) == 0 {
				d.TmuxName = sanitizeTmuxName(unit.Name)
			}
			_, exists := res.tmuxNameLut[d.TmuxName]
			if exists {
				panic("Duplicate tmux window name '" + d.TmuxName + "'! Possibly caused by generated from unit names that differ only in special non-alphanumeric characters.")
			}
			res.tmuxNameLut[d.TmuxName] = d
		case *UnitGroup:
			d := unit.driver.(*UnitGroup)
			d.requirements = make([]*Unit, len(d.Requires))
			for i, subpartName := range d.Requires {
				d.requirements[i] = res.unitsLut[subpartName]
			}
		}
	}

	return &res, nil
}

func (cfg *Config) BindTmuxSession(ts *TmuxSession) {
	ts.onProcSpawned = func(proc *TmuxProcess) {
		up := cfg.tmuxNameLut[proc.Name]
		if up != nil {
			up.proc = proc
		}
	}
	ts.onProcPruned = func(proc *TmuxProcess) {
		up := cfg.tmuxNameLut[proc.Name]
		if up != nil {
			up.proc = nil
		}
	}
}

// Nullable
func (cfg *Config) MatchByName(name string) *Unit {
	// TODO better fuzzy matching algorithm; ideas:
	// - case insensitive matching
	// - whitespace/-/_ insensitive matching
	// - allow unit names to be supplied as a regex, and return a list of candidates?
	// - some kind of fuzzy scoring algorithm like fzf/sublime text's command palette
	return cfg.unitsLut[name]
}
