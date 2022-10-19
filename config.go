// config.go
// Copyright(c) 2022 Matt Pharr, licensed under the GNU Public License, Version 3.
// SPDX: GPL-3.0-only

package main

import (
	"bufio"
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"time"

	"github.com/mmp/imgui-go/v4"
)

// Things that apply to all configs
type GlobalConfig struct {
	SectorFile   string
	PositionFile string
	NotesFile    string
	AliasesFile  string

	VatsimName     string
	VatsimCID      string
	VatsimPassword string
	VatsimRating   NetworkRating
	CustomServers  map[string]string

	PositionConfigs       map[string]*PositionConfig
	ActivePosition        string
	ColorSchemes          map[string]*ColorScheme
	InitialWindowSize     [2]int
	InitialWindowPosition [2]int
	ImGuiSettings         string
	AudioSettings         AudioSettings

	aliases map[string]string

	notesRoot *NotesNode
}

type NotesNode struct {
	title    string
	text     []string
	children []*NotesNode
}

type PositionConfig struct {
	ColorSchemeName string
	ActiveAirports  map[string]interface{}
	DisplayRoot     *DisplayNode
	SplitLineWidth  int32

	VatsimCallsign        string
	VatsimFacility        Facility
	PrimaryRadarCenter    string
	SecondaryRadarCenters [3]string
	RadarRange            int32
	PrimaryFrequency      Frequency

	radioPrimed bool

	todos  []ToDoReminderItem
	timers []TimerReminderItem

	mit              []*Aircraft
	selectedAircraft *Aircraft

	highlightedLocation        Point2LL
	highlightedLocationEndTime time.Time
	drawnRoute                 string
	drawnRouteEndTime          time.Time
}

// Some UI state that needs  to stick around
var (
	selectedServer   string
	newServerName    string
	newServerAddress string
)

func (c *GlobalConfig) DrawUI() {
	if imgui.BeginTableV("GlobalFiles", 4, 0, imgui.Vec2{}, 0) {
		imgui.TableNextRow()
		imgui.TableNextColumn()
		imgui.Text("Sector file: ")
		imgui.TableNextColumn()
		imgui.Text(c.SectorFile)
		imgui.TableNextColumn()
		if imgui.Button("New...##sectorfile") {
			ui.openSectorFileDialog.Activate()
		}
		imgui.TableNextColumn()
		if c.SectorFile != "" && imgui.Button("Reload##sectorfile") {
			_ = database.LoadSectorFile(c.SectorFile)
		}

		imgui.TableNextRow()
		imgui.TableNextColumn()
		imgui.Text("Position file: ")
		imgui.TableNextColumn()
		imgui.Text(c.PositionFile)
		imgui.TableNextColumn()
		if imgui.Button("New...##positionfile") {
			ui.openPositionFileDialog.Activate()
		}
		imgui.TableNextColumn()
		if c.PositionFile != "" && imgui.Button("Reload##positionfile") {
			_ = database.LoadPositionFile(c.PositionFile)
		}

		imgui.TableNextRow()
		imgui.TableNextColumn()
		imgui.Text("Aliases file: ")
		imgui.TableNextColumn()
		imgui.Text(c.AliasesFile)
		imgui.TableNextColumn()
		if imgui.Button("New...##aliasesfile") {
			ui.openAliasesFileDialog.Activate()
		}
		imgui.TableNextColumn()
		if c.AliasesFile != "" && imgui.Button("Reload##aliasesfile") {
			c.LoadAliasesFile()
		}

		imgui.TableNextRow()
		imgui.TableNextColumn()
		imgui.Text("Notes file: ")
		imgui.TableNextColumn()
		imgui.Text(c.NotesFile)
		imgui.TableNextColumn()
		if imgui.Button("New...##notesfile") {
			ui.openNotesFileDialog.Activate()
		}
		imgui.TableNextColumn()
		if c.NotesFile != "" && imgui.Button("Reload##notesfile") {
			c.LoadNotesFile()
		}

		imgui.EndTable()
	}

	imgui.Separator()
	imgui.Text("Custom servers")
	flags := imgui.TableFlagsBordersH | imgui.TableFlagsBordersOuterV | imgui.TableFlagsRowBg | imgui.TableFlagsScrollY
	if imgui.BeginTableV("##customServers", 2, flags, imgui.Vec2{300, 100}, 0.0) {
		imgui.TableSetupColumn("Name")
		imgui.TableSetupColumn("Address")
		imgui.TableHeadersRow()
		for _, server := range SortedMapKeys(globalConfig.CustomServers) {
			imgui.TableNextRow()
			imgui.TableNextColumn()
			if imgui.SelectableV(server, server == selectedServer, imgui.SelectableFlagsSpanAllColumns, imgui.Vec2{}) {
				selectedServer = server
			}
			imgui.TableNextColumn()
			imgui.Text(globalConfig.CustomServers[server])
		}
		imgui.EndTable()
	}

	add := func() {
		globalConfig.CustomServers[newServerName] = newServerAddress
		newServerName = ""
		newServerAddress = ""
	}

	inputFlags := imgui.InputTextFlagsEnterReturnsTrue
	if imgui.InputTextV("Name", &newServerName, inputFlags, nil) {
		if newServerName != "" && newServerAddress != "" {
			add()
			imgui.SetKeyboardFocusHereV(-1)
		}
	}
	if imgui.InputTextV("Address", &newServerAddress, inputFlags, nil) {
		if newServerName != "" && newServerAddress != "" {
			add()
			imgui.SetKeyboardFocusHereV(-1)
		}
	}

	enableAdd := newServerName != "" && newServerAddress != ""
	if !enableAdd {
		imgui.PushItemFlag(imgui.ItemFlagsDisabled, true)
		imgui.PushStyleVarFloat(imgui.StyleVarAlpha, imgui.CurrentStyle().Alpha()*0.5)
	}
	imgui.SameLine()
	if imgui.Button("+##newServer") {
		add()
	}
	if !enableAdd {
		imgui.PopItemFlag()
		imgui.PopStyleVar()
	}

	enableDelete := selectedServer != ""
	if !enableDelete {
		imgui.PushItemFlag(imgui.ItemFlagsDisabled, true)
		imgui.PushStyleVarFloat(imgui.StyleVarAlpha, imgui.CurrentStyle().Alpha()*0.5)
	}
	imgui.SameLine()
	if imgui.Button(FontAwesomeIconTrash + "##newServer") {
		delete(globalConfig.CustomServers, selectedServer)
		selectedServer = ""
	}
	if !enableDelete {
		imgui.PopItemFlag()
		imgui.PopStyleVar()
	}

	imgui.Separator()
	positionConfig.DrawUI()
}

