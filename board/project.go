package board

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// NetClass is one entry from the project's net settings. Only the fields that
// bear on where copper may go are kept.
type NetClass struct {
	Name          string  `json:"name"`
	Clearance     float64 `json:"clearance"`
	TrackWidth    float64 `json:"track_width"`
	DiffPairWidth float64 `json:"diff_pair_width"`
	DiffPairGap   float64 `json:"diff_pair_gap"`
	ViaDiameter   float64 `json:"via_diameter"`
	ViaDrill      float64 `json:"via_drill"`
}

// NetClassPattern assigns nets matching a glob to a class.
type NetClassPattern struct {
	NetClass string `json:"netclass"`
	Pattern  string `json:"pattern"`
}

// Project is the parts of a .kicad_pro this tool needs: the net classes, the
// patterns assigning nets to them, and the board-wide minimums.
//
// It carries JSON tags because a host may have to keep it alongside an
// uploaded board between requests. A board analysed with its project file must
// not silently become more permissive on a later request than it was on the
// first, which is what happens if the net classes are dropped and every
// clearance falls back to the board minimum.
type Project struct {
	Path string `json:"path,omitempty"`

	Classes  []NetClass        `json:"classes,omitempty"`
	Patterns []NetClassPattern `json:"patterns,omitempty"`

	// MinClearance is the board-wide floor from Board Setup.
	MinClearance float64 `json:"min_clearance"`

	// MinTrackWidth is the board-wide minimum track width.
	MinTrackWidth float64 `json:"min_track_width"`

	// EdgeClearance is the minimum copper-to-board-edge distance.
	EdgeClearance float64 `json:"edge_clearance"`

	// UseHeightForLength mirrors the setting that decides whether via barrels
	// count toward net length.
	UseHeightForLength bool `json:"use_height_for_length"`

	// HasCustomRules is true when a .kicad_dru sits beside the project.
	//
	// Where a rule tightens clearance, this tool's own check could pass
	// something KiCad rejects, which is why the flow ends by running KiCad's
	// own DRC either way.
	HasCustomRules bool `json:"has_custom_rules"`

	// CustomRules is the text of that .kicad_dru, kept so the rules can be
	// parsed again on a later request.
	//
	// It lives on the project because the project is what already travels with
	// a board between requests, and because the parsed form cannot travel: a
	// rule like intersectsCourtyard('U3') is only meaningful against the
	// footprints of a particular board, so it has to be re-read against the
	// board it is being applied to.
	CustomRules string `json:"custom_rules,omitempty"`

	byName map[string]*NetClass
}

// Defaults returns a project with no net classes and KiCad's own board
// minimums, for a board supplied without its .kicad_pro.
//
// Nothing here is guessed upward: with no classes every clearance resolves to
// MinClearance, so the tool is stricter than a board with real classes would
// require rather than looser.
func Defaults() *Project {
	return &Project{
		MinClearance:       0.2,
		MinTrackWidth:      0.2,
		EdgeClearance:      0.5,
		UseHeightForLength: true,
	}
}

// index builds the name lookup on demand, so a Project that arrived over JSON
// rather than from LoadProject still resolves its classes.
func (p *Project) index() map[string]*NetClass {
	if p.byName == nil || len(p.byName) != len(p.Classes) {
		p.byName = make(map[string]*NetClass, len(p.Classes))
		for i := range p.Classes {
			p.byName[p.Classes[i].Name] = &p.Classes[i]
		}
	}
	return p.byName
}

type projectFile struct {
	Board struct {
		DesignSettings struct {
			Rules map[string]any `json:"rules"`
		} `json:"design_settings"`
	} `json:"board"`
	NetSettings struct {
		Classes  []NetClass        `json:"classes"`
		Patterns []NetClassPattern `json:"netclass_patterns"`
	} `json:"net_settings"`
}

