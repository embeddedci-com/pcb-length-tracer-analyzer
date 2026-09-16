package ddr

import (
	"regexp"
	"strings"
)

// The published length rules of the parts people actually build with.
//
// Every number here is copied from the vendor's own layout table, with the
// document, its version and the table it came from beside it, because that is
// what a reviewer will check the board against. Nothing is averaged, rounded
// to something tidier, or carried over from another part: where a guide does
// not state a limit, the preset leaves it unset and the tool's own default
// applies.
//
// A guide states its limits either as a length or as a delay, and this keeps
// whichever it used. Rockchip's 8-layer tables are in picoseconds on purpose --
// on a board where a signal runs partly on an outer layer and partly on an
// inner one, equal length is not equal time -- so converting them to
// millimetres here would throw away the thing the table is careful about.
//
// A limit a guide does not state is left at zero rather than filled in from
// somewhere else. Microchip removed the strobe against clock bound from the
// SAMA5D2 note in its 2018 revision and TI never gave one for the AM64x, and
// borrowing a figure from a sibling part would put a number in front of a
// reviewer that no document backs. The tool's own default applies instead, and
// the preset says so.

// Preset is one part's rules for one memory type.
type Preset struct {
	// ID is stable: it is what a board remembers having chosen.
	ID string `json:"id"`

	Name   string `json:"name"`
	Vendor string `json:"vendor"`

	// Parts are the part numbers it applies to, as a person writes them.
	Parts []string `json:"parts,omitempty"`

	// match recognises the part from a controller footprint's value, against
	// the value with everything but letters and digits removed and upper
	// cased. It is deliberately a family token rather than a full part
	// number: boards label that footprint anything from "RK3588" to
	// "MIMX8MN6CVTIZAA", and a preset claiming the wrong chip is worse than
	// one claiming none, so it matches only what is unmistakable.
	match string

	// Memory is the memory type the table is for: rules differ between LPDDR4
	// and LPDDR5 on the same part.
	Memory string `json:"memory"`

	// Source is the document, version and table, and URL a copy of it.
	Source string `json:"source"`
	URL    string `json:"url,omitempty"`

	// Note is what the table says that this tool cannot express, and anything
	// a user should know before trusting the numbers.
	Note string `json:"note,omitempty"`

	// The limits. A zero one is a limit the guide does not state, where the
	// tool's default applies; server.PresetInfo names those so the form and
	// the catalogue can say which figures are the vendor's.
	DataToStrobe   Tolerance `json:"-"`
	IntraPair      Tolerance `json:"-"`
	AddressToClock Tolerance `json:"-"`
	StrobeToClock  Tolerance `json:"-"`

	// MaxChipDeltaMM is zero where the guide says nothing about one memory
	// device against another, which is every point-to-point part here.
	MaxChipDeltaMM float64 `json:"-"`

	// ClockOffsetPercent is zero unless the guide asks for the address group
	// to lead or lag the clock.
	ClockOffsetPercent float64 `json:"-"`
}

// mils is a tolerance a guide states in mils.
func mils(n float64) Tolerance { return Tolerance{MM: n * MMPerMil} }

// ps is a tolerance a guide states as a delay.
func ps(n float64) Tolerance { return Tolerance{PS: n} }