func (gc *GlobalConfig) LoadAliasesFile() {
	if gc.AliasesFile == "" {
		return
	}
	gc.aliases = make(map[string]string)

	f, err := os.Open(gc.AliasesFile)
	if err != nil {
		lg.Printf("%s: unable to read aliases file: %v", gc.AliasesFile, err)
		ShowErrorDialog("%s: unable to read aliases file: %v.", gc.AliasesFile, err)
	}
	defer f.Close()

	errors := ""
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if len(line) == 0 || line[0] != '.' {
			continue
		}

		def := strings.SplitAfterN(line, " ", 2)
		lg.Errorf("%s -> %d %+v", line, len(def), def)
		if len(def) != 2 {
			errors += def[0] + ": no alias definition found\n"
			continue
		}

		def[0] = strings.TrimSpace(def[0])
		if _, ok := gc.aliases[def[0]]; ok {
			errors += def[0] + ": multiple definitions in alias file\n"
			// but continue and keep the latter one...
		}

		gc.aliases[def[0]] = def[1]
	}

	if len(errors) > 0 {
		ShowErrorDialog("Errors found in alias file:\n%s", errors)
	}
}

func (gc *GlobalConfig) LoadNotesFile() {
	if gc.NotesFile == "" {
		return
	}

	notes, err := os.ReadFile(gc.NotesFile)
	if err != nil {
		lg.Printf("%s: unable to read notes file: %v", gc.NotesFile, err)
		ShowErrorDialog("%s: unable to read notes file: %v.", gc.NotesFile, err)
	} else {
		gc.notesRoot = parseNotes(string(notes))
	}
}

func configFilePath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		lg.Errorf("Unable to find user config dir: %v", err)
		dir = "."
	}

	dir = path.Join(dir, "Vice")
	err = os.MkdirAll(dir, 0o700)
	if err != nil {
		lg.Errorf("%s: unable to make directory for config file: %v", dir, err)
	}

	return path.Join(dir, "config.json")
}

func (gc *GlobalConfig) Encode(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "    ")
	return enc.Encode(gc)
}

func (c *GlobalConfig) Save() error {
	lg.Printf("Saving config to: %s", configFilePath())
	f, err := os.Create(configFilePath())
	if err != nil {
		return err
	}
	defer f.Close()

	return c.Encode(f)
}

