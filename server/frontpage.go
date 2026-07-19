package main

import (
	"io"
	"path/filepath"
	"text/template"
)

var frontpage *template.Template

type frontpageData struct {
	Units []frontpageUnit
}

type frontpageUnit struct {
	Name             string
	Description      string
	UserDefinedAttrs string
	Class            string
	Tooltip          string
	IsStopped        bool
	IsStopping       bool
	IsRunning        bool
	ForceStopAllowed bool
	IsGroup          bool
	RunningSubparts  int
	TotalSubparts    int
}

func parseFrontpageTemplate(unitsys *UnitSystem) (*template.Template, error) {
	return template.ParseFiles(filepath.Join(unitsys.StaticFilesDir, "index.html"))
}

func renderFrontpage(w io.Writer, unitsys *UnitSystem) error {
	return frontpage.Execute(w, newFrontpageData(unitsys))
}

func newFrontpageData(unitsys *UnitSystem) frontpageData {
	data := frontpageData{
		Units: make([]frontpageUnit, 0, len(unitsys.units)),
	}

	for _, unit := range unitsys.units {
		if unit.Hidden {
			continue
		}
		data.Units = append(data.Units, newFrontpageUnit(unit))
	}

	return data
}

func newFrontpageUnit(unit *Unit) frontpageUnit {
	status := unit.v.status()
	view := frontpageUnit{
		Name:             unit.Name,
		Description:      unit.Description,
		UserDefinedAttrs: userDefinedAttributes(unit),
		IsStopped:        status == Stopped,
		IsStopping:       status == Stopping,
		IsRunning:        status == Running,
		ForceStopAllowed: unit.v.forceStopAllowed(),
	}

	switch v := unit.v.(type) {
	case *Unitv4Service:
		view.Class = "unitservice"
		view.Tooltip = "A standalone service"
	case *Unitv4Group:
		view.Class = "unitgroup"
		view.Tooltip = "Many subpart services grouped together"
		view.IsGroup = true
		view.RunningSubparts = v.numReqsRunning()
		view.TotalSubparts = len(v.requirements)
	}

	return view
}

func userDefinedAttributes(unit *Unit) string {
	if len(unit.Styles) > 0 {
		return `style="` + unit.Styles + `"`
	}
	return ""
}
