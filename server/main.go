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
	def *UnitDefinition
	pg  *TmuxProcGroup
}

func (hpg *HttpProcGroup) IsOrphan() bool {
	return hpg.pg != nil && hpg.pg.Orphan
}

func (hpg *HttpProcGroup) IsStopped() bool {
	return hpg.pg == nil
}

func (hpg *HttpProcGroup) IsStopping() bool {
	return hpg.pg != nil && !hpg.pg.StoppingAttempt.IsZero()
}

func (hpg *HttpProcGroup) IsRunning() bool {
	return hpg.pg != nil && hpg.pg.StoppingAttempt.IsZero()
}

func (hpg *HttpProcGroup) ForceStopAllowed() bool {
	return hpg.IsStopping() && time.Since(hpg.pg.StoppingAttempt) > 10*time.Second
}

func (hpg *HttpProcGroup) Name() string {
	if hpg.def != nil {
		return hpg.def.Name
	}
	if hpg.pg != nil {
		return hpg.pg.Name
	}
	return "<unnamed>"
}

func (hpg *HttpProcGroup) Description() string {
	if hpg.def != nil {
		return hpg.def.Description
	}
	return ""
}

func (hpg *HttpProcGroup) ExtraStyles() string {
	if len(hpg.def.Color) > 0 {
		return "background-color: " + hpg.def.Color
	}
	return ""
}

func collectProcGroupInfo() []HttpProcGroup {
	modelLock.RLock()
	defer modelLock.RUnlock()

	hpg := []HttpProcGroup{}
	// Already sorted lexigraphically
	for _, unit := range conf.Units {
		pg := ts.byUnit[unit]
		if pg == nil {
			for _, unalive := range ts.suspectDead {
				if unalive.Unit == unit {
					pg = unalive
					break
				}
			}
		}

		hpg = append(hpg, HttpProcGroup{
			def: unit,
			pg:  pg,
		})
	}
	// TODO sort
	for _, procGroup := range ts.byWindowIndex {
		if procGroup.Unit == nil {
			hpg = append(hpg, HttpProcGroup{
				def: nil,
				pg:  procGroup,
			})
		}
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

	hpg := HttpProcGroup{def: unit, pg: ts.byUnit[unit]}
	if hpg.pg == nil {
		modelLock.Unlock()
		return
	}
	if force && hpg.ForceStopAllowed() {
		http.Error(w, "force kill not allowed: not enough time has passed since stopping attempt", http.StatusBadRequest)
		modelLock.Unlock()
		return
	}

	if force {
		ts.ForceKillProcGroup(hpg.pg)
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

	ts, err = NewTmuxSession(conf, conf.SessionName)
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
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir(conf.StaticFilesDir))))
	http.ListenAndServe(":8005", nil)
}
