package main

import (
	"fmt"
	"os/exec"
	"strconv"
	"syscall"
)

type TmuxSession struct {
	Name     string
	services []*TmuxService
}

type ServiceStatus int

const (
	SS_Stopped ServiceStatus = iota
	SS_Running
)

type TmuxService struct {
	Name        string
	WindowIndex int
	Pid         int
	Status      ServiceStatus
	// TODO wtf is this when I wrote it?
	//ServiceClass string
}

var TmuxExecutable = "/bin/tmux"

func NewTmuxSession(session string) (*TmuxSession, error) {
	// Dummy window to keep the session alive
	cmd := exec.Command(TmuxExecutable, "new-session", "-d", "-s", session, "/bin/sh")
	_, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to create tmux session: %w", err)
	}

	return &TmuxSession{Name: session}, nil
}

// window: an arbitary window name for running the service, uniquely identifying the service
// command_parts: see tmux *shell_command* a full shell command for starting the service.
//
//	For an abbreviated example: `miniserve -p 1234` results in `/bin/sh -c 'miniserv -p 1234'`,
//	whereas `miniserve` `-p` `1234` results in running miniserve directly with the arguments.
func (session *TmuxSession) SpawnService(window string, command_parts ...string) (*TmuxService, error) {
	cmd_arglist := []string{"new-window", "-t", session.Name + ":", "-n", window, "-P", "-F", "#{window_index}:#{pane_pid}"}
	cmd_arglist = append(cmd_arglist, command_parts...)
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

	serv := &TmuxService{
		Name:        window,
		WindowIndex: windowIndex,
		Pid:         pid,
		Status:      SS_Running,
	}
	session.services = append(session.services, serv)
	return serv, nil
}

// TODO discover new windows not created by us

func (session *TmuxSession) Poll() {
	for _, serv := range session.services {
		err := syscall.Kill(serv.Pid, syscall.Signal(0))
		if err != nil {
			serv.Pid = 0
			serv.Status = SS_Stopped
		}
	}
}

func (session *TmuxSession) Prune() {
	ss := session.services
	// Technically, the "last unprocessed service" but that's a long name
	lastAlive := len(ss) - 1
	i := 0
	for i <= lastAlive {
		serv := ss[i]
		if serv.Status == SS_Stopped {
			ss[i] = ss[lastAlive]
			lastAlive--
		} else {
			i++
		}
	}
	session.services = ss[:lastAlive+1]
}

func (session *TmuxSession) SendKeys(serv *TmuxService, keys ...string) error {
	cmd_arglist := []string{"send-keys", "-t", session.Name + ":" + serv.Name}
	cmd_arglist = append(cmd_arglist, keys...)
	cmd := exec.Command(TmuxExecutable, cmd_arglist...)
	err := cmd.Run()
	if err != nil {
		return err
	}

	return nil
}
