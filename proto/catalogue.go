package proto

// What each family normally looks like.
//
// Two things live here, and both exist because the tool proposes and the user
// decides.
//
// The typical signal names are what a family's nets are usually called. They
// are used to recognise an interface, and they are shown to the user so that an
// interface named some other way can be assigned by hand: "a typical RGMII has
// TXD0 to TXD3, TX_CTL and GTX_CLK going out, and the same coming back" is a
// question somebody can answer about their own board.
//
// The typical impedances are what the interface is normally drawn to. They are
// not requirements this tool enforces -- it cannot know the fabricator's
// stackup, and the number that governs is in the controller's layout guide --
// but a board whose geometry comes out at 55 ohms where the family wants 40 is
// worth telling somebody about before they spend an afternoon matching its
// lengths.

// Family describes one kind of interface.
type Family struct {
	Kind Kind

	// Label is the family's name in full.
	Label string

	// Summary is one line on what it is and what has to be matched.
	Summary string

	// TX and RX are the signal names a board usually gives each direction, and
	// Common is what belongs to neither. Written without an instance prefix,
	// the way a data sheet writes them.
	TX, RX, Common []string

	// SingleEndedOhms and DiffOhms are the usual impedance targets. Zero means
	// the family does not have one worth stating.
	SingleEndedOhms, DiffOhms float64

	// OhmsNote qualifies the figures: which generation, or where they vary.
	OhmsNote string

	// IntraPairMM is the usual limit on the skew between the halves of a pair.
	IntraPairMM float64

	// GroupMM is the usual limit between a group's members and its clock.
	GroupMM float64

	// Differential is true when the family is mostly or entirely pairs.
	Differential bool
}

// Families are the interfaces this tool knows how to describe, in the order a
// picker should offer them.
var Families = []Family{
	{
		Kind: DDR, Label: "DDR memory",
		Summary:         "byte lanes matched to their own strobe, address and command to the clock, per leg of the fly-by chain",
		TX:              []string{"A0..A17", "BA0..BA1", "BG0..BG1", "CKE", "CS_N", "ODT", "ACT_N", "RAS_N", "CAS_N", "WE_N", "CK_T", "CK_C"},
		RX:              []string{"DQ0..DQ63", "DQS_T", "DQS_C", "DM_N", "DBI_N"},
		Common:          []string{"RESET_N", "ALERT_N", "ZQ", "TEN"},
		SingleEndedOhms: 40, DiffOhms: 80,
		OhmsNote:    "DDR4 is usually 40 ohms single-ended and 80 differential; DDR3 is 50 and 100",
		IntraPairMM: 0, GroupMM: 0.635, // no pair limit for DDR (AN5724)
	},
	{
		Kind: RGMII, Label: "Ethernet RGMII",
		Summary:         "each direction is source-synchronous: the data travels with its own clock and is matched to it",
		TX:              []string{"TXD0..TXD3", "TX_CTL", "TX_EN", "GTX_CLK", "TX_CLK"},
		RX:              []string{"RXD0..RXD3", "RX_CTL", "RX_DV", "RX_CLK"},
		Common:          []string{"MDC", "MDIO", "MDINT", "CLK125", "RESET_N"},
		SingleEndedOhms: 50,
		OhmsNote:        "RGMII is single-ended throughout",
		GroupMM:         10,
	},
	{
		Kind: MIIRMII, Label: "Ethernet RMII/MII",
		Summary:         "data matched to the reference clock both ways",
		TX:              []string{"TXD0..TXD1", "TX_EN"},
		RX:              []string{"RXD0..RXD1", "CRS_DV", "RX_ER"},
		Common:          []string{"REF_CLK", "MDC", "MDIO"},
		SingleEndedOhms: 50,
		GroupMM:         12,
	},
	{
		Kind: PCIe, Label: "PCI Express",
		Summary:     "differential pairs only: each lane recovers its own clock, so there is nothing to match lane to lane",
		TX:          []string{"TX0+/-", "TX1+/-", "PERp/PERn"},
		RX:          []string{"RX0+/-", "RX1+/-", "PETp/PETn"},
		Common:      []string{"REFCLK+/-", "PERST_N", "CLKREQ_N", "WAKE_N"},
		DiffOhms:    85,
		OhmsNote:    "85 ohms for Gen3 and later; many Gen1 and Gen2 designs use 100",
		IntraPairMM: 0.127, Differential: true,
	},
	{
		Kind: USB2, Label: "USB 2.0",
		Summary:     "one differential pair; the two halves have to match each other and nothing else",
		Common:      []string{"D+", "D-", "USB_DP", "USB_DM", "VBUS", "ID"},
		DiffOhms:    90,
		OhmsNote:    "90 ohms differential is the USB 2.0 figure",
		IntraPairMM: 0.15, Differential: true,
	},
	{
		Kind: USBSS, Label: "USB 3 SuperSpeed",
		Summary:     "one transmit and one receive pair per lane, each matched within itself",
		TX:          []string{"SSTX+/-", "TX1+/-"},
		RX:          []string{"SSRX+/-", "RX1+/-"},
		DiffOhms:    90,
		IntraPairMM: 0.127, Differential: true,
	},
	{
		Kind: MIPI, Label: "MIPI D-PHY (CSI/DSI)",
		Summary:         "data lanes matched to the clock lane, and each pair matched within itself",
		TX:              []string{"D0+/-", "D1+/-", "D2+/-", "D3+/-"},
		Common:          []string{"CK+/-", "CLK_P/CLK_N"},
		SingleEndedOhms: 50, DiffOhms: 100,
		OhmsNote:    "100 ohms differential; the single-ended figure matters in low-power mode",
		IntraPairMM: 0.1, GroupMM: 0.5, Differential: true,
	},
	{
		Kind: SDMMC, Label: "SD / eMMC",
		Summary:         "command and data matched to the clock",
		Common:          []string{"CLK", "CMD", "D0..D7", "DS", "RST_N"},
		SingleEndedOhms: 50,
		OhmsNote:        "50 ohms single-ended; HS400 eMMC is far tighter on skew than the slower modes",
		GroupMM:         2.5,
	},
	{
		Kind: DiffOnly, Label: "Differential pairs",
		Summary:     "the two halves of each pair matched to each other, whatever the signal turns out to be",
		DiffOhms:    100,
		OhmsNote:    "100 ohms is the usual default where the interface is not known",
		IntraPairMM: 0.127, Differential: true,
	},
}

// FamilyOf returns the description of a kind.
func FamilyOf(k Kind) (Family, bool) {
	for _, f := range Families {
		if f.Kind == k {
			return f, true
		}
	}
	return Family{}, false
}
