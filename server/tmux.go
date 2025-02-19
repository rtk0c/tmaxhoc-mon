package main

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

type TmuxSession struct {
	SessionName string

	// LUT from [TmuxProcess.PaneId] to proc groups.
	// Every proc group managed by this session must be inside this map.
	byPaneId map[int]*TmuxProcess

	onProcSpawned func(*TmuxProcess)
	onProcPruned  func(*TmuxProcess)

	// The special reserved window 0 to keep session alive when all procs have stopped
	reservedWindowPaneId int
}

func (ts *TmuxSession) targetSession() string {
	return ts.SessionName + ":"
}

type TmuxProcess struct {
	Name string

	// Unique pane id in the form of '%<int>' identifying the pane
	PaneId int
	// PID of the process running in this pane
	Pid int

	// If true, this process has exited and been removed by [TmuxSession.PollAndPrune].
	// This field is for let library users to drop the reference to this proc group after it died,
	// without having to do a lookup in the [TmuxService].
	Dead bool

	// If true, this process was parsed rather than launched by [TmuxSession.SpawnProcesses].
	// Note that this property is orthogonal to [TmuxProcess.Unit];
	// an adopted proc group may have an associated unit, and a non-adopted proc group may not have an associated unit.
	Adopted bool
}

func (proc *TmuxProcess) targetPane() string {
	return "%" + strconv.Itoa(proc.PaneId)
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

	ts := &TmuxSession{
		SessionName: sessionName,

		byPaneId: make(map[int]*TmuxProcess),

		reservedWindowPaneId: -1,
	}

	cmd = exec.Command(TmuxExecutable, "list-panes", "-s", "-t", ts.targetSession(), "-F", "#{window_index}\t#{pane_id}")
	panes, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(string(panes), "\n") {
		var windowIndex, paneId int
		fmt.Sscanf(line, "%d\t%%%d", &windowIndex, &paneId)
		if windowIndex == 0 {
			ts.reservedWindowPaneId = paneId
			break
		}
	}
	if ts.reservedWindowPaneId == -1 {
		fmt.Println("[WARN] no reserved window present in the session")
	}

	return ts, nil
}

func (ts *TmuxSession) addProcess(proc *TmuxProcess) {
	ts.byPaneId[proc.PaneId] = proc
	ts.onProcSpawned(proc)
}

func (ts *TmuxSession) removeProcess(proc *TmuxProcess) {
	delete(ts.byPaneId, proc.PaneId)
	proc.Dead = true
	ts.onProcPruned(proc)
}

// windowName is an arbitary tmux window name for running the processes in.
//
// commandParts is the command to execute to starting the process. See tmux *shell_command* a full shell command for
// starting the service. For an abbreviated example, `miniserve -p 1234` results in `/bin/sh -c 'miniserv -p 1234'`,
// whereas `miniserve` `-p` `1234` results in running miniserve directly with the arguments.
func (ts *TmuxSession) spawnProcess(windowName string, commandParts ...string) (*TmuxProcess, error) {
	cmdArglist := []string{"new-window", "-t", ts.targetSession(), "-n", windowName, "-P", "-F", "#{pane_id}\t#{pane_pid}"}
	cmdArglist = append(cmdArglist, commandParts...)
	cmd := exec.Command(TmuxExecutable, cmdArglist...)
	info, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var paneId, pid int
	fmt.Sscanf(string(info), "%%%d\t%d", &paneId, &pid)

	proc := &TmuxProcess{
		Name:   windowName,
		PaneId: paneId,
		Pid:    pid,
	}
	ts.addProcess(proc)

	fmt.Printf("spawned proc group %%%d pid=%d '%s'\n", paneId, pid, windowName)
	return proc, nil
}

func (ts *TmuxSession) spawnByScript(windowName string, script string, args ...string) error {
	args = append(args, ts.SessionName)
	cmd := exec.Command(script, args...)
	stdout, err := cmd.Output()
	if err != nil {
		return err
	}

	for _, line := range strings.Split(string(stdout), "\n") {
		var paneId, pid int
		fmt.Sscanf(line, "%%%d\t%d", &paneId, &pid)

		proc := &TmuxProcess{
			Name:   windowName,
			PaneId: paneId,
			Pid:    pid,
		}
		ts.addProcess(proc)
	}

	return nil
}

func (ts *TmuxSession) ForceKillProcess(proc *TmuxProcess) {
	err := syscall.Kill(proc.Pid, syscall.SIGKILL)
	if err != nil {
		fmt.Printf("[ERROR] failed to force kill pid=%d: %s\n", proc.Pid, err)
	}
}

func (ts *TmuxSession) PollAndPrune() error {
	//// Detect dead proc groups, and prune them ////
	for _, proc := range ts.byPaneId {
		err := syscall.Kill(proc.Pid, syscall.Signal(0))
		if err != nil {
			fmt.Printf("removing dead proc group %%%d pid=%d '%s'\n", proc.PaneId, proc.Pid, proc.Name)
			ts.removeProcess(proc)
		}
	}

	//// Poll for newly created windows by somebody else, keep records and try to map them to units ////
	cmd := exec.Command(TmuxExecutable, "list-panes", "-s", "-t", ts.targetSession(), "-F", "#{pane_id}\t#{pane_pid}\t#{window_name}")
	panes, err := cmd.Output()
	if err != nil {
		return err
	}
	for _, line := range strings.Split(string(panes), "\n") {
		var paneId, pid int
		var windowName string
		{
			parts := strings.Split(line, "\t")
			if len(parts) != 3 {
				continue
			}
			paneId, _ = strconv.Atoi(parts[0][1:]) // %123
			pid, _ = strconv.Atoi(parts[1])
			windowName = parts[2]
		}

		if paneId == ts.reservedWindowPaneId {
			continue
		}
		_, exists := ts.byPaneId[paneId]
		if exists {
			continue
		}

		proc := &TmuxProcess{
			Name:    windowName,
			PaneId:  paneId,
			Pid:     pid,
			Adopted: true,
		}
		ts.addProcess(proc)

		fmt.Printf("polled proc group %%%d pid=%d '%s'\n", paneId, pid, windowName)
	}

	return nil
}

func (ts *TmuxSession) SendKeys(proc *TmuxProcess, keys ...string) error {
	cmdArglist := []string{"send-keys", "-t", proc.targetPane()}
	cmdArglist = append(cmdArglist, keys...)
	cmd := exec.Command(TmuxExecutable, cmdArglist...)
	err := cmd.Run()
	if err != nil {
		return err
	}

	return nil
}
