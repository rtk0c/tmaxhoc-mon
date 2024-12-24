package main

import (
	"flag"
	"fmt"
	"net/http"
	"sync"
	"text/template"
	"time"
)

var conf *Config
var ts *TmuxSession
var modelLock sync.RWMutex

// We specifically want text/template; any "malicious code" would involve being on the other side of the airtight hatchway
// because all params come from either unit definitions, or tmux window names;
// access to either already implies arbitary code execution capability
//
// This also enables the unit definitions to have HTML like <b>bold</b> in descriptions, etc.
var frontpage *template.Template

type HttpProcGroup struct {
	def  *UnitDefinition
	proc *TmuxProcess
}

// Implies running proc group
func (hpg *HttpProcGroup) IsAdopted() bool {
	return hpg.proc != nil && hpg.proc.Adopted
}

func (hpg *HttpProcGroup) IsStopped() bool {
	return hpg.proc == nil
}

func (hpg *HttpProcGroup) IsStopping() bool {
	return hpg.proc != nil && !hpg.proc.StoppingAttempt.IsZero()
}

func (hpg *HttpProcGroup) IsRunning() bool {
	return hpg.proc != nil && hpg.proc.StoppingAttempt.IsZero()
}

func (hpg *HttpProcGroup) ForceStopAllowed() bool {
	return hpg.IsStopping() && time.Since(hpg.proc.StoppingAttempt) > 10*time.Second
}

func (hpg *HttpProcGroup) Name() string {
	if hpg.def != nil {
		return hpg.def.Name
	}
	if hpg.proc != nil {
		return hpg.proc.Name
	}
	panic("BUG: HttpProcGroup cannot have neither definition nor proc group")
}

func (hpg *HttpProcGroup) Description() string {
	if hpg.def != nil {
		return hpg.def.Description
	}
	return ""
}

func (hpg *HttpProcGroup) UserDefinedAttributes() string {
	if hpg.def != nil && len(hpg.def.Styles) > 0 {
		return "style=\"" + hpg.def.Styles + "\""
	}
	return ""
}

func collectProcGroupInfo() []HttpProcGroup {
	modelLock.RLock()
	defer modelLock.RUnlock()

	hpg := []HttpProcGroup{}
	// Already in display order
	for _, unit := range conf.Units {
		if unit.Hidden {
			continue
		}

		proc := ts.byUnit[unit]
		if proc == nil {
			for _, unalive := range ts.suspectDead {
				if unalive.Unit == unit {
					proc = unalive
					break
				}
			}
		}

		hpg = append(hpg, HttpProcGroup{
			def:  unit,
			proc: proc,
		})
	}

	return hpg
}

func httpHandler(w http.ResponseWriter, req *http.Request) {
	data := collectProcGroupInfo()
	err := frontpage.Execute(w, data)
	if err != nil {
		panic(err)
	}
}

func apiStartUnit(w http.ResponseWriter, req *http.Request) {
	if conf.MaxUnits > 0 && len(ts.byWindowIndex) >= conf.MaxUnits {
		http.Error(w, `
Failed to start unit:
Cannot run more than 1 server at the same time. Please stop something else before starting this server.
Use the browser back button to go to the server panel again.`, http.StatusForbidden)
		return
	}

	unitName := req.FormValue("unit")
	unit := conf.unitsLut[unitName]
	fmt.Printf("got /api/start-unit for unit=%s\n", unitName)

	if unit != nil {
		modelLock.Lock()
		ts.StartUnit(unit)
		modelLock.Unlock()
	}

	http.Redirect(w, req, "/", http.StatusFound)
}

func apiStopUnit(w http.ResponseWriter, req *http.Request) {
	unitName := req.FormValue("unit")
	unit := conf.unitsLut[unitName]
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

	hpg := HttpProcGroup{def: unit, proc: ts.byUnit[unit]}
	if hpg.proc == nil {
		modelLock.Unlock()
		return
	}
	if force && hpg.ForceStopAllowed() {
		http.Error(w, "force kill not allowed: not enough time has passed since stopping attempt", http.StatusBadRequest)
		modelLock.Unlock()
		return
	}

	if force {
		ts.ForceKillProcGroup(hpg.proc)
	} else {
		ts.StopUnit(unit)
	}
	modelLock.Unlock()
	http.Redirect(w, req, "/", http.StatusFound)
}

func main() {
	var err error

	configFile := flag.String("config", "config.toml", "Path to the config file")
	flag.Parse()

	conf, err = NewConfig(*configFile)
	if err != nil {
		panic(err)
	}

	ts, err = NewTmuxSession(conf.SessionName)
	if err != nil {
		panic(err)
	}

	res, err := template.ParseFiles(conf.FrontpageTemplate)
	if err != nil {
		panic(err)
	}
	frontpage = res

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
	http.Handle("/static/", http.FileServer(http.Dir(conf.StaticFilesDir)))
	http.ListenAndServe(":8005", nil)
}
