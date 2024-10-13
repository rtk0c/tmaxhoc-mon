package main

import (
	"flag"
	"fmt"
	"net/http"
	"sync"
	"time"
)

var ts *TmuxSession
var registar *UnitRegistrar
var regLock sync.RWMutex

func httpUnit(w http.ResponseWriter, u *Unit) {
	fmt.Fprintf(w, `
<div id="unit.%[1]s" class="unit">
<p class="unit-name">%[1]s</p>
`, u.Name)

	switch u.Status {
	case US_Running:
		fmt.Fprint(w, `<span class="marker marker-running">Running</span>`)
		fmt.Fprintf(w, `
<form method="post" action="/api/stop-unit">
<input type="hidden" name="unit" value="%s">
<input type="submit" value="Stop">
</form>`, u.Name)
	case US_Stopped:
		fmt.Fprint(w, `<span class="marker marker-stopped">Stopped</span>`)
		fmt.Fprintf(w, `
<form method="post" action="/api/start-unit">
<input type="hidden" name="unit" value="%s">
<input type="submit" value="Start">
</form>`, u.Name)
	}

	fmt.Fprintln(w, "</div>")
}

func httpDisplayRegistrar(w http.ResponseWriter) {
	regLock.RLock()

	fmt.Fprintln(w, `<div class="unit-container">`)
	for _, unit := range registar.units {
		httpUnit(w, unit)
	}
	fmt.Fprintln(w, `</div>`)

	regLock.RUnlock()
}

func httpHandler(w http.ResponseWriter, req *http.Request) {
	fmt.Fprintln(w, `
<!DOCTYPE html><html>
<head>
<title>tmaxhoc</title>
<link rel="stylesheet" href="/static/css/main.css">
</head>
<body>`)

	httpDisplayRegistrar(w)

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
	unit := registar.unitsLut[unitName]
	registar.StopUnit(unit)

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
	unit := registar.unitsLut[unitName]
	registar.StartUnit(unit)

	fmt.Printf("got /api/stop-unit for unit=%s\n", unitName)

	http.Redirect(w, req, "/", http.StatusFound)
}

func main() {
	var err error

	ts, err = NewTmuxSession("Minecraft")
	if err != nil {
		panic(err)
	}

	registar, err = NewUnitRegistrar(ts)
	if err != nil {
		panic(err)
	}

	tsPollTimer := time.NewTicker(5 * time.Second)
	tsPollStop := make(chan bool)
	go func() {
		for {
			select {
			case <-tsPollTimer.C:
				ts.Poll()
				ts.Prune()
				regLock.Lock()
				registar.MapFromTmux()
				regLock.Unlock()
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
