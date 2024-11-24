package main

import (
	"fmt"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type TmuxSession struct {
	Name string
	// LUT from [TmuxProcGroup.WindowIndex] to proc groups.
	// Every proc group managed by this session must be inside this map.
	byWindowIndex map[int]*TmuxProcGroup
	// LUT from [TmuxProcGroup.Unit] to proc groups.
	// A proc group managed by this session is in this map only if it has an associated unit.
	byUnit map[*UnitDefinition]*TmuxProcGroup
	// Proc groups that are about to die or is evident to be dead by a conflicting window index.
	suspectDead []*TmuxProcGroup
	// Config to match proc groups from
	associatedConfig *Config
}

type TmuxProcGroup struct {
	// Nullable
	// The unit associated with this proc groups
	Unit        *UnitDefinition
	Name        string
	WindowIndex int
	Pid         int
	// If true, a stop command has been issued but we're not sure it has died.
	// TODO stopping time so we can have a timeout mechanism?
	StoppingAttempt time.Time
	// If true, this process has exited and been removed by [TmuxSession.PollAndPrune].
	// This field is for let library users to drop the reference to this proc group after it died,
	// without having to do a lookup in the [TmuxService].
	Dead bool
	// If true, this process was parsed rather than launched by [TmuxSession.SpawnProcesses].
	// Note that this property is orthogonal to [TmuxProcGroup.Unit];
	// an orphan proc group may have an associated unit, and a non-orphan proc group may not have an associated unit.
	Orphan bool
}

var TmuxExecutable = "/bin/tmux"

func NewTmuxSession(config *Config, session string) (*TmuxSession, error) {
	cmd := exec.Command(TmuxExecutable, "has-session", "-t", session)
	err := cmd.Run()
	if err != nil {
		// Dummy window to keep the session alive
		cmd := exec.Command(TmuxExecutable, "new-session", "-d", "-s", session, "/bin/sh")
		_, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("failed to create tmux session: %w", err)
		}
	}

	return &TmuxSession{
		Name:             session,
		byWindowIndex:    make(map[int]*TmuxProcGroup),
		byUnit:           make(map[*UnitDefinition]*TmuxProcGroup),
		associatedConfig: config,
	}, nil
}

func (ts *TmuxSession) insertProcGroup(procGroup *TmuxProcGroup) {
	old := ts.byWindowIndex[procGroup.WindowIndex]
	if old != nil {
		// Not needed. Insertion below overrides it anyways.
		/* delete(ts.byWindowIndex, old.WindowIndex) */
		delete(ts.byUnit, old.Unit)
		ts.suspectDead = append(ts.suspectDead, old)
	}

	ts.byWindowIndex[procGroup.WindowIndex] = procGroup
	if procGroup.Unit != nil {
		ts.byUnit[procGroup.Unit] = procGroup
	}
}

func (ts *TmuxSession) removeProcGroup(procGroup *TmuxProcGroup) {
	delete(ts.byWindowIndex, procGroup.WindowIndex)
	delete(ts.byUnit, procGroup.Unit)
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
		Unit:        ts.associatedConfig.MatchByName(windowName),
		Name:        windowName,
		WindowIndex: windowIndex,
		Pid:         pid,
	}
	ts.insertProcGroup(procGroup)
	fmt.Printf("spawned proc group %d:%s of pid=%d\n", windowIndex, windowName, pid)
	return procGroup, nil
}

func (ts *TmuxSession) StartUnit(unit *UnitDefinition) {
	procGroup := ts.byUnit[unit]
	if procGroup != nil {
		return
	}

	ts.spawnProcesses(unit.Name, unit.StartCommand...)
}

func (ts *TmuxSession) StopUnit(unit *UnitDefinition) {
	procGroup := ts.byUnit[unit]
	if procGroup == nil {
		return
	}

	ts.SendKeys(procGroup, unit.StopCommand...)
	procGroup.StoppingAttempt = time.Now()
	ts.removeProcGroup(procGroup)
	ts.suspectDead = append(ts.suspectDead, procGroup)
}

func (ts *TmuxSession) ForceKillProcGroup(procGroup *TmuxProcGroup) {
	err := syscall.Kill(procGroup.Pid, syscall.SIGKILL)
	if err != nil {
		fmt.Printf("[ERROR] failed to force kill pid=%d: %s\n", procGroup.Pid, err)
	}
}

func (ts *TmuxSession) PollAndPrune() error {
	//// Detect dead proc groups, and prune them ////
	for _, procGroup := range ts.byWindowIndex {
		err := syscall.Kill(procGroup.Pid, syscall.Signal(0))
		if err != nil {
			fmt.Printf("removing dead proc group %d:%s of pid=%d\n", procGroup.WindowIndex, procGroup.Name, procGroup.Pid)
			ts.removeProcGroup(procGroup)
			procGroup.Dead = true
		}
	}
	// Technically, the "last unprocessed proc group" but that's a long name
	lastAlive := len(ts.suspectDead) - 1
	i := 0
	for i <= lastAlive {
		suspect := ts.suspectDead[i]
		err := syscall.Kill(suspect.Pid, syscall.Signal(0))
		if err != nil {
			fmt.Printf("removing confirmed suspect dead proc group %d:%s of pid=%d\n", suspect.WindowIndex, suspect.Name, suspect.Pid)
			// Defensive: all suspect deads should already be removed from byXxx lookup tables, but in case it is not
			// catch it here, so we still have a consistent state
			ts.removeProcGroup(suspect)
			ts.suspectDead[i] = ts.suspectDead[lastAlive]
			suspect.Dead = true
			lastAlive--
		} else {
			i++
		}
	}
	ts.suspectDead = ts.suspectDead[:lastAlive+1]

	//// Poll for newly created windows by somebody else, keep records and try to map them to units ////
	cmd := exec.Command(TmuxExecutable, "list-panes", "-s", "-t", ts.Name+":", "-F", "#{window_index}:#{pane_pid}:#{window_name}")
	panes, err := cmd.Output()
	if err != nil {
		return err
	}

	for _, line := range strings.Split(string(panes), "\n") {
		parts := strings.SplitN(line, ":", 3)
		windowIndex, err := strconv.Atoi(parts[0])
		if err != nil {
			return err
		}
		// This is the special reserved window 0 to keep session alive when all procs have stopped
		if windowIndex == 0 {
			continue
		}
		pid, err := strconv.Atoi(parts[1])
		if err != nil {
			return err
		}
		windowName := parts[2]

		procGroup := ts.byWindowIndex[windowIndex]
		if procGroup != nil {
			continue
		}
		if slices.Contains(ts.suspectDead, procGroup) {
			continue
		}

		procGroup = &TmuxProcGroup{
			Unit:        ts.associatedConfig.MatchByName(windowName),
			Name:        windowName,
			WindowIndex: windowIndex,
			Pid:         pid,
			Orphan:      true,
		}
		ts.insertProcGroup(procGroup)
		fmt.Printf("polled proc group %d:%s of pid=%d\n", windowIndex, windowName, pid)
	}

	return nil
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
