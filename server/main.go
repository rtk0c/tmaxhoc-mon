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

func httpProcGroup(w http.ResponseWriter, pg *TmuxProcGroup) {
	fmt.Fprintf(w, `
<div id="unit.%[1]s" class="unit">
<p class="unit-name">%[1]s</p>
`, pg.Name)

	if pg.Dead {
		fmt.Fprint(w, `<span class="marker marker-running">Running</span>`)
		fmt.Fprintf(w, `
<form method="post" action="/api/stop-unit">
<input type="hidden" name="unit" value="%s">
<input type="submit" value="Stop">
</form>`, pg.Name)
	} else {
		fmt.Fprint(w, `<span class="marker marker-stopped">Stopped</span>`)
		fmt.Fprintf(w, `
<form method="post" action="/api/start-unit">
<input type="hidden" name="unit" value="%s">
<input type="submit" value="Start">
</form>`, pg.Name)
	}

	fmt.Fprintln(w, "</div>")
}

func httpProcGroups(w http.ResponseWriter) {
	modelLock.RLock()

	fmt.Fprintln(w, `<div class="unit-container">`)
	for _, procGroup := range ts.procGroups {
		httpProcGroup(w, procGroup)
	}
	fmt.Fprintln(w, `</div>`)

	modelLock.RUnlock()
}

func httpHandler(w http.ResponseWriter, req *http.Request) {
	fmt.Fprintln(w, `
<!DOCTYPE html><html>
<head>
<title>tmaxhoc</title>
<link rel="stylesheet" href="/static/css/main.css">
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
	ts.StopUnit(unit)

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
	ts.StartUnit(unit)

	fmt.Printf("got /api/stop-unit for unit=%s\n", unitName)

	http.Redirect(w, req, "/", http.StatusFound)
}

func main() {
	var err error

	unitd, err = NewUnitd()
	if err != nil {
		panic(err)
	}

	ts, err = NewTmuxSession("Minecraft")
	if err != nil {
		panic(err)
	}

	tsPollTimer := time.NewTicker(5 * time.Second)
	tsPollStop := make(chan bool)
	go func() {
		for {
			select {
			case <-tsPollTimer.C:
				modelLock.Lock()
				ts.Poll()
				ts.Prune()
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
