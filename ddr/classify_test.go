package ddr

import (
	"testing"

	"github.com/embeddedci-com/pcb-autorouter/board"
)

func loadBoard(t *testing.T) *board.Board {
	t.Helper()
	b, err := board.Load("../demo-pcb/ai-vision.kicad_pcb")
	if err != nil {
		t.Skipf("demo board missing: %v", err)
	}
	return b
}

func classify(t *testing.T) *Interface {
	t.Helper()
	i, err := Classify(loadBoard(t), Options{NetPrefix: "/ddr4/"})
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	return i
}

func TestClassifyDemoBoardTopology(t *testing.T) {
	i := classify(t)
	if i.Controller != "U3" {
		t.Errorf("controller = %s, want U3", i.Controller)
	}
	if len(i.Devices) != 2 || i.Devices[0] != "U4" || i.Devices[1] != "U5" {
		t.Errorf("devices = %v, want [U4 U5]", i.Devices)
	}
	if i.Width != 32 {
		t.Errorf("width = %d, want 32", i.Width)
	}
	if i.Lanes != 4 {
		t.Errorf("lanes = %d, want 4", i.Lanes)
	}
	if len(i.Unclassified) != 0 {
		t.Errorf("unclassified nets: %v", i.Unclassified)
	}
	if len(i.Notes) != 0 {
		t.Errorf("notes: %v", i.Notes)
	}
}

func TestClassifyRoles(t *testing.T) {
	i := classify(t)
	want := map[string]Role{
		"/ddr4/DDR_DQ0":    RoleData,
		"/ddr4/DDR_DQ31":   RoleData,
		"/ddr4/DDR_DQS0_P": RoleStrobe,
		"/ddr4/DDR_DQS3_N": RoleStrobe,
		"/ddr4/DDR_DQM0":   RoleDataMask,
		"/ddr4/DDR_CLK_P":  RoleClock,
		"/ddr4/DDR_CLK_N":  RoleClock,
		"/ddr4/DDR_A0":     RoleAddress,
		"/ddr4/DDR_A13":    RoleAddress,
		"/ddr4/DDR_BA0":    RoleAddress,
		"/ddr4/DDR_BG0":    RoleAddress,
		"/ddr4/DDR_RASN":   RoleCommand,
		"/ddr4/DDR_CASN":   RoleCommand,
		"/ddr4/DDR_WEN":    RoleCommand,
		"/ddr4/DDR_ACTN":   RoleCommand,
		"/ddr4/DDR_CSN":    RoleCommand,
		"/ddr4/DDR_CKE":    RoleCommand,
		"/ddr4/DDR_ODT":    RoleCommand,
		"/ddr4/DDR_RESETN": RoleControl,
	}
	for net, role := range want {
		s := i.Signal(net)
		if s == nil {
			t.Errorf("%s not classified", net)
			continue
		}
		if s.Role != role {
			t.Errorf("%s role = %s, want %s", net, s.Role, role)
		}
	}
	// Counts, so a rule that steals nets from another role is caught.
	counts := map[Role]int{}
	for _, s := range i.Signals {
		counts[s.Role]++
	}
	for role, n := range map[Role]int{
		RoleData: 32, RoleStrobe: 8, RoleDataMask: 4, RoleClock: 2,
		RoleAddress: 17, RoleCommand: 7, RoleControl: 1,
	} {
		if counts[role] != n {
			t.Errorf("%s count = %d, want %d", role, counts[role], n)
		}
	}
}

// TestActiveLowNamesAreNotMistakenForDifferentialHalves is the trap this
// classifier has to avoid: CASN, ACTN, WEN and RESETN all end in N, and a
// polarity rule that looks only at the last letter turns them into the
// complement halves of pairs that do not exist -- and then tries to match them
// against nothing.
func TestActiveLowNamesAreNotMistakenForDifferentialHalves(t *testing.T) {
	i := classify(t)
	for _, net := range []string{
		"/ddr4/DDR_CASN", "/ddr4/DDR_RASN", "/ddr4/DDR_WEN",
		"/ddr4/DDR_ACTN", "/ddr4/DDR_CSN", "/ddr4/DDR_RESETN",
	} {
		s := i.Signal(net)
		if s == nil {
			t.Fatalf("%s not classified", net)
		}
		if s.Polarity != Single {
			t.Errorf("%s polarity = %v, want Single", net, s.Polarity)
		}
		if s.Pair != "" {
			t.Errorf("%s paired with %s", net, s.Pair)
		}
	}
	// And a data bit ending in a digit must not lose its number to a polarity
	// rule either.
	if s := i.Signal("/ddr4/DDR_DQ0"); s == nil || s.Index != 0 || s.Polarity != Single {
		t.Errorf("DQ0 = %+v", s)
	}
}

