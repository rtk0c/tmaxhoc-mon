package main

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

type TmuxSession struct {
	Name       string
	procGroups []*TmuxProcGroup
	// LUT from [TmuxProcGroup.WindowIndex] to proc groups
	byWindowIndex map[int]*TmuxProcGroup
	// LUT from [TmuxProcGroup.Unit] to proc groups
	byUnit map[*UnitDefinition]*TmuxProcGroup
	// Unitd to match proc groups from
	associatedUnitd *Unitd
}

type TmuxProcGroup struct {
	// Nullable
	// The unit associated with this proc groups
	Unit        *UnitDefinition
	Name        string
	WindowIndex int
	Pid         int
	// If true, this process has exited and will be removed on the next call to [TmuxSession.Prune]
	Dead bool
	// If true, this process was parsed rather than launched by [TmuxSession.SpawnProcesses].
	// Note that this property is orthogonal to [TmuxProcGroup.Unit];
	// an orphan proc group may have an associated unit, and a non-orphan proc group may not have an associated unit.
	Orphan bool
}

var TmuxExecutable = "/bin/tmux"

func NewTmuxSession(session string) (*TmuxSession, error) {
	cmd := exec.Command(TmuxExecutable, "has-session", "-t", session)
	err := cmd.Run()
	if err == nil {
		// Dummy window to keep the session alive
		cmd := exec.Command(TmuxExecutable, "new-session", "-d", "-s", session, "/bin/sh")
		_, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("failed to create tmux session: %w", err)
		}
	}

	return &TmuxSession{Name: session}, nil
}

func (ts *TmuxSession) insertProcGroup(procGroup *TmuxProcGroup) {
	ts.procGroups = append(ts.procGroups, procGroup)
	ts.byWindowIndex[procGroup.Pid] = procGroup
	ts.byUnit[procGroup.Unit] = procGroup
}

// windowName is an arbitary tmux window name for running the processes in.
//
// commandParts is the command to execute to starting the process. See tmux *shell_command* a full shell command for
// starting the service. For an abbreviated example, `miniserve -p 1234` results in `/bin/sh -c 'miniserv -p 1234'`,
// whereas `miniserve` `-p` `1234` results in running miniserve directly with the arguments.
//
// TODO multiple processes
func (ts *TmuxSession) spawnProcesses(windowName string, commandParts ...string) (*TmuxProcGroup, error) {
	cmdArglist := []string{"new-window", "-t", ts.Name + ":", "-n", windowName, "-P", "-F", "#{window_index}:#{pane_pid}"}
	cmdArglist = append(cmdArglist, commandParts...)
	cmd := exec.Command(TmuxExecutable, cmdArglist...)
	windowInfo, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var windowIndex, pid int
	fmt.Sscanf(string(windowInfo), "%d:%d", &windowIndex, &pid)

	cmd = exec.Command(TmuxExecutable, "set-option", "-t", ts.Name+":"+strconv.Itoa(windowIndex), "synchronize-panes", "on")
	err = cmd.Run()
	if err != nil {
		return nil, err
	}

	procGroup := &TmuxProcGroup{
		Name:        windowName,
		WindowIndex: windowIndex,
		Pid:         pid,
	}
	ts.insertProcGroup(procGroup)
	return procGroup, nil
}

func (ts *TmuxSession) StartUnit(unit *UnitDefinition) {
	procGroup := ts.byUnit[unit]
	if procGroup != nil  {
		// TODO allow Dead proc groups to be restarted?
		return
	}

	ts.spawnProcesses(unit.Name, unit.startCommand...)
}

func (ts *TmuxSession) StopUnit(unit *UnitDefinition) {
	procGroup := ts.byUnit[unit]
	if procGroup == nil {
		return
	}

	ts.SendKeys(procGroup, unit.stopCommand...)
	// Let the next call to [TmuxSession.Poll] to cleanup this ts
}

func (ts *TmuxSession) Poll() error {
	for _, serv := range ts.procGroups {
		err := syscall.Kill(serv.Pid, syscall.Signal(0))
		if err != nil {
			serv.Pid = 0
			serv.Dead = true
		}
	}

	cmd := exec.Command(TmuxExecutable, "list-panes", "-s", "-t", ts.Name+":", "-F", "#{window_index}$#{pane_pid}$#{window_name}")
	panes, err := cmd.Output()
	if err != nil {
		return err
	}

	for _, lineByte := range strings.Split(string(panes), "\n") {
		parts := strings.SplitN(lineByte, "$", 2)
		windowIndex, err := strconv.Atoi(parts[0])
		if err != nil {
			return err
		}
		pid, err := strconv.Atoi(parts[1])
		if err != nil {
			return err
		}
		windowName := parts[2]

		procGroup := ts.byWindowIndex[windowIndex]
		if procGroup == nil {
			procGroup = &TmuxProcGroup{
				Unit:        ts.associatedUnitd.MatchByName(windowName),
				Name:        windowName,
				WindowIndex: windowIndex,
				Pid:         pid,
				Orphan:      true,
			}
			ts.insertProcGroup(procGroup)
		}
	}

	return nil
}

func (ts *TmuxSession) Prune() {
	procGroups := ts.procGroups
	// Technically, the "last unprocessed service" but that's a long name
	lastAlive := len(procGroups) - 1
	i := 0
	for i <= lastAlive {
		if procGroups[i].Dead {
			delete(ts.byWindowIndex, procGroups[i].Pid)
			procGroups[i] = procGroups[lastAlive]
			lastAlive--
		} else {
			i++
		}
	}
	ts.procGroups = procGroups[:lastAlive+1]
}

func (session *TmuxSession) SendKeys(serv *TmuxProcGroup, keys ...string) error {
	cmdArglist := []string{"send-keys", "-t", session.Name + ":" + serv.Name}
	cmdArglist = append(cmdArglist, keys...)
	cmd := exec.Command(TmuxExecutable, cmdArglist...)
	err := cmd.Run()
	if err != nil {
		return err
	}

	return nil
}