func (gc *GlobalConfig) MakeConfigActive(name string) {
	if globalConfig.PositionConfigs == nil {
		globalConfig.PositionConfigs = make(map[string]*PositionConfig)
	}
	if len(globalConfig.PositionConfigs) == 0 {
		name = "Default"
		globalConfig.PositionConfigs["Default"] = NewPositionConfig()
	}

	oldConfig := positionConfig

	// NOTE: do not be clever and try to skip this work if
	// ActivePosition==name already; this function used e.g. when the color
	// scheme changes and we need to reset everything derived from that.
	gc.ActivePosition = name
	var ok bool
	if positionConfig, ok = gc.PositionConfigs[name]; !ok {
		lg.Errorf("%s: unknown position config!", name)
		return
	}

	cs := positionConfig.GetColorScheme()

	wmActivateNewConfig(oldConfig, positionConfig, cs)

	if cs.IsDark() {
		imgui.StyleColorsDark()
		style := imgui.CurrentStyle()
		darkGray := imgui.Vec4{.1, .1, .1, 1}
		style.SetColor(imgui.StyleColorWindowBg, darkGray)
		style.SetColor(imgui.StyleColorChildBg, darkGray)
		style.SetColor(imgui.StyleColorPopupBg, darkGray)
	} else {
		imgui.StyleColorsLight()
		style := imgui.CurrentStyle()
		lightGray := imgui.Vec4{.9, .9, .9, 1}
		style.SetColor(imgui.StyleColorWindowBg, lightGray)
		style.SetColor(imgui.StyleColorChildBg, lightGray)
		style.SetColor(imgui.StyleColorPopupBg, lightGray)
	}
	database.SetColorScheme(cs)
}

func (gc *GlobalConfig) PromptToSaveIfChanged(renderer Renderer, platform Platform) bool {
	fn := configFilePath()
	onDisk, err := os.ReadFile(fn)
	if err != nil {
		lg.Errorf("%s: unable to read config file: %v", fn, err)
		return false
	}

	var b strings.Builder
	if err = gc.Encode(&b); err != nil {
		lg.Errorf("%s: unable to encode config: %v", fn, err)
		return false
	}

	if b.String() == string(onDisk) {
		return false
	}

	uiShowModalDialog(NewModalDialogBox(&YesOrNoModalClient{
		title: "Save current configuration?",
		query: "Configuration has changed since the last time it was saved to disk.\nSave current configuration?",
		ok: func() {
			err := globalConfig.Save()
			if err != nil {
				ShowErrorDialog("Unable to save configuration file: %v", err)
			}
		}}), false)

	return true
}

func (pc *PositionConfig) NotifyAircraftSelected(ac *Aircraft) {
	pc.DisplayRoot.VisitPanes(func(pane Pane) {
		if cli, ok := pane.(*CLIPane); ok {
			if !cli.ConsumeAircraftSelection(ac) {
				pc.selectedAircraft = ac
			}
		}
	})
}

func (pc *PositionConfig) MonitoredFrequencies(frequencies []Frequency) []Frequency {
	var monitored []Frequency
	for _, f := range frequencies {
		// For now it's just the primed frequency...
		if pc.radioPrimed && f == pc.PrimaryFrequency {
			monitored = append(monitored, f)
		}
	}
	return monitored
}

func NewPositionConfig() *PositionConfig {
	c := &PositionConfig{}
	c.ActiveAirports = make(map[string]interface{})
	if database != nil && database.defaultAirport != "" {
		c.ActiveAirports[database.defaultAirport] = nil
		c.PrimaryRadarCenter = database.defaultAirport
	}
	c.RadarRange = 20

	c.DisplayRoot = &DisplayNode{Pane: NewRadarScopePane("Main Scope")}
	c.SplitLineWidth = 4
	c.ColorSchemeName = "Dark"
	return c
}

func (c *PositionConfig) IsActiveAirport(id string) bool {
	if c.ActiveAirports == nil {
		return false
	}

	_, ok := c.ActiveAirports[id]
	return ok
}

func (c *PositionConfig) GetColorScheme() *ColorScheme {
	if cs, ok := globalConfig.ColorSchemes[c.ColorSchemeName]; !ok {
		lg.Printf("%s: color scheme unknown", c.ColorSchemeName)
		cs = NewColorScheme()
		if globalConfig.ColorSchemes == nil {
			globalConfig.ColorSchemes = make(map[string]*ColorScheme)
		}
		globalConfig.ColorSchemes[c.ColorSchemeName] = cs
		return cs
	} else {
		return cs
	}
}

