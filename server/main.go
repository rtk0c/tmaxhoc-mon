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
	fmt.Fprintf(w, `
<div id="pg.%[1]s" class="pg pg_unit">
<p class="pg-name">%[1]s</p>
`, unit.Name)

	pg = ts.byUnit[unit]
	var status, class, action, endpoint string
	if pg != nil {
		if pg.Stopping {
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

	fmt.Fprintln(w, "</div>")
}

func httpProcGroups(w http.ResponseWriter) {
	fmt.Fprintln(w, `<div class="proc-group-container">`)
	modelLock.RLock()

	// Already sorted lexigraphically
	for _, unit := range unitd.units {
		httpUnitProcGroup(w, unit, ts.byUnit[unit])
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
	if req.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	req.ParseForm()
	unitName := req.Form.Get("unit")
	unit := unitd.unitsLut[unitName]
	if unit != nil {
		modelLock.Lock()
		ts.StartUnit(unit)
		modelLock.Unlock()
	}

	fmt.Printf("got /api/start-unit for unit=%s\n", unitName)

	http.Redirect(w, req, "/", http.StatusFound)
}

func apiStopUnit(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	req.ParseForm()
	unitName := req.Form.Get("unit")
	unit := unitd.unitsLut[unitName]
	if unit != nil {
		modelLock.Lock()
		ts.StopUnit(unit)
		modelLock.Unlock()
	}

	fmt.Printf("got /api/stop-unit for unit=%s\n", unitName)

	http.Redirect(w, req, "/", http.StatusFound)
}

func main() {
	var err error

	unitd, err = NewUnitd()
	if err != nil {
		panic(err)
	}

	ts, err = NewTmuxSession(unitd, "Minecraft")
	if err != nil {
		panic(err)
	}

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

	staticFilesDir := flag.String("static-files", ".", "Path to the directory holding static files")
	flag.Parse()
	fmt.Println(*staticFilesDir)

	http.HandleFunc("/", httpHandler)
	http.HandleFunc("/api/start-unit", apiStartUnit)
	http.HandleFunc("/api/stop-unit", apiStopUnit)
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir(*staticFilesDir))))
	http.ListenAndServe(":8005", nil)
}
