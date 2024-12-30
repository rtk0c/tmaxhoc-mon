//go:generate templ generate

package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"sync"
	"time"
)

var unitsys *UnitSystem
var ts *TmuxSession
var modelLock sync.RWMutex

func httpHandler(w http.ResponseWriter, req *http.Request) {
	modelLock.RLock()
	defer modelLock.RUnlock()

	component := compFrontpage(unitsys)
	component.Render(context.Background(), w)
}

func apiStartUnit(w http.ResponseWriter, req *http.Request) {
	if unitsys.MaxUnits > 0 && unitsys.RunningServicesCount() >= unitsys.MaxUnits {
		http.Error(w, fmt.Sprintf(`
Failed to start unit:
Cannot run more than %d server at the same time. Please stop something else before starting this server.
Use the browser back button to go to the server panel again.`, unitsys.MaxUnits), http.StatusForbidden)
		return
	}

	unitName := req.FormValue("unit")
	unit := unitsys.unitsLut[unitName]
	fmt.Printf("got /api/start-unit for unit=%s\n", unitName)

	if unit != nil {
		modelLock.Lock()
		unit.driver.start(ts)
		modelLock.Unlock()
	}

	http.Redirect(w, req, "/", http.StatusFound)
}

func apiStopUnit(w http.ResponseWriter, req *http.Request) {
	unitName := req.FormValue("unit")
	unit := unitsys.unitsLut[unitName]
	fmt.Printf("got /api/stop-unit for unit=%s\n", unitName)
	if unit == nil {
		return
	}

	force := false
	forceOpt := req.FormValue("force")
	if forceOpt == "true" {
		force = true
	} else if forceOpt == "" {
		force = false
	} else {
		http.Error(w, "invalid option: force='"+forceOpt+"', accepted '' or 'true'", http.StatusBadRequest)
		return
	}

	modelLock.Lock()
	// wasting some time per request, since call to Redirect()/Error() doesn't need to be locked
	// but doesn't really matter
	defer modelLock.Unlock()

	if force {
		// TODO somehow abstract this away in virtual methods?
		switch unit.driver.(type) {
		case *ServiceUnit:
			d := unit.driver.(*ServiceUnit)
			if d.forceStopAllowed() {
				d.forceStop(ts)
				http.Redirect(w, req, "/", http.StatusFound)
			} else {
				http.Error(w, "force kill not allowed: not enough time has passed since stopping attempt", http.StatusBadRequest)
			}
		case *GroupUnit:
			http.Error(w, "force kill not allowed on target units", http.StatusBadRequest)
		}
		return
	}

	unit.driver.stop(ts)
	http.Redirect(w, req, "/", http.StatusFound)
}

func main() {
	var err error

	configFile := flag.String("config", "config.toml", "Path to the config file")
	flag.Parse()

	unitsys, err = NewUnitSystemFromConfig(*configFile)
	if err != nil {
		panic(err)
	}

	ts, err = NewTmuxSession(unitsys.SessionName)
	if err != nil {
		panic(err)
	}

	unitsys.BindTmuxSession(ts)

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
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir(unitsys.StaticFilesDir))))
	http.ListenAndServe(":8005", nil)
}