func TestDifferentialPairsArePaired(t *testing.T) {
	i := classify(t)
	pairs := map[string]string{
		"/ddr4/DDR_DQS0_P": "/ddr4/DDR_DQS0_N",
		"/ddr4/DDR_DQS1_P": "/ddr4/DDR_DQS1_N",
		"/ddr4/DDR_DQS2_P": "/ddr4/DDR_DQS2_N",
		"/ddr4/DDR_DQS3_P": "/ddr4/DDR_DQS3_N",
		"/ddr4/DDR_CLK_P":  "/ddr4/DDR_CLK_N",
	}
	for p, n := range pairs {
		sp, sn := i.Signal(p), i.Signal(n)
		if sp == nil || sn == nil {
			t.Fatalf("%s / %s not classified", p, n)
		}
		if sp.Pair != n || sn.Pair != p {
			t.Errorf("%s paired with %q, %s paired with %q", p, sp.Pair, n, sn.Pair)
		}
		if sp.Polarity != Positive || sn.Polarity != Negative {
			t.Errorf("polarity: %s=%v %s=%v", p, sp.Polarity, n, sn.Polarity)
		}
	}
}

// TestByteLanesFollowTheDevices is the part that a naming rule alone gets
// wrong. On this x32 interface built from two x16 devices, DQS2 and DQM2 serve
// the lane carrying DQ16..DQ23 and land on U5; nothing in the name "DQS2" says
// so, and the classifier has to read it off the topology.
func TestByteLanesFollowTheDevices(t *testing.T) {
	i := classify(t)
	for lane, nets := range map[int][]string{
		0: {"/ddr4/DDR_DQ0", "/ddr4/DDR_DQ7", "/ddr4/DDR_DQS0_P", "/ddr4/DDR_DQS0_N", "/ddr4/DDR_DQM0"},
		1: {"/ddr4/DDR_DQ8", "/ddr4/DDR_DQ15", "/ddr4/DDR_DQS1_P", "/ddr4/DDR_DQS1_N", "/ddr4/DDR_DQM1"},
		2: {"/ddr4/DDR_DQ16", "/ddr4/DDR_DQ23", "/ddr4/DDR_DQS2_P", "/ddr4/DDR_DQS2_N", "/ddr4/DDR_DQM2"},
		3: {"/ddr4/DDR_DQ24", "/ddr4/DDR_DQ31", "/ddr4/DDR_DQS3_P", "/ddr4/DDR_DQS3_N", "/ddr4/DDR_DQM3"},
	} {
		for _, net := range nets {
			if got := i.LaneOf(net); got != lane {
				t.Errorf("%s is in lane %d, want %d (%s)", net, got, lane, i.Signal(net).Why)
			}
		}
	}
	// Lanes 0 and 1 live on U4, lanes 2 and 3 on U5.
	for net, dev := range map[string]string{
		"/ddr4/DDR_DQS0_P": "U4", "/ddr4/DDR_DQS1_P": "U4",
		"/ddr4/DDR_DQS2_P": "U5", "/ddr4/DDR_DQS3_P": "U5",
	} {
		s := i.Signal(net)
		found := false
		for _, r := range s.Devices {
			if r == dev {
				found = true
			}
		}
		if !found {
			t.Errorf("%s reaches %v, expected it to include %s", net, s.Devices, dev)
		}
	}
	// Address and command lines belong to no byte lane.
	for _, net := range []string{"/ddr4/DDR_A0", "/ddr4/DDR_CLK_P", "/ddr4/DDR_CSN"} {
		if got := i.LaneOf(net); got != -1 {
			t.Errorf("%s assigned to lane %d, want none", net, got)
		}
	}
}

