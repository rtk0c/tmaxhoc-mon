package main

import (
	"fmt"
	"time"
)

type UnitStatus int

const (
	Stopped UnitStatus = iota
	Stopping
	Running
)

// A workload backed directly by some processes.
type Unitv4Service struct {
	// Name of the tmux window hosting this unit process.
	TmuxName string

	// If non-zero, a stop command has been issued but we're not sure it has died.
	stoppingAttempt time.Time

	procs []*TmuxProcess

	lifecycleDriver ServiceLifecycleDriver
}

type ServiceLifecycleDriver interface {
	start(serv *Unitv4Service, ts *TmuxSession) error
	stop(serv *Unitv4Service, ts *TmuxSession)
}

func (serv *Unitv4Service) start(ts *TmuxSession) error {
	if len(serv.procs) > 0 {
		return nil
	}

	return serv.lifecycleDriver.start(serv, ts)
}

func (serv *Unitv4Service) stop(ts *TmuxSession) {
	if len(serv.procs) == 0 {
		return
	}

	serv.lifecycleDriver.stop(serv, ts)
	serv.stoppingAttempt = time.Now()
}

func (serv *Unitv4Service) status() UnitStatus {
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

func (serv *Unitv4Service) forceStopAllowed() bool {
	return serv.status() == Stopping && time.Since(serv.stoppingAttempt) > 10*time.Second
}

func (serv *Unitv4Service) forceStop(ts *TmuxSession) {
	for _, proc := range serv.procs {
		ts.ForceKillProcess(proc)
	}
}

// A workload that exists as a composite of some other workloads.
type Unitv4Group struct {
	// Dependencies listed by [Unit.Name], which are started before this starts, and stopped after this stops.
	// Generated after unmarshal.
	requirements []*Unit
}

func (gp *Unitv4Group) start(ts *TmuxSession) error {
	for _, req := range gp.requirements {
		err := req.v.start(ts)
		if err != nil {
			return err
		}
	}
	return nil
}

func (gp *Unitv4Group) stop(ts *TmuxSession) {
	for _, req := range gp.requirements {
		req.v.stop(ts)
	}
}

func (gp *Unitv4Group) status() UnitStatus {
	allStopping := false
	for _, req := range gp.requirements {
		status := req.v.status()
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

func (*Unitv4Group) forceStopAllowed() bool   { return false }
func (*Unitv4Group) forceStop(_ *TmuxSession) {}

func (gp *Unitv4Group) numReqsRunning() int {
	n := 0
	for _, req := range gp.requirements {
		if req.v.status() == Running {
			n++
		}
	}
	return n
}

// A single managed workload. What happens when this workload is started/stopped varies by the exact kind of this workload.
type Unit struct {
	Name        string
	Description string
	Styles      string // Written as HTML styles attribute in the panel.

	// If true, this unit is not displayed in the panel.
	Hidden bool

	// The "virtual" part of this unit that determines the kind
	v Unitv
}

type Unitv interface {
	start(ts *TmuxSession) error
	stop(ts *TmuxSession)
	status() UnitStatus

	forceStopAllowed() bool
	forceStop(ts *TmuxSession)
}

type UnitSystem struct {
	// List of units in the same order as the config file.
	// Will also be displayed on the panel in this order.
	// Immutable after load.
	units []*Unit
	// Lookup table from [Unit.Name] to the [Unit] itself.
	// Immutable after load. Generated after unmarshal;.
	unitsLut    map[string]*Unit
	tmuxNameLut map[string]*Unitv4Service

	// Max number of units allowed to run at a time
	MaxUnits int

	SessionName string

	// Path to the directory holding static files
	StaticFilesDir string
}

func (cfg *UnitSystem) BindTmuxSession(ts *TmuxSession) {
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
func (cfg *UnitSystem) MatchByName(name string) *Unit {
	// TODO better fuzzy matching algorithm; ideas:
	// - case insensitive matching
	// - whitespace/-/_ insensitive matching
	// - allow unit names to be supplied as a regex, and return a list of candidates?
	// - some kind of fuzzy scoring algorithm like fzf/sublime text's command palette
	return cfg.unitsLut[name]
}

func (cfg *UnitSystem) RunningServicesCount() int {
	count := 0
	for _, unit := range cfg.units {
		switch unit.v.(type) {
		case *Unitv4Service:
			if unit.v.status() == Running {
				count++
			}
		}
	}
	return count
}
