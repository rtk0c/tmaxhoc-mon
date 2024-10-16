package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

type UnitDefinition struct {
	Name         string
	Color        string
	startCommand []string
	stopCommand  []string
}

type Unitd struct {
	// List of units, lexigraphically sorted by [Unit.Name].
	// Immutable after load.
	units []*UnitDefinition
	// Lookup table from [Unit.Name] to the [Unit] itself.
	// Immutable after load.
	unitsLut map[string]*UnitDefinition
}

func handleOptionEntry(unit *UnitDefinition, key string, value []string) {
	if unit == nil {
		// TODO handle global option
		return
	}

	switch key {
	case "Color":
		if len(value) != 1 {
			panic("Invalid color '" + strings.Join(value, "\\") + "'")
		}
		unit.Color = value[0]
	case "StartCommand":
		unit.startCommand = value
	case "StopCommand":
		unit.stopCommand = value
	default:
		fmt.Println("[ERROR] invalid config option " + key)
	}
}

func NewUnitd(unitDefsFile string) (*Unitd, error) {
	r := &Unitd{
		units:    make([]*UnitDefinition, 0),
		unitsLut: make(map[string]*UnitDefinition),
	}

	f, err := os.Open(unitDefsFile)
	if err != nil {
		panic(err)
	}
	fbuf := bufio.NewScanner(f)
	var currUnit *UnitDefinition
	var currKey string
	var currValue []string
	hasKVPair := false
	for fbuf.Scan() {
		line := fbuf.Text()

		// Skip empty lines
		if len(line) == 0 {
			continue
		}

		// New section
		rest := line
		rest, found1 := strings.CutPrefix(rest, "[")
		rest, found2 := strings.CutSuffix(rest, "]")
		if found1 && found2 {
			if hasKVPair {
				handleOptionEntry(currUnit, currKey, currValue)
			}
			currUnit = &UnitDefinition{Name: rest}
			r.units = append(r.units, currUnit)
			r.unitsLut[currUnit.Name] = currUnit
			hasKVPair = false
			continue
		}

		// Multiline values
		// Append to current K/V pair
		if hasKVPair {
			rest, hasIndent := strings.CutPrefix(line, "  ")
			if hasIndent {
				currValue = append(currValue, strings.TrimSpace(rest))
				continue
			}
		}
		// New K/V pair
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			// Push previous K/V pair
			// For the last K/V pair before the first section, this will be pushed by the
			if hasKVPair {
				handleOptionEntry(currUnit, currKey, currValue)
			}
			currKey = strings.TrimSpace(parts[0])
			currValue = []string{strings.TrimSpace(parts[1])}
			hasKVPair = true
			continue
		}

		// Some other thing
	}
	if hasKVPair {
		handleOptionEntry(currUnit, currKey, currValue)
	}

	return r, nil
}

// Nullable
func (unitd *Unitd) MatchByName(name string) *UnitDefinition {
	// TODO better fuzzy matching algorithm; ideas:
	// - case insensitive matching
	// - whitespace/-/_ insensitive matching
	// - allow unit names to be supplied as a regex, and return a list of candidates?
	// - some kind of fuzzy scoring algorithm like fzf/sublime text's command palette
	return unitd.unitsLut[name]
}
