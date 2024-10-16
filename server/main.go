package main

import (
	"flag"
	"fmt"
	"net/http"
	"sync"
	"time"
)

var ts *TmuxSession
var unitd *Unitd
var modelLock sync.RWMutex

func httpOrphanProcGroup(w http.ResponseWriter, pg *TmuxProcGroup) {
	fmt.Fprintf(w, `
<div id="pg.%[1]s" class="pg pg_orphan">
<p class="pg-name">%[1]s</p>
`, pg.Name)

	// Non-unit proc group, cannot exist in a stopped state
	fmt.Fprint(w, `<span class="marker marker-running">Running</span>`)

	fmt.Fprintln(w, "</div>")
}

func httpUnitProcGroup(w http.ResponseWriter, unit *UnitDefinition /*nullable*/, pg *TmuxProcGroup) {
	unitColor := ""
	if len(unit.Color) > 0 {
		unitColor = "background-color: " + unit.Color
	}

	fmt.Fprintf(w, `
<div id="pg.%[1]s" class="pg pg_unit" style="%s">
<p class="pg-name">%[1]s</p>
`, unit.Name, unitColor)

	var status, class, action, endpoint string
	if pg != nil {
		if isStopping(pg) {
			status = "Stopping"
			class = "marker-stopping"
			action = ""
			endpoint = ""
		} else {
			status = "Running"
			class = "marker-running"
			action = "Stop"
			endpoint = "stop-unit"
		}
	} else {
		status = "Stopped"
		class = "marker-stopped"
		action = "Start"
		endpoint = "start-unit"
	}

	fmt.Fprintf(w, `<span class="marker %s">%s</span>`, class, status)
	if len(action) > 0 {
		fmt.Fprintf(w, `
<form method="post" action="/api/%s">
<input type="hidden" name="unit" value="%s">
<input type="submit" value="%s">
</form>`, endpoint, unit.Name, action)
	}
	if pg != nil && forceStopAllowed(pg) {
		fmt.Fprintf(w, `
<form method="post" action="/api/stop-unit">
<input type="hidden" name="unit" value="%s">
<input type="hidden" name="force" value="true">
<input type="submit" value="Force stop">
</form>`, unit.Name)
	}

	fmt.Fprintln(w, "</div>")
}

func httpProcGroups(w http.ResponseWriter) {
	fmt.Fprintln(w, `<div class="proc-group-container">`)
	modelLock.RLock()

	// Already sorted lexigraphically
	for _, unit := range unitd.units {
		pg := ts.byUnit[unit]
		if pg == nil {
			for _, unalive := range ts.suspectDead {
				if unalive.Unit == unit {
					pg = unalive
					break
				}
			}
		}
		httpUnitProcGroup(w, unit, pg)
	}
	// TODO sort
	for _, procGroup := range ts.byWindowIndex {
		if procGroup.Unit == nil {
			httpOrphanProcGroup(w, procGroup)
		}
	}

	modelLock.RUnlock()
	fmt.Fprintln(w, `</div>`)
}

func httpHandler(w http.ResponseWriter, req *http.Request) {
	fmt.Fprintln(w, `
<!DOCTYPE html><html>
<head>
<title>tmaxhoc</title>
<link rel="stylesheet" href="/static/css/main.css" />
</head>
<body>`)

	httpProcGroups(w)

	fmt.Fprintln(w, `
</body>
</html>`)
}

func apiStartUnit(w http.ResponseWriter, req *http.Request) {
	if len(ts.byWindowIndex) >= 1 {
		http.Error(w, `
Failed to start unit:
Cannot run more than 1 server at the same time. Please stop something else before starting this server.
Use the browser back button to go to the server panel again.`, http.StatusForbidden)
		return
	}

	unitName := req.FormValue("unit")
	unit := unitd.unitsLut[unitName]
	fmt.Printf("got /api/start-unit for unit=%s\n", unitName)

	if unit != nil {
		modelLock.Lock()
		ts.StartUnit(unit)
		modelLock.Unlock()
	}

	http.Redirect(w, req, "/", http.StatusFound)
}

func isStopping(pg *TmuxProcGroup) bool {
	return !pg.StoppingAttempt.IsZero()
}

func forceStopAllowed(pg *TmuxProcGroup) bool {
	return isStopping(pg) && time.Since(pg.StoppingAttempt) > 10*time.Second
}

func apiStopUnit(w http.ResponseWriter, req *http.Request) {
	unitName := req.FormValue("unit")
	unit := unitd.unitsLut[unitName]
	if unit == nil {
		return
	}
	fmt.Printf("got /api/stop-unit for unit=%s\n", unitName)
	procGroup := ts.byUnit[unit]

	force := false
	opt := req.FormValue("force")
	if forceStopAllowed(procGroup) {
		force = opt == "true"
	} else {
		if len(opt) > 0 {
			fmt.Println("[ERROR] not enough time has passed since stopping attempt to force kill")
			return
		}
	}

	modelLock.Lock()
	if force {
		ts.ForceKillProcGroup(procGroup)
	} else {
		ts.StopUnit(unit)
	}
	modelLock.Unlock()

	http.Redirect(w, req, "/", http.StatusFound)
}

func main() {
	var err error

	staticFilesDir := flag.String("static-files", ".", "Path to the directory holding static files")
	unidDefsFile := flag.String("unit-definitions", "", "Path to the config file for unit definitions")
	flag.Parse()

	unitd, err = NewUnitd(*unidDefsFile)
	if err != nil {
		panic(err)
	}

	ts, err = NewTmuxSession(unitd, "Minecraft")
	if err != nil {
		panic(err)
	}

	// TODO event loop, and instead of tracking a "suspect dead list", don't store newly spawned processes at all,
	//   but instead immediately queue a PollAndPrune() to detect the new proc group (and reset the timer)
	//   this way incoming requests can also trigger a PollAndPrune() if necessary to keep visitors from waiting
	//   for a state update.
	tsPollTimer := time.NewTicker(5 * time.Second)
	tsPollStop := make(chan bool)
	ts.PollAndPrune()
	go func() {
		for {
			select {
			case <-tsPollTimer.C:
				modelLock.Lock()
				ts.PollAndPrune()
				modelLock.Unlock()
			case <-tsPollStop:
				return
			}
		}
	}()

	http.HandleFunc("/", httpHandler)
	http.HandleFunc("POST /api/start-unit", apiStartUnit)
	http.HandleFunc("POST /api/stop-unit", apiStopUnit)
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir(*staticFilesDir))))
	http.ListenAndServe(":8005", nil)
}