// presets, newest parts first within a vendor: the list is what the form
// offers, in the order it offers it.
var presets = []Preset{
	{
		ID:     "st-stm32mp25-ddr4",
		Name:   "STM32MP25x, DDR4",
		Vendor: "STMicroelectronics",
		Parts:  []string{"STM32MP25"},
		match:  `STM32MP25`,
		Memory: "DDR4",
		Source: "ST DDR4 length equalization sheet for STM32MP25xxAI, with AN5724",
		URL:    "https://www.st.com/resource/en/application_note/an5724-stm32mp25xx-hardware-design-guidelines-stmicroelectronics.pdf",
		Note: "Package lengths per ball come with this part, so lengths include the wiring " +
			"inside the package. AN5724 sets no pair limit: it forbids equalizing inside a pair " +
			"and takes the mean of N and P as the pair's length, so the tool's default stands there.",
		// AN5724's own mils, converted exactly. Its millimetres are rounded,
		// and its two documents do not round alike: see DefaultRules.
		DataToStrobe:   mils(56),
		AddressToClock: mils(140),
		StrobeToClock:  mils(475),
		MaxChipDeltaMM: 1378 * MMPerMil,
	},
	{
		ID:     "stm32mp1-ddr3l",
		Name:   "STM32MP1 Series, DDR3L and LPDDR3",
		Vendor: "STMicroelectronics",
		Parts:  []string{"STM32MP151", "STM32MP153", "STM32MP157"},
		match:  `STM32MP15`,
		Memory: "DDR3/DDR3L/LPDDR2/LPDDR3",
		Source: "ST AN5122 Rev 3 (2019-02), STM32MP1 Series DDR memory routing guidelines, sections 6.1 to 6.3",
		URL:    "https://www.st.com/resource/en/application_note/an5122-stm32mp1-series-ddr-memory-routing-guidelines-stmicroelectronics.pdf",
		Note: "The note's windows are one sided: address and command must be 0 to 40 mil shorter than " +
			"the clock, and the strobe 0 to 590 mil shorter, with the clock the longest trace of all. " +
			"This tool centers both on the clock, so it checks the size of the difference but not its " +
			"direction. Like AN5724 it sets no pair limit, taking the mean of N and P instead, so the " +
			"tool's default stands there. The chip to chip figure is for the two-device fly-by case.",
		DataToStrobe:   mils(40),
		AddressToClock: mils(40),
		StrobeToClock:  mils(590),
		MaxChipDeltaMM: 1300 * MMPerMil,
	},
	{
		ID:     "rk3588-lpddr5-hdi",
		Name:   "RK3588 and RK3588S, LPDDR5 (10-layer HDI)",
		Vendor: "Rockchip",
		Parts:  []string{"RK3588", "RK3588S"},
		match:  `RK3588`,
		Memory: "LPDDR5",
		Source: "RK3588 Hardware Design Guide V1.0 (2022-01-06), table 3-7",
		URL:    "https://github.com/FanX-Tek/RK3588_hardware/blob/master/RK3588/01_Official%20Release/01_Common%20Document/RK3588%20Hardware%20Design%20Guide-V1.0.pdf",
		Note: "For the 10-layer HDI stackup, where the guide matches by length. " +
			"WCLK is held to the same limit as DQS. Rockchip strongly recommends its own DDR template.",
		DataToStrobe:   mils(25),
		IntraPair:      mils(5),
		AddressToClock: mils(40),
		StrobeToClock:  mils(200),
	},
	{
		ID:             "rk3588-lpddr4-hdi",
		Name:           "RK3588 and RK3588S, LPDDR4/LPDDR4X (10-layer HDI)",
		Vendor:         "Rockchip",
		Parts:          []string{"RK3588", "RK3588S"},
		match:          `RK3588`,
		Memory:         "LPDDR4/LPDDR4X",
		Source:         "RK3588 Hardware Design Guide V1.0 (2022-01-06), tables 3-8 and 3-9",
		URL:            "https://github.com/FanX-Tek/RK3588_hardware/blob/master/RK3588/01_Official%20Release/01_Common%20Document/RK3588%20Hardware%20Design%20Guide-V1.0.pdf",
		Note:           "For the 10-layer HDI stackup, where the guide matches by length.",
		DataToStrobe:   mils(25),
		IntraPair:      mils(5),
		AddressToClock: mils(40),
		StrobeToClock:  mils(250),
	},
	{
		ID:     "rk3588-lpddr4-8layer",
		Name:   "RK3588 and RK3588S, LPDDR4/LPDDR4X (8-layer, equal delay)",
		Vendor: "Rockchip",
		Parts:  []string{"RK3588", "RK3588S"},
		match:  `RK3588`,
		Memory: "LPDDR4/LPDDR4X",
		Source: "RK3588 Hardware Design Guide V1.0 (2022-01-06), tables 3-10 and 3-11",
		URL:    "https://github.com/FanX-Tek/RK3588_hardware/blob/master/RK3588/01_Official%20Release/01_Common%20Document/RK3588%20Hardware%20Design%20Guide-V1.0.pdf",
		Note: "For the 8-layer through-hole stackup. The guide gives these as delays, " +
			"because surface and inner layers differ in speed, so the limits here are in picoseconds.",
		DataToStrobe:   ps(16),
		IntraPair:      ps(1),
		AddressToClock: ps(16),
		StrobeToClock:  ps(40),
	},
	{
		ID:     "rk3568-lpddr4",
		Name:   "RK3566 and RK3568, LPDDR4/LPDDR4X",
		Vendor: "Rockchip",
		Parts:  []string{"RK3566", "RK3568"},
		match:  `RK356[68]`,
		Memory: "LPDDR4/LPDDR4X",
		Source: "RK3568 High Speed PCB Design Guide V1.0 (2021-04-12), tables 33 and 34",
		URL:    "https://github.com/hqnicolas/RK3568-hardware-design/blob/main/01_Common%20Document/Rockchip_RK3568_High_Speed_PCB_Design_Guide_V10_EN_2021-4-12.pdf",
		Note: "The guide says DQ and DM need not match DQS, only stay under 600 mil and be as short " +
			"as possible, so the data limit here is that bound rather than a matching requirement.",
		DataToStrobe:   mils(600),
		IntraPair:      mils(12),
		AddressToClock: mils(500),
		StrobeToClock:  mils(1200),
	},
	{
		ID:     "rk3568-ddr4",
		Name:   "RK3566 and RK3568, DDR4",
		Vendor: "Rockchip",
		Parts:  []string{"RK3566", "RK3568"},
		match:  `RK356[68]`,
		Memory: "DDR4",
		Source: "RK3568 High Speed PCB Design Guide V1.0 (2021-04-12), tables 27 to 29",
		URL:    "https://github.com/hqnicolas/RK3568-hardware-design/blob/main/01_Common%20Document/Rockchip_RK3568_High_Speed_PCB_Design_Guide_V10_EN_2021-4-12.pdf",
		Note: "Address and command are held to the guide's 600 mil for the controller-to-memory run. " +
			"The guide holds CSn, CKE and ODT to 30 mil instead, which this tool does not separate, " +
			"and the branches after a memory device to 20 mil of each other.",
		DataToStrobe:   mils(600),
		IntraPair:      mils(12),
		AddressToClock: mils(600),
		StrobeToClock:  mils(1500),
	},
	{
		ID:     "rk3399-ddr3",
		Name:   "RK3399, DDR3/DDR3L",
		Vendor: "Rockchip",
		Parts:  []string{"RK3399"},
		match:  `RK3399`,
		Memory: "DDR3/DDR3L",
		Source: "RK3399 Design Guide V1.0 (2017-04-20), tables 4-5 to 4-8",
		URL:    "https://github.com/cnchens/RK3399_Hardware_Design_Reference/blob/master/RK3399_Design_Guide_V1.0_20170420.pdf",
		Note: "The guide gives these as delays. Its 5 ps applies inside a byte lane; " +
			"between byte lanes the clock-to-strobe limit of 150 ps governs.",
		DataToStrobe:   ps(5),
		IntraPair:      ps(1),
		AddressToClock: ps(10),
		StrobeToClock:  ps(150),
	},
	{
		ID:             "rk3399-lpddr3",
		Name:           "RK3399, LPDDR3",
		Vendor:         "Rockchip",
		Parts:          []string{"RK3399"},
		match:          `RK3399`,
		Memory:         "LPDDR3",
		Source:         "RK3399 Design Guide V1.0 (2017-04-20), tables 4-1 to 4-4",
		URL:            "https://github.com/cnchens/RK3399_Hardware_Design_Reference/blob/master/RK3399_Design_Guide_V1.0_20170420.pdf",
		Note:           "The guide gives these as delays. Command and control are both held to 5 ps against the clock.",
		DataToStrobe:   ps(5),
		IntraPair:      ps(1),
		AddressToClock: ps(5),
		StrobeToClock:  ps(150),
	},
	{
		ID:     "imx93-imx8mp-imx8mn-lpddr4",
		Name:   "i.MX 93, i.MX 8M Plus and i.MX 8M Nano, LPDDR4/LPDDR4X",
		Vendor: "NXP",
		Parts:  []string{"i.MX 93", "i.MX 8M Plus", "i.MX 8M Nano"},
		match:  `IMX8MP|IMX8MN|IMX93`,
		Memory: "LPDDR4/LPDDR4X",
		Source: "NXP i.MX 93 Hardware Design Guide Rev. 1 (2023-04-24), table 16, " +
			"i.MX 8M Plus Hardware Developer's Guide Rev. 1 (2024-03-26), table 18, " +
			"and i.MX 8M Nano Hardware Developer's Guide Rev. 1 (2020-11), table 22",
		URL: "https://community.nxp.com/pwmxy87654/attachments/pwmxy87654/imx-processors/225641/1/IMX93HDG.pdf",
		Note: "All three guides hold these limits, at 3733, 4000 and 3200 MT/s. " +
			"They also ask subgroups of CA to match within 2 ps of each other, and the i.MX 93 holds " +
			"CS0 and CS1 to 1 ps of the clock, neither of which this tool separates from address and command. " +
			"The i.MX 93 strobe window is not centered (125 ps short, 75 ps long) and the tighter side is used.",
		DataToStrobe:   ps(50),
		IntraPair:      ps(1),
		AddressToClock: ps(50),
		StrobeToClock:  ps(75),
	},
	{
		ID:     "imx8m-lpddr4",
		Name:   "i.MX 8M Mini and i.MX 8M Quad, LPDDR4",
		Vendor: "NXP",
		Parts:  []string{"i.MX 8M Mini", "i.MX 8M Quad", "i.MX 8M QuadLite", "i.MX 8M Dual"},
		match:  `IMX8MM|IMX8MQ|IMX8MD`,
		Memory: "LPDDR4",
		Source: "NXP i.MX 8M Mini Hardware Developer's Guide Rev. 1 (2019-08), table 21, " +
			"and i.MX 8MDQLQ Hardware Developer's Guide Rev. 2 (2019-06), table 16",
		URL: "https://community.nxp.com/pwmxy87654/attachments/pwmxy87654/imx-processors/218788/1/IMX8MMHDG-NEW.pdf",
		Note: "Both guides hold these limits, at 3000 MT/s on the Mini and 3200 on the Quad. " +
			"They also cap the clock's own flight time, which this tool does not check.",
		DataToStrobe:   ps(10),
		IntraPair:      ps(1),
		AddressToClock: ps(25),
		StrobeToClock:  ps(85),
	},
	{
		ID:     "imx8m-ddr3l",
		Name:   "i.MX 8M Mini and i.MX 8M Quad, DDR3L-1600",
		Vendor: "NXP",
		Parts:  []string{"i.MX 8M Mini", "i.MX 8M Quad", "i.MX 8M QuadLite", "i.MX 8M Dual"},
		match:  `IMX8MM|IMX8MQ|IMX8MD`,
		Memory: "DDR3L",
		Source: "NXP i.MX 8M Mini Hardware Developer's Guide Rev. 1 (2019-08), table 28, " +
			"and i.MX 8MDQLQ Hardware Developer's Guide Rev. 2 (2019-06), table 21",
		URL: "https://community.nxp.com/pwmxy87654/attachments/pwmxy87654/imx-processors/218788/1/IMX8MMHDG-NEW.pdf",
		Note: "The guides bound the strobe against the clock at one clock period rather than a window, " +
			"which is 1250 ps at 1600 MT/s, and a strobe shorter than the clock is allowed.",
		DataToStrobe:   ps(10),
		IntraPair:      ps(1),
		AddressToClock: ps(25),
		StrobeToClock:  ps(1250),
	},
	{
		ID:     "imx8mq-ddr4",
		Name:   "i.MX 8M Quad, DDR4-2400",
		Vendor: "NXP",
		Parts:  []string{"i.MX 8M Quad", "i.MX 8M QuadLite", "i.MX 8M Dual"},
		match:  `IMX8MQ|IMX8MD`,
		Memory: "DDR4",
		Source: "NXP i.MX 8MDQLQ Hardware Developer's Guide Rev. 2 (2019-06), table 21",
		URL:    "https://www.mouser.com/pdfdocs/NXP_MCIMX8M-EVK_HD.pdf",
		Note: "The strobe is bounded against the clock at one clock period, 833 ps at 2400 MT/s. " +
			"Not for the i.MX 8M Mini with DDR4: its own guide asks address and command to run 75 to 125 ps " +
			"shorter than the clock, an offset this tool cannot center.",
		DataToStrobe:   ps(10),
		IntraPair:      ps(1),
		AddressToClock: ps(25),
		StrobeToClock:  ps(833),
	},
	{
		ID:     "imx8mn-ddr4",
		Name:   "i.MX 8M Nano, DDR4-2400",
		Vendor: "NXP",
		Parts:  []string{"i.MX 8M Nano"},
		match:  `IMX8MN`,
		Memory: "DDR4",
		Source: "NXP i.MX 8M Nano Hardware Developer's Guide Rev. 1 (2020-11), table 25",
		URL:    "https://www.readkong.com/page/i-mx-8m-nano-hardware-developer-s-guide-nxp-7536664",
		Note: "The Nano holds address and command to 10 ps of the clock, tighter than the 8M Quad's 25 ps. " +
			"The strobe is bounded against the clock at one clock period, 833 ps at 2400 MT/s.",
		DataToStrobe:   ps(10),
		IntraPair:      ps(1),
		AddressToClock: ps(10),
		StrobeToClock:  ps(833),
	},
	{
		ID:     "am62x-lpddr4",
		Name:   "AM62x and AM62Lx, LPDDR4",
		Vendor: "Texas Instruments",
		Parts:  []string{"AM62x", "AM62Lx", "AM625", "AM623"},
		match:  `AM62`,
		Memory: "LPDDR4",
		Source: "TI AM62x, AM62Lx DDR Board Design and Layout Guidelines SPRAD06C (2025-03), tables 3-6 and 3-7",
		URL:    "https://www.ti.com/lit/pdf/sprad06",
		Note: "For LPDDR4-1600, the one rate the guide covers. Per-bit deskew in the PHY is why these are loose. " +
			"TI matches within a byte lane only, and asks the clock to be the longer of the two, by up to three " +
			"clock periods. The pair limit is the clock pair's 0.75 ps, tighter than the strobe pair's 1.5 ps. " +
			"Data may run 150 ps longer than its strobe but only 49 ps shorter, and the tighter side is used.",
		DataToStrobe:   ps(49),
		IntraPair:      ps(0.75),
		AddressToClock: ps(312.5),
		StrobeToClock:  ps(3750),
	},
	{
		ID:     "am64x-lpddr4",
		Name:   "AM64x and AM243x, LPDDR4",
		Vendor: "Texas Instruments",
		Parts:  []string{"AM64x", "AM243x", "AM6442", "AM2434"},
		match:  `AM64|AM243`,
		Memory: "LPDDR4",
		Source: "TI AM64x\\AM243x DDR Board Design and Layout Guidelines SPRACU1A (2021-06), tables 3-6 and 3-7",
		URL:    "https://www.ti.com/lit/an/spracu1a/spracu1a.pdf",
		Note: "For LPDDR4-1600, the one rate the guide covers. Far tighter than the AM62x, whose PHY deskews " +
			"per bit. The pair limit is 0.4 ps for both the clock and the strobe. " +
			"The guide sets no strobe against clock limit at all, matching only within a byte lane, " +
			"so the tool's default stands there.",
		DataToStrobe:   ps(2),
		IntraPair:      ps(0.4),
		AddressToClock: ps(3),
	},
	{
		ID:     "sama5d3-ddr2",
		Name:   "SAMA5D3, DDR2/LPDDR2",
		Vendor: "Microchip",
		Parts:  []string{"SAMA5D3", "ATSAMA5D3"},
		match:  `SAMA5D3`,
		Memory: "DDR2/LPDDR2",
		Source: "Microchip SAMA5D3 Layout Recommendations, Atmel-11284B (2016-04-11), section 3.3",
		URL:    "https://ww1.microchip.com/downloads/en/AppNotes/Atmel-11284-32-bit-Cortex-A5-Microcontroller-SAMA5D3-Layout-Recommendations_Application-Note.pdf",
		Note: "The note gives these as lengths in mils, as prose rather than a table. " +
			"The 20 mil pair figure is for both the strobe and the clock.",
		DataToStrobe:   mils(100),
		IntraPair:      mils(20),
		AddressToClock: mils(200),
		StrobeToClock:  mils(400),
	},
	{
		ID:     "sama5d2-ddr3l",
		Name:   "SAMA5D2, DDR3L/LPDDR3",
		Vendor: "Microchip",
		Parts:  []string{"SAMA5D2", "ATSAMA5D2"},
		match:  `SAMA5D2`,
		Memory: "DDR3L/LPDDR3",
		Source: "Microchip SAMA5D2 Layout Recommendations, AN2814 Rev. A (2018-10), section 2.3",
		URL:    "https://ww1.microchip.com/downloads/en/Appnotes/SAMA5D2-Layout-Recommendations-Application%20Note-DS00002814A.pdf",
		Note: "Revision A deleted the strobe against clock bound the earlier revision carried, " +
			"so the tool's default stands there. It also loosened data to strobe from 50 mil to 100 mil.",
		DataToStrobe:   mils(100),
		IntraPair:      mils(20),
		AddressToClock: mils(200),
	},
}

// Presets are the published rule sets the tool ships.
func Presets() []Preset {
	out := make([]Preset, len(presets))
	copy(out, presets)
	return out
}

// PresetByID finds one, or reports that there is no such preset.
func PresetByID(id string) (Preset, bool) {
	for _, p := range presets {
		if p.ID == id {
			return p, true
		}
	}
	return Preset{}, false
}

// PresetsForPart returns the presets whose chip a controller footprint's value
// names, newest first. Empty means the part was not recognised, which is the
// usual answer for a board that labels the footprint with an orderable code
// this does not know: the caller should say so rather than guess.
func PresetsForPart(value string) []Preset {
	v := normalizePart(value)
	if v == "" {
		return nil
	}
	var out []Preset
	for _, p := range presets {
		if p.match == "" {
			continue
		}
		if regexp.MustCompile(p.match).MatchString(v) {
			out = append(out, p)
		}
	}
	return out
}

// normalizePart strips a part number down to what is comparable: "i.MX 8M
// Plus", "i.MX8M-Plus" and "IMX8MPLUS" are the same chip written three ways.
func normalizePart(value string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(value) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}
