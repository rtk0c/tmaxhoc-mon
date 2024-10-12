package main

import (
	"fmt"
	"net/http"
	"time"
)

func httpHandler(w http.ResponseWriter, req *http.Request) {
	// TODO
}

func main() {
	// http.HandleFunc("/", httpHandler)
	// http.ListenAndServe(":8005", nil)

	ts, err := NewTmuxSession("Minecraft")
	if err != nil {
		panic(err)
	}

	stoneblock, err := ts.SpawnService("stoneblock", "/bin/sh")
	if err != nil {
		panic(err)
	}
	ts.SendKeys(stoneblock, "echo 'Hello, world!'", "Enter")
	fmt.Printf("stoneblock: %d\n", stoneblock.Status)
	ts.SendKeys(stoneblock, "exit", "Enter")
	// Kernel/tmux/the shell is not that fast at handling the exit request. Wait a little bit
	// This is will happen in the monitor too: periodically per n seconds polling for process status
	time.Sleep(100 * time.Millisecond)
	ts.Poll()
	fmt.Printf("stoneblock: %d\n", stoneblock.Status)
	ts.Prune()
	fmt.Println(len(ts.services))
}