func TestNetsWithRoleIsSortedByIndex(t *testing.T) {
	i := classify(t)
	data := i.NetsWithRole(RoleData)
	if len(data) != 32 {
		t.Fatalf("%d data nets", len(data))
	}
	if data[0] != "/ddr4/DDR_DQ0" || data[31] != "/ddr4/DDR_DQ31" {
		t.Errorf("data order wrong: first %s last %s", data[0], data[31])
	}
	// DQ9 must come after DQ8, not after DQ31, which is what sorting by name
	// would do.
	if data[9] != "/ddr4/DDR_DQ9" {
		t.Errorf("data[9] = %s, want DQ9", data[9])
	}
}

// TestSignalNameParsing exercises the name rules directly, including the
// vendor prefixes and JEDEC spellings that do not appear on the demo board.
func TestSignalNameParsing(t *testing.T) {
	cases := []struct {
		net  string
		role Role
		idx  int
		pol  Polarity
	}{
		{"/ddr4/DDR_DQ18", RoleData, 18, Single},
		{"DDR4_DQ7", RoleData, 7, Single},
		{"LPDDR4_DQ0", RoleData, 0, Single},
		{"MEM_DQ15", RoleData, 15, Single},
		{"/sheet/DDR_DQS2_P", RoleStrobe, 2, Positive},
		{"DDR_DQS2_N", RoleStrobe, 2, Negative},
		{"DDR_CK_T", RoleClock, -1, Positive},
		{"DDR_CK_C", RoleClock, -1, Negative},
		{"DDR_CK0_T", RoleClock, 0, Positive},
		{"DDR_CLK_P", RoleClock, -1, Positive},
		{"DDR_DM0", RoleDataMask, 0, Single},
		{"DDR_DQM3", RoleDataMask, 3, Single},
		{"DDR_A10", RoleAddress, 10, Single},
		{"DDR_BA1", RoleAddress, 1, Single},
		{"DDR_BG0", RoleAddress, 0, Single},
		{"DDR_ODT", RoleCommand, -1, Single},
		{"DDR_CKE", RoleCommand, -1, Single},
		{"DDR_RESETN", RoleControl, -1, Single},
		// Active-low spellings must stay single-ended.
		{"DDR_CASN", RoleCommand, -1, Single},
		{"DDR_WEN", RoleCommand, -1, Single},
		{"DDR_ACTN", RoleCommand, -1, Single},
		// Not DDR at all.
		{"/expansion/USB_DATA_P", RoleUnknown, -1, Single},
		{"GND", RoleUnknown, -1, Single},
		{"VDD_DDR", RoleUnknown, -1, Single},
	}
	for _, c := range cases {
		role, _, idx, pol := classifyName(c.net)
		if role != c.role || idx != c.idx || pol != c.pol {
			t.Errorf("%s: got (%s, %d, %v), want (%s, %d, %v)", c.net, role, idx, pol, c.role, c.idx, c.pol)
		}
	}
}

func TestClassifyNoDDRNets(t *testing.T) {
	b := loadBoard(t)
	if _, err := Classify(b, Options{NetPrefix: "/nothing/"}); err == nil {
		t.Error("expected an error when no DDR nets match")
	}
}

func TestClassifyLaneOverride(t *testing.T) {
	b := loadBoard(t)
	i, err := Classify(b, Options{
		NetPrefix: "/ddr4/",
		LaneOfNet: map[string]int{"/ddr4/DDR_DQ0": 3},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := i.LaneOf("/ddr4/DDR_DQ0"); got != 3 {
		t.Errorf("override ignored: lane = %d", got)
	}
}

func TestClassifyExplicitController(t *testing.T) {
	b := loadBoard(t)
	i, err := Classify(b, Options{NetPrefix: "/ddr4/", Controller: "U4"})
	if err != nil {
		t.Fatal(err)
	}
	if i.Controller != "U4" {
		t.Errorf("controller = %s, want the explicit U4", i.Controller)
	}
}
