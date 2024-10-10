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
// commancommand_partsd: see tmux *shell_command* a full shell command for starting the service.
//          For an abbreviated example: `miniserve -p 1234` results in `/bin/sh -c 'miniserv -p 1234'`,
//          whereas `miniserve` `-p` `1234` results in running miniserve directly with the arguments.
func (session *TmuxSession) SpawnService(window string, command_parts []string) {
	// TODO support multi-process services? like a multi-shard DST cluster
	/*
	cmd := exec.Command(TmuxExecutable, "set-option", "-t", session.Name + ":" + window, "synchronize-panes", "on")
	_, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	*/

	cmd := exec.Command(TmuxExecutable, "new-window", "-t", session.Name + ":" + window, command_parts...)
	_, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	session.Services = append(session.Services, Service{
		Name: window,
	})
}