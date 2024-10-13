package main

import (
	"flag"
	"fmt"
	"net/http"
	"sync"
	"time"
)

var registar *UnitRegistrar
var regLock sync.RWMutex

func httpUnit(w http.ResponseWriter, u *Unit) {
	fmt.Fprintf(w, `
<div id="unit.%[1]s" class="unit">
<span class="unit-name">%[1]s</span>
</div>
`, u.Name)
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
</head>
<body>`)

	httpDisplayRegistrar(w)

	fmt.Fprintln(w, `
</body>
</html>`)
}

func main() {
	ts, err := NewTmuxSession("Minecraft")
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
			case <-tsPollStop:
				return
			}
		}
	}()

	registar, err = NewUnitRegistrar(ts)
	if err != nil {
		panic(err)
	}

	staticFilesDir := flag.String("static-files", ".", "Path to the directory holding static files")
	flag.Parse()
	fmt.Println(*staticFilesDir)

	http.HandleFunc("/", httpHandler)
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir(*staticFilesDir))))
	http.ListenAndServe(":8005", nil)
}
