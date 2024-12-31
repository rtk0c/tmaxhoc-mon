package main

import (
	"os/exec"
	"strconv"
)

type ServiceUnitStartMode int

const (
	// Run as a command in a tmux window directly,
	// by passing [ServiceUnit.Start] to `tmux new-window` (see [TmuxSession.spawnProcess]).
	ServiceDirectStart ServiceUnitStartMode = iota
	// Run as startup script, passing our managed tmux session name as the sole argument.
	// Tmux processed obtained by parsing stdout for lines in the form of tmux -F '#{pane_id}\t{pane_pid}'.
	ServiceScriptedStart
)

type ServiceUnitStopMode int

const (
	// Type something into the tmux window.
	// by passing [ServiceUnit.Stop] to `tmux send-command` to every process in this unit
	ServiceInputStop ServiceUnitStopMode = iota
	// Run as stop script, passing #{pane_id} and #{pane_pid} as additional arguments for each process in this unit.
	ServiceScriptStop
)

type SlfdrvSimple struct {
	Start []string
	Stop  []string

	StartMode ServiceUnitStartMode
	StopMode  ServiceUnitStopMode
}

func (drv *SlfdrvSimple) start(serv *ServiceUnit, ts *TmuxSession) error {
	switch drv.StartMode {
	case ServiceDirectStart:
		_, err := ts.spawnProcess(serv.TmuxName, drv.Start...)
		if err != nil {
			return err
		}
		return nil

	case ServiceScriptedStart:
		exe := drv.Start[0]
		args := drv.Start[1:]
		return ts.spawnByScript(serv.TmuxName, exe, args...)
	}

	return nil
}

func (drv *SlfdrvSimple) stop(serv *ServiceUnit, ts *TmuxSession) {
	switch drv.StopMode {
	case ServiceInputStop:
		for _, proc := range serv.procs {
			ts.SendKeys(proc, drv.Stop...)
		}

	case ServiceScriptStop:
		args := drv.Stop[1:]
		for _, proc := range serv.procs {
			args = append(args, proc.targetPane())
			args = append(args, strconv.Itoa(proc.Pid))
		}
		cmd := exec.Command(drv.Stop[0], args...)
		cmd.Run()
	}
}
