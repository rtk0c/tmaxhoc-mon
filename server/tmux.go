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
	lut map[int]*TmuxProcGroup
}

type TmuxProcGroup struct {
	Name        string
	WindowIndex int
	Pid         int
	// If true, this process has exited and will be removed on the next call to [TmuxSession.Prune]
	Dead bool
	// If true, this process was parsed rather than launched by [TmuxSession.SpawnProcesses]
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

func (ss *TmuxSession) insertProcGroup(procGroup *TmuxProcGroup) {
	ss.procGroups = append(ss.procGroups, procGroup)
	ss.lut[procGroup.Pid] = procGroup
}

// windowName is an arbitary tmux window name for running the processes in.
//
// commandParts is the command to execute to starting the process. See tmux *shell_command* a full shell command for
// starting the service. For an abbreviated example, `miniserve -p 1234` results in `/bin/sh -c 'miniserv -p 1234'`,
// whereas `miniserve` `-p` `1234` results in running miniserve directly with the arguments.
//
// TODO multiple processes
func (session *TmuxSession) SpawnProcesses(windowName string, commandParts ...string) (*TmuxProcGroup, error) {
	cmd_arglist := []string{"new-window", "-t", session.Name + ":", "-n", windowName, "-P", "-F", "#{window_index}:#{pane_pid}"}
	cmd_arglist = append(cmd_arglist, commandParts...)
	cmd := exec.Command(TmuxExecutable, cmd_arglist...)
	windowInfo, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var windowIndex, pid int
	fmt.Sscanf(string(windowInfo), "%d:%d", &windowIndex, &pid)

	cmd = exec.Command(TmuxExecutable, "set-option", "-t", session.Name+":"+strconv.Itoa(windowIndex), "synchronize-panes", "on")
	err = cmd.Run()
	if err != nil {
		return nil, err
	}

	procGroup := &TmuxProcGroup{
		Name:        windowName,
		WindowIndex: windowIndex,
		Pid:         pid,
	}
	session.insertProcGroup(procGroup)
	return procGroup, nil
}

func (session *TmuxSession) Poll() error {
	for _, serv := range session.procGroups {
		err := syscall.Kill(serv.Pid, syscall.Signal(0))
		if err != nil {
			serv.Pid = 0
			serv.Dead = true
		}
	}

	cmd := exec.Command(TmuxExecutable, "list-panes", "-s", "-t", session.Name+":", "-F", "#{window_index}$#{pane_pid}$#{window_name}")
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

		procGroup := session.lut[windowIndex]
		if procGroup == nil {
			procGroup = &TmuxProcGroup{
				Name:        windowName,
				WindowIndex: windowIndex,
				Pid:         pid,
				Orphan:      true,
			}
			session.insertProcGroup(procGroup)
		}
	}

	return nil
}

func (session *TmuxSession) Prune() {
	ss := session.procGroups
	// Technically, the "last unprocessed service" but that's a long name
	lastAlive := len(ss) - 1
	i := 0
	for i <= lastAlive {
		serv := ss[i]
		if serv.Dead {
			delete(session.lut, serv.Pid)
			ss[i] = ss[lastAlive]
			lastAlive--
		} else {
			i++
		}
	}
	session.procGroups = ss[:lastAlive+1]
}

func (session *TmuxSession) SendKeys(serv *TmuxProcGroup, keys ...string) error {
	cmd_arglist := []string{"send-keys", "-t", session.Name + ":" + serv.Name}
	cmd_arglist = append(cmd_arglist, keys...)
	cmd := exec.Command(TmuxExecutable, cmd_arglist...)
	err := cmd.Run()
	if err != nil {
		return err
	}

	return nil
}
