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
	// LUT from [TmuxProcess.WindowIndex] to proc groups.
	// Every proc group managed by this session must be inside this map.
	byWindowIndex map[int]*TmuxProcess
	// Proc groups that are about to die or is evident to be dead by a conflicting window index.
	suspectDead []*TmuxProcess

	onProcSpawned func(*TmuxProcess)
	onProcPruned  func(*TmuxProcess)
}

type TmuxProcess struct {
	Name        string
	WindowIndex int
	Pid         int
	// If non-zero, a stop command has been issued but we're not sure it has died.
	StoppingAttempt time.Time
	// If true, this process has exited and been removed by [TmuxSession.PollAndPrune].
	// This field is for let library users to drop the reference to this proc group after it died,
	// without having to do a lookup in the [TmuxService].
	Dead bool
	// If true, this process was parsed rather than launched by [TmuxSession.SpawnProcesses].
	// Note that this property is orthogonal to [TmuxProcess.Unit];
	// an adopted proc group may have an associated unit, and a non-adopted proc group may not have an associated unit.
	Adopted bool
}

var TmuxExecutable = "/bin/tmux"

func NewTmuxSession(sessionName string) (*TmuxSession, error) {
	cmd := exec.Command(TmuxExecutable, "has-session", "-t", sessionName)
	err := cmd.Run()
	if err != nil {
		// Dummy window to keep the session alive
		cmd := exec.Command(TmuxExecutable, "new-session", "-d", "-s", sessionName, "/bin/sh")
		_, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("failed to create tmux session: %w", err)
		}
	}

	return &TmuxSession{
		Name:          sessionName,
		byWindowIndex: make(map[int]*TmuxProcess),
	}, nil
}

func (ts *TmuxSession) insertProcGroup(proc *TmuxProcess) {
	old := ts.byWindowIndex[proc.WindowIndex]
	if old != nil {
		// Not needed. Insertion below overrides it anyways.
		/* delete(ts.byWindowIndex, old.WindowIndex) */
		ts.suspectDead = append(ts.suspectDead, old)
	}

	ts.byWindowIndex[proc.WindowIndex] = proc

	ts.onProcSpawned(proc)
}

func (ts *TmuxSession) removeProcGroup(proc *TmuxProcess) {
	// In case of [TmuxSession.markSuspectDead] being called, this does nothing
	// In case the process died on its own, it will still be in the window index lookup table
	delete(ts.byWindowIndex, proc.WindowIndex)

	proc.Dead = true
	ts.onProcPruned(proc)
}

func (ts *TmuxSession) markSuspectDead(proc *TmuxProcess) {
	delete(ts.byWindowIndex, proc.WindowIndex)
	ts.suspectDead = append(ts.suspectDead, proc)
	proc.StoppingAttempt = time.Now()
}

// windowName is an arbitary tmux window name for running the processes in.
//
// commandParts is the command to execute to starting the process. See tmux *shell_command* a full shell command for
// starting the service. For an abbreviated example, `miniserve -p 1234` results in `/bin/sh -c 'miniserv -p 1234'`,
// whereas `miniserve` `-p` `1234` results in running miniserve directly with the arguments.
func (ts *TmuxSession) spawnProcess(windowName string, commandParts ...string) (*TmuxProcess, error) {
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

	proc := &TmuxProcess{
		Name:        windowName,
		WindowIndex: windowIndex,
		Pid:         pid,
	}
	ts.insertProcGroup(proc)
	fmt.Printf("spawned proc group %d:%s of pid=%d\n", windowIndex, windowName, pid)
	return proc, nil
}

func (ts *TmuxSession) ForceKillProcGroup(proc *TmuxProcess) {
	err := syscall.Kill(proc.Pid, syscall.SIGKILL)
	if err != nil {
		fmt.Printf("[ERROR] failed to force kill pid=%d: %s\n", proc.Pid, err)
	}
}

func (ts *TmuxSession) PollAndPrune() error {
	//// Detect dead proc groups, and prune them ////
	for _, proc := range ts.byWindowIndex {
		err := syscall.Kill(proc.Pid, syscall.Signal(0))
		if err != nil {
			fmt.Printf("removing dead proc group %d:%s of pid=%d\n", proc.WindowIndex, proc.Name, proc.Pid)
			ts.removeProcGroup(proc)
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
			ts.removeProcGroup(suspect)
			ts.suspectDead[i] = ts.suspectDead[lastAlive]
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

		proc := ts.byWindowIndex[windowIndex]
		if proc != nil {
			continue
		}
		if slices.Contains(ts.suspectDead, proc) {
			continue
		}

		proc = &TmuxProcess{
			Name:        windowName,
			WindowIndex: windowIndex,
			Pid:         pid,
			Adopted:     true,
		}
		ts.insertProcGroup(proc)
		fmt.Printf("polled proc group %d:%s of pid=%d\n", windowIndex, windowName, pid)
	}

	return nil
}

func (session *TmuxSession) SendKeys(serv *TmuxProcess, keys ...string) error {
	cmdArglist := []string{"send-keys", "-t", session.Name + ":" + serv.Name}
	cmdArglist = append(cmdArglist, keys...)
	cmd := exec.Command(TmuxExecutable, cmdArglist...)
	err := cmd.Run()
	if err != nil {
		return err
	}

	return nil
}
