package main

import (
	"fmt"
	"net/http"
)

func httpHandler(w http.ResponseWriter, req *http.Request) {
	// TODO
}

func main() {
	http.HandlerFunc("/", httpHandler)
	http.Listen(":8005", nil)

	ts := NewTmuxSession("Minecraft")
	ts.SpawnService("stoneblock", [...]string{"/bin/sh"})
}