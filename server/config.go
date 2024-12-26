package main

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
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

type ServiceUnit struct {
	// Name of the tmux window hosting this unit process.
	TmuxName string `toml:"TmuxWindowName"`

	// If non-zero, a stop command has been issued but we're not sure it has died.
	stoppingAttempt time.Time

	procs []*TmuxProcess

	// Arguments to `tmux new-window` (see [TmuxSession.spawnProcess]).
	StartCommand []string
	// Arguments to `tmux send-command` to every process in this unit
	StopCommand []string

	// If true, start command is run as a process, passing our managed tmux session name as the sole argument,
	// and parsing stdout for lines in the form of tmux -F '#{pane_id}\t{pane_pid}'.
	ScriptedStart bool
	// If true, stop command is run as process, passing #{pane_id} and #{pane_pid} as additional arguments for each process in this unit.
	ScriptedStop bool
}

func (serv *ServiceUnit) start(ts *TmuxSession) error {
	if len(serv.procs) > 0 {
		return nil
	}

	if serv.ScriptedStart {
		exe := serv.StartCommand[0]
		args := serv.StartCommand[1:]
		return ts.spawnByScript(serv.TmuxName, exe, args...)
	}

	proc, err := ts.spawnProcess(serv.TmuxName, serv.StartCommand...)
	if err != nil {
		return err
	}
	serv.procs = []*TmuxProcess{proc}
	return nil
}

func (serv *ServiceUnit) stop(ts *TmuxSession) {
	if len(serv.procs) == 0 {
		return
	}

	if serv.ScriptedStop {
		args := serv.StopCommand[1:]
		for _, proc := range serv.procs {
			args = append(args, proc.targetPane())
			args = append(args, strconv.Itoa(proc.Pid))
		}
		cmd := exec.Command(serv.StopCommand[0], args...)
		cmd.Run()
		return
	}

	for _, proc := range serv.procs {
		ts.SendKeys(proc, serv.StopCommand...)
		serv.stoppingAttempt = time.Now()
	}
}

func (serv *ServiceUnit) status() UnitStatus {
	if len(serv.procs) > 0 {
		if serv.stoppingAttempt.IsZero() {
			return Running
		} else {
			return Stopping
		}
	} else {
		return Stopped
	}
}

func (serv *ServiceUnit) forceStopAllowed() bool {
	return serv.status() == Stopping && time.Since(serv.stoppingAttempt) > 10*time.Second
}

func (serv *ServiceUnit) forceStop(ts *TmuxSession) {
	for _, proc := range serv.procs {
		ts.ForceKillProcess(proc)
	}
}

type GroupUnit struct {
	// Dependencies listed by [Unit.Name], which are started before this starts, and stopped after this stops.
	Requires []string
	// Dependencies references.
	// Generated after unmarshal.
	requirements []*Unit
}

func (gp *GroupUnit) start(ts *TmuxSession) error {
	for _, req := range gp.requirements {
		err := req.driver.start(ts)
		if err != nil {
			return err
		}
	}
	return nil
}

func (gp *GroupUnit) stop(ts *TmuxSession) {
	for _, req := range gp.requirements {
		req.driver.stop(ts)
	}
}

func (gp *GroupUnit) status() UnitStatus {
	allStopping := false
	for _, req := range gp.requirements {
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

func (*GroupUnit) forceStopAllowed() bool   { return false }
func (*GroupUnit) forceStop(_ *TmuxSession) {}

func (gp *GroupUnit) numReqsRunning() int {
	n := 0
	for _, req := range gp.requirements {
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

	Service *ServiceUnit `toml:",omitempty"`
	Target  *GroupUnit   `toml:",omitempty"`
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
	tmuxNameLut map[string]*ServiceUnit

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
	res.tmuxNameLut = make(map[string]*ServiceUnit)
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
		case *ServiceUnit:
			d := unit.driver.(*ServiceUnit)
			if len(d.TmuxName) == 0 {
				d.TmuxName = sanitizeTmuxName(unit.Name)
			}
			_, exists := res.tmuxNameLut[d.TmuxName]
			if exists {
				panic("Duplicate tmux window name '" + d.TmuxName + "'! Possibly caused by generated from unit names that differ only in special non-alphanumeric characters.")
			}
			res.tmuxNameLut[d.TmuxName] = d
		case *GroupUnit:
			d := unit.driver.(*GroupUnit)
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
		serv := cfg.tmuxNameLut[proc.Name]
		if serv != nil {
			serv.procs = append(serv.procs, proc)
		}
	}
	ts.onProcPruned = func(proc *TmuxProcess) {
		serv := cfg.tmuxNameLut[proc.Name]
		if serv != nil {
			idx := -1
			for i, known := range serv.procs {
				if proc == known {
					idx = i
					break
				}
			}
			if idx == -1 {
				fmt.Println("[WARN] process pruned that was never reported to unit manager")
				return
			}
			lastIdx := len(serv.procs) - 1
			serv.procs[idx] = serv.procs[lastIdx]
			serv.procs = serv.procs[:lastIdx]
			if len(serv.procs) == 0 {
				serv.stoppingAttempt = time.Time{}
			}
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
