package main

import (
	"fmt"
	"os/exec"
	"strconv"
	"syscall"
)

type TmuxSession struct {
	Name       string
	procGroups []*TmuxProcGroup
}

type TmuxProcGroup struct {
	Name        string
	WindowIndex int
	Pid         int
	// If true, this process has exited and will be removed on the next call to [TmuxSession.Prune]
	Dead bool
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

	serv := &TmuxProcGroup{
		Name:        windowName,
		WindowIndex: windowIndex,
		Pid:         pid,
		Dead:        false,
	}
	session.procGroups = append(session.procGroups, serv)
	return serv, nil
}

// TODO discover new windows not created by us

func (session *TmuxSession) Poll() {
	for _, serv := range session.procGroups {
		err := syscall.Kill(serv.Pid, syscall.Signal(0))
		if err != nil {
			serv.Pid = 0
			serv.Dead = true
		}
	}
}

func (session *TmuxSession) Prune() {
	ss := session.procGroups
	// Technically, the "last unprocessed service" but that's a long name
	lastAlive := len(ss) - 1
	i := 0
	for i <= lastAlive {
		serv := ss[i]
		if serv.Dead {
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
