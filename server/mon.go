package main

import (
	"fmt"
	"os/exec"
)

type TmuxSession struct {
	Name string
	Services []Service
}

type Service struct {
	Name string
	WindowIndex int
	// TODO wtf is this when I wrote it?
	//ServiceClass string
}

var TmuxExecutable = "/bin/tmux"

func NewTmuxSession(session string) TmuxSession {
	// Dummy window to keep the session alive
	cmd := exec.Command(TmuxExecutable, "new-session", "-d", "-s", session, "/bin/sh")
	_, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("Failed to create tmux session: %w", err)
	}

	return TmuxSession{Name: session}
}

// window: an arbitary window name for running the service, uniquely identifying the service
// command_parts: see tmux *shell_command* a full shell command for starting the service.
//     For an abbreviated example: `miniserve -p 1234` results in `/bin/sh -c 'miniserv -p 1234'`,
//     whereas `miniserve` `-p` `1234` results in running miniserve directly with the arguments.
func (session *TmuxSession) SpawnService(window string, command_parts []string) {
	cmd := exec.Command(TmuxExecutable, "new-window", "-t", session.Name + ":", "-n", window, "-P", command_parts...)
	rest, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	sessionName, rest, _ := fmt.Cut(rest, ":")
	windowIndex, paneIndex, _ = fmt.Cut(rest, ".")

	cmd := exec.Command(TmuxExecutable, "set-option", "-t", session.Name + ":" + windowIndex, "synchronize-panes", "on")
	err := cmd.Run()
	if err != nil {
		return nil, err
	}

	session.Services = append(session.Services, Service{
		Name: window,
		WindowIndex: strconv.Atoi(windowIndex),
	})
}