func (c *PositionConfig) DrawUI() {
	c.ActiveAirports = drawAirportSelector(c.ActiveAirports, "Active airports")

	imgui.InputTextV("Primary radar center", &c.PrimaryRadarCenter, imgui.InputTextFlagsCharsUppercase, nil)
	imgui.Text("Secondary radar centers")
	for i := range c.SecondaryRadarCenters {
		imgui.SameLine()
		imgui.InputTextV(fmt.Sprintf("##secondary%d", i), &c.SecondaryRadarCenters[i], imgui.InputTextFlagsCharsUppercase, nil)
	}
	imgui.InputIntV("Radar range", &c.RadarRange, 5, 25, 0 /* flags */)

	imgui.SliderInt("Split line width", &c.SplitLineWidth, 1, 10)
	if imgui.BeginCombo("Color scheme", c.ColorSchemeName) {
		names := SortedMapKeys(globalConfig.ColorSchemes)

		for _, name := range names {
			flags := imgui.SelectableFlagsNone
			if imgui.SelectableV(name, name == c.ColorSchemeName, flags, imgui.Vec2{}) &&
				name != c.ColorSchemeName {
				c.ColorSchemeName = name

				// This is slightly wasteful (e.g., resets the DrawList allocations),
				// but ensures that all of the panes get the new colors.
				globalConfig.MakeConfigActive(globalConfig.ActivePosition)
			}
		}
		imgui.EndCombo()
	}
}

func (c *PositionConfig) Duplicate() *PositionConfig {
	nc := &PositionConfig{}
	*nc = *c
	nc.DisplayRoot = c.DisplayRoot.Duplicate()
	nc.ActiveAirports = make(map[string]interface{})
	for ap := range c.ActiveAirports {
		nc.ActiveAirports[ap] = nil
	}
	// don't copy the todos or timers
	return nc
}

var (
	//go:embed resources/default-config.json
	defaultConfig string
)

func LoadOrMakeDefaultConfig() {
	fn := configFilePath()
	lg.Printf("Loading config from: %s", fn)

	config, err := os.ReadFile(fn)
	if err != nil {
		config = []byte(defaultConfig)
		if errors.Is(err, os.ErrNotExist) {
			lg.Printf("%s: config file doesn't exist", fn)
			_ = os.WriteFile(fn, config, 0o600)
		} else {
			lg.Printf("%s: unable to read config file: %v", fn, err)
			ShowErrorDialog("%s: unable to read config file: %v\nUsing default configuration.",
				fn, err)
			fn = "default.config"
		}
	}

	r := bytes.NewReader(config)
	d := json.NewDecoder(r)

	globalConfig = &GlobalConfig{}
	if err := d.Decode(globalConfig); err != nil {
		ShowErrorDialog("%s: configuration file is corrupt: %v", fn, err)
	}
	if globalConfig.CustomServers == nil {
		globalConfig.CustomServers = make(map[string]string)
	}

	globalConfig.LoadAliasesFile()
	globalConfig.LoadNotesFile()

	imgui.LoadIniSettingsFromMemory(globalConfig.ImGuiSettings)
}

func parseNotes(text string) *NotesNode {
	root := &NotesNode{}
	var hierarchy []*NotesNode
	hierarchy = append(hierarchy, root)

	for _, line := range strings.Split(text, "\n") {
		depth := 0
		for depth < len(line) && line[depth] == '*' {
			depth++
		}

		current := hierarchy[len(hierarchy)-1]
		isHeader := depth > 0
		if !isHeader {
			if len(current.text) == 0 && strings.TrimSpace(line) == "" {
				// drop leading blank lines
			} else {
				current.text = append(current.text, line)
			}
			continue
		}

		// We're done with the text for this node; drop any trailing lines
		// in the text that are purely whitespace.
		for i := len(current.text) - 1; i > 0; i-- {
			if strings.TrimSpace(current.text[i]) == "" {
				current.text = current.text[:i]
			} else {
				break
			}
		}

		for depth > len(hierarchy) {
			hierarchy = append(hierarchy, &NotesNode{})
			n := len(hierarchy)
			hierarchy[n-2].children = append(hierarchy[n-2].children, hierarchy[n-1])
		}

		newNode := &NotesNode{title: strings.TrimSpace(line[depth:])}
		if depth == len(hierarchy) {
			hierarchy = append(hierarchy, newNode)
		} else {
			hierarchy[depth] = newNode
			hierarchy = hierarchy[:depth+1]
		}
		n := len(hierarchy)
		hierarchy[n-2].children = append(hierarchy[n-2].children, newNode)
	}

	return root
}

func (pc *PositionConfig) Update(*ControlUpdates) {
	i := 0
	for i < len(pc.mit) {
		ac := pc.mit[i]
		if ac == nil {
			//lg.Printf("%s: lost a/c for mit. removing it.", pc.mit[i].Callsign())
			pc.mit = append(pc.mit[:i], pc.mit[i+1:]...)
		} else if ac.OnGround() || ac.Position().IsZero() {
			pc.mit = append(pc.mit[:i], pc.mit[i+1:]...)
		} else {
			// Only increment i if the aircraft survived
			i++
		}
	}
}