// LoadProject reads the .kicad_pro beside a board. A board with no project
// file is usable: sensible KiCad defaults are assumed and reported as such.
func LoadProject(boardPath string) (*Project, error) {
	dir := filepath.Dir(boardPath)
	base := strings.TrimSuffix(filepath.Base(boardPath), filepath.Ext(boardPath))
	path := filepath.Join(dir, base+".kicad_pro")

	hasDRU := false
	if _, err := os.Stat(filepath.Join(dir, base+".kicad_dru")); err == nil {
		hasDRU = true
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			p := Defaults()
			p.Path = path
			p.HasCustomRules = hasDRU
			return p, nil
		}
		return nil, err
	}
	p, err := ParseProject(raw, path)
	if err != nil {
		return nil, err
	}
	p.HasCustomRules = hasDRU
	return p, nil
}

// ParseProject reads the bytes of a .kicad_pro. It is what a host uses when
// the project file arrives over HTTP rather than off disk.
func ParseProject(raw []byte, path string) (*Project, error) {
	p := Defaults()
	p.Path = path

	var pf projectFile
	if err := json.Unmarshal(raw, &pf); err != nil {
		return nil, err
	}
	p.Classes = pf.NetSettings.Classes
	p.Patterns = pf.NetSettings.Patterns
	r := pf.Board.DesignSettings.Rules
	if v, ok := r["min_clearance"].(float64); ok {
		p.MinClearance = v
	}
	if v, ok := r["min_track_width"].(float64); ok {
		p.MinTrackWidth = v
	}
	if v, ok := r["min_copper_edge_clearance"].(float64); ok {
		p.EdgeClearance = v
	}
	if v, ok := r["use_height_for_length_calcs"].(bool); ok {
		p.UseHeightForLength = v
	}
	return p, nil
}

// ClassOf returns the net class assigned to a net.
//
// KiCad matches nets to classes with shell-style patterns, later entries
// winning, and falls back to the class named Default. The same order is used
// here so that a net gets the clearance the designer set for it rather than a
// guess.
func (p *Project) ClassOf(net string) *NetClass {
	idx := p.index()
	var hit *NetClass
	for _, pat := range p.Patterns {
		if matchPattern(pat.Pattern, net) {
			if c, ok := idx[pat.NetClass]; ok {
				hit = c
			}
		}
	}
	if hit != nil {
		return hit
	}
	if c, ok := idx["Default"]; ok {
		return c
	}
	return nil
}

// ClearanceOf returns the clearance a net's copper requires, never below the
// board-wide minimum.
func (p *Project) ClearanceOf(net string) float64 {
	c := p.MinClearance
	if cl := p.ClassOf(net); cl != nil && cl.Clearance > c {
		c = cl.Clearance
	}
	return c
}

// ClearanceBetween returns the clearance required between two nets: the larger
// of their two requirements, which is how KiCad resolves it.
func (p *Project) ClearanceBetween(a, b string) float64 {
	return math.Max(p.ClearanceOf(a), p.ClearanceOf(b))
}

// TrackWidthOf returns the net's class track width, or zero if unset.
func (p *Project) TrackWidthOf(net string) float64 {
	cl := p.ClassOf(net)
	if cl == nil {
		return 0
	}
	if cl.TrackWidth > 0 {
		return cl.TrackWidth
	}
	return cl.DiffPairWidth
}

// ClassNames lists the class names, sorted.
func (p *Project) ClassNames() []string {
	out := make([]string, 0, len(p.Classes))
	for _, c := range p.Classes {
		out = append(out, c.Name)
	}
	sort.Strings(out)
	return out
}

// matchPattern implements the glob KiCad uses for net class patterns: * for any
// run of characters, ? for one. Anything else is literal, and the whole net
// name has to match.
func matchPattern(pat, s string) bool {
	// Iterative wildcard match, so a pattern full of stars cannot blow up.
	var star = -1
	var mark = 0
	i, j := 0, 0
	for i < len(s) {
		switch {
		case j < len(pat) && (pat[j] == '?' || pat[j] == s[i]):
			i++
			j++
		case j < len(pat) && pat[j] == '*':
			star = j
			mark = i
			j++
		case star >= 0:
			j = star + 1
			mark++
			i = mark
		default:
			return false
		}
	}
	for j < len(pat) && pat[j] == '*' {
		j++
	}
	return j == len(pat)
}
