package main

import (
	"html/template"
	"io"
)

type frontpageData struct {
	Units []frontpageUnit
}

type frontpageUnit struct {
	Name             string
	Description      template.HTML
	Styles           template.CSS
	HasStyles        bool
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

var frontpageTemplate = template.Must(template.New("frontpage").Parse(`<!DOCTYPE html>
<html>
<head>
	<title>tmaxhoc</title>
	<link rel="stylesheet" href="/static/css/main.css" />
	<script src="/static/js/main.js"></script>
</head>
<body>
	<div id="unitsContainer">
	{{range .Units}}
		<div class="unit {{.Class}}"{{if .HasStyles}} style="{{.Styles}}"{{end}}>
			<p class="unit-name" title="{{.Tooltip}}">{{.Name}}</p>
			{{if .IsStopped}}
				<span class="marker marker-stopped">Stopped</span>
				<form class="unit-action" method="post" action="/api/start-unit">
					<input type="hidden" name="unit" value="{{.Name}}">
					<input type="submit" value="Start">
				</form>
			{{else if .IsStopping}}
				<span class="marker marker-stopping">Stopping</span>
				{{if .ForceStopAllowed}}
					<form class="unit-action" method="post" action="/api/stop-unit">
						<input type="hidden" name="unit" value="{{.Name}}">
						<input type="hidden" name="force" value="true">
						<input type="submit" value="Force stop">
					</form>
				{{end}}
			{{else if .IsRunning}}
				<span class="marker marker-running">Running</span>
				<form class="unit-action" method="post" action="/api/stop-unit">
					<input type="hidden" name="unit" value="{{.Name}}">
					<input type="submit" value="Stop">
				</form>
			{{end}}
			{{if .IsGroup}}
				<span class="c-space-around">subparts: {{.RunningSubparts}}/{{.TotalSubparts}}</span>
			{{end}}
			<div class="unit-desc">{{.Description}}</div>
		</div>
	{{end}}
	</div>
</body>
</html>
`))

func renderFrontpage(w io.Writer, unitsys *UnitSystem) error {
	return frontpageTemplate.Execute(w, newFrontpageData(unitsys))
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
		Description:      template.HTML(unit.Description),
		Styles:           template.CSS(unit.Styles),
		HasStyles:        len(unit.Styles) > 0,
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
