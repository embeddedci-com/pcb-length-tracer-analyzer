# AI Vision Board — STM32MP257 + Hailo-8L (M.2) — Design Document

## Purpose

A Linux-capable, portfolio-grade AI-vision PCB that separates two concerns that were previously bundled into a single risky board:

1. **Host SoC** — owns DDR routing, Linux bring-up, camera ISP/MIPI-CSI pipeline. Chosen for cost, documentation quality, and *confirmed* PCIe + camera availability — not for NPU performance.
2. **AI acceleration** — offloaded entirely to a socketed Hailo-8L M.2 module. Decouples the expensive, hard-to-replace part from the PCB that's still being debugged; a bad DDR spin doesn't take the Hailo module down with it.

This board sits in the same capstone track as the Rockchip AI Vision Board and the isolated FPGA+DDR3L board, but takes a different risk posture: rather than isolating DDR practice onto a separate board before combining skills, it isolates the *expensive* component (Hailo module) from the *first-attempt* component (DDR layout), so a failed spin costs a cheap SoC reorder, not a $70+ accelerator.

**Revision note**: originally scoped around the NXP i.MX8M Nano. That chip was found, via direct datasheet check, to have **no PCIe controller at all** — a research error caught mid-project (see "Chip history" below). STM32MP257 replaces it as the primary chip, with confirmed PCIe and MIPI-CSI camera support.

## Why STM32MP257

| Requirement | STM32MP257 | Notes |
|---|---|---|
| External DDR | DDR3L (16/32-bit) / DDR4 (16/32-bit) / LPDDR4 (16/32-bit) | Confirmed via ST datasheet. **DDR4 selected** (reversed from an earlier LPDDR4 choice — see Memory section for the full rationale: validated-parts-list and configuration-risk concerns outweighed LPDDR4's routing/sourcing advantages) |
| PCIe | **Yes, confirmed — Gen2, 1 lane** | ST's own product page and datasheet title explicitly list PCIe/USB3.0; a separate ST feature-summary document confirms the exact spec: "PCIe Gen2, 1 lane." The official STM32MP257F-EV1 eval board has a physical Mini PCIe connector |
| Camera input | **Yes, confirmed** | MIPI CSI-2, up to 2 data lanes, 2.5 Gbit/s per lane, with onboard ISP; EV1 board has a dual-lane MIPI CSI-2 camera connector |
| NPU / AI acceleration | ST markets "Edge AI accelerators" on-chip | Not required given the Hailo-8L offload plan, but present as a fallback/secondary path if useful later |
| Debug ecosystem | ST official | ST-LINK-V3EC on-board debugger, STM32CubeMP2 software, mainlined OpenSTLinux — same debug family as prior STM32 MCU experience |

### Chip history: why i.MX8M Nano was dropped

The board was originally scoped around the i.MX8M Nano. A direct read of NXP's actual datasheet (not a family-level assumption) showed **no PCIe controller anywhere in its connectivity feature list, block diagram, or full modules list** — a real research error, since i.MX8M Mini and i.MX8M Plus (same family) do have PCIe, and that was wrongly assumed to carry over to Nano. Corrected once actually checked. i.MX8M Mini was then ruled out too (has PCIe, but no MIPI-CSI camera input — DSI/display only). Within the i.MX8M family, only i.MX8M Plus has both, at ~4x Nano's chip cost and with a redundant onboard NPU once Hailo is added.

### Chip selection (from LCSC BOM search)

| Part | LCSC # | Package | Price (qty 1) | Notes |
|---|---|---|---|---|
| **STM32MP257FAL3** | C41755867 | VFBGA-361 (10x10mm) | **$26.72** | Smaller package; supports 16-bit DDR4 point-to-point (see package note below and Memory section for the DDR4 switch) |
| STM32MP257DAK3 | C39930266 | VFBGA-424 (14x14mm) | $30.56 | Larger package, more physical balls |

**Package letter note — resolved.** Confirmed via ST's official Table 3 ("STM32MP25xC/F differences per packages"): the letter maps directly to package: **AJ = TFBGA361** (16×16mm, 0.8mm pitch — a *different*, larger package that happens to share the same 361 ball count), **AL = VFBGA361** (10×10mm, 0.5mm pitch — our FAL3), **AK = VFBGA424**, **AI = TFBGA436**. TFBGA361 and VFBGA361 are easy to confuse but are genuinely different physical footprints — confirmed FAL3 is the VFBGA361, matching LCSC's listing.

**Package capability, also resolved via the same table**: 16-bit LPDDR4 (1200MHz, up to 2GB, single rank) is supported identically across **all four packages**, VFBGA361 included — no dash, no restriction. Only **32-bit** LPDDR4/DDR4/DDR3L is gated to the two larger packages (VFBGA424, TFBGA436). Since this board uses 16-bit, FAL3 fully supports it — confirmed directly from ST's datasheet, not inferred. AN5724 (routing app note) even has a section written specifically for this package ("Layer allocation for 6-layer boards with VFBGA361") and a routing rule noting VFBGA361 as the *exception* that does *not* need bottom-layer A/C routing — evidence of an established, supported configuration, not a limitation.

Other candidates evaluated and set aside:
- **TI AM62A7/AM62A3** — genuinely excellent purpose-built vision SoC (onboard NPU, ISP, published EVK design files down to PCB stackup), confirmed on LCSC. Set aside primarily because general-purpose PCIe availability for an M.2 Hailo slot (vs. only a WiFi-dedicated M.2 Key-E on TI's own eval board) was not confirmed before this doc update — worth revisiting if STM32MP257 hits an unexpected blocker.
- **NXP i.MX93** — camera and onboard NPU (Ethos-U65) confirmed; PCIe presence could not be found anywhere in NXP's own documentation (unlike i.MX95/i.MX943, which have active PCIe kernel support). Treated as likely absent, following the same caution that i.MX8M Nano should have received from the start.
- **RK3568** — cheaper hobbyist ecosystem, real M.2 PCIe precedent (Radxa ROCK 3A), but weaker vendor documentation/debug tooling than ST.
- **Raspberry Pi CM5 + official AI Kit** — lowest Hailo-integration risk, but CM5's LPDDR4X is soldered onto the module — a custom carrier board does zero DDR routing, defeating this board's core learning goal.

## Official reference documentation

- **AN5723 — Guidelines for DDR configuration on STM32MP2 MPUs** — the actual DDR tool/wizard reference: system parameters, swizzle mechanism explained, worked LPDDR4 example (Section 8.2), and **Table 7 (AC pin mapping by device versus protocol)** — the source for the complete CA/CKE/CS/CLK ball mapping in the Memory section above. https://www.st.com/resource/en/application_note/an5723-guidelines-for-ddr-configuration-on-stm32mp2-mpus-stmicroelectronics.pdf
- **AN5724 — Guidelines for DDR memory routing on STM32MP2 MPUs** — dedicated ST application note covering DDR3L, DDR4, *and* LPDDR4 topology, schematics, and layout rules in one document. Confirmed specifics: 32-bit LPDDR4 uses point-to-point (single chip); 32-bit DDR3L/DDR4 uses two 16-bit chips in fly-by topology; includes a section specifically for 6-layer VFBGA361 layouts. https://www.st.com/resource/en/application_note/an5724-guidelines-for-ddr-memory-routing-on-stm32mp2-mpus-stmicroelectronics.pdf
- **AN6198 — STPMIC25 NVM configuration** — full register-level default NVM content for variants A/B/D; the source confirming DDR rails are unconfigured by default in all variants, and that D specifically enables SD-Card UHS-I boot support. https://www.st.com/resource/en/application_note/an6198-stpmic25-nvm-configuration-stmicroelectronics.pdf
- **AN5727 — How to use STPMIC25 for a wall-adapter-powered application on STM32MP25x lines MPUs** — covers the full VIN/startup state machine for variant A; not yet checked for the specific BUCK6/LDO3/REFDDR programming sequence needed for our DDR4 rails (VDDQ, VTT, VREF, plus VPP).
- **STM32MP257 full datasheet** (DS14284) — memory interface tables, PCIe/camera peripheral list, electrical characteristics, and **Table 3 (differences per package)** confirming 16-bit LPDDR4 support across all four package options. https://www.st.com/resource/en/datasheet/stm32mp257f.pdf
- **STM32MP257 documentation hub** — full set of ST PDFs/app notes for this chip. https://www.st.com/en/microcontrollers-microprocessors/stm32mp257/documentation.html
- **"Validated DRAM on STM32 MPU portfolio" wiki** — ST-maintained list of confirmed-compatible DRAM part numbers (Micron/Nanya/Winbond/ISSI only — no Samsung; see Memory section for how this was handled). Also an active ST community forum thread flagging single-rank vs. dual-rank compatibility as a real gotcha.
- **STM32MP257F-EV1 evaluation board** — CAD Resources (schematics, layout, BOM, Gerbers) available under ST's Open Platform License Agreement, explicitly permitting reuse. Uses STM32MP257F**AI3** (TFBGA436, different package from ours) with 32-bit DDR4 (two Micron MT40A1G16TB-062E chips) — a useful topology/PMIC reference, not a direct LPDDR4 ballout match. Confirmed real parts from its BOM: PMIC STPMIC25APQR, PCIe reference clock generator (Microchip DSC557-0344FI0-T, crystal-less, 100MHz), camera FFC connector (Molex 524372271, 22-pin 0.5mm).

## AI acceleration: Hailo-8L over M.2

- **Module**: Hailo-8L, M.2 **B+M key**, 2242 form factor.
- **Interface**: PCIe. Hailo-8L is specifically sized for single-lane operation, which matches **exactly** — STM32MP257's PCIe controller is confirmed as **Gen2, 1 lane** (from ST's own feature-summary documentation), not an approximation. No lane mismatch, and no headroom for a wider-lane module later — this SoC caps at x1 regardless of PCB design, so a future Hailo-10H (if it needs more than x1) would be limited by the host, not the routing.
- **Performance**: 13 TOPS, ~8 TOPS/W — appropriate for single/dual-camera object detection or classification workloads.
- **Driver status — key open risk, broken into four real sub-tasks**:
  1. **PCIe root-complex enablement** — STM32MP257's PCIe controller must be explicitly configured for host mode (clocks, reset lines, reference clock to the M.2 slot). Not guaranteed by default configs; tied to this board's specific layout.
  2. **Kernel module** — `hailort-drivers` (Hailo's PCIe driver, GPLv2, github.com/hailo-ai/hailort-drivers) is a generic Linux PCI driver, not vendor-specific — builds against plain kernel headers. Real work here is packaging it against ST's OpenSTLinux build, not writing driver code.
  3. **Firmware** — Hailo firmware blobs need to land in `/lib/firmware/hailo/` on the rootfs; a build-system post-step, not code.
  4. **Userspace (HailoRT + hailortcli)** — no official Buildroot package exists; a Hailo community forum thread documents someone else attempting exactly this and hitting build issues with a hand-written `hailort.mk`. Check whether Hailo has any official Yocto/OpenSTLinux integration story for ST specifically before assuming Buildroot is the path — this is a new open item given the chip swap (previously scoped against NXP's Yocto BSP).
  - No prior public precedent exists for STM32MP257 specifically — the closest documented case remains Hailo-8 manually ported onto an **RK3588** kernel (different vendor's BSP). Budget this as its own multi-day task, not a `modprobe` away.
- **Swap-in-place design intent**: keep the module socketed, not soldered. If a board respin is needed for DDR/PCIe reasons, the Hailo-8L moves to the next revision unchanged.
- **Recovery/bring-up tool**: check ST's equivalent to NXP's Serial Download Protocol (USB-based recovery boot with no storage required) — STM32MP2's boot ROM likely has an analogous USB DFU/serial recovery mode; confirm against the reference manual before relying on it.

## Camera

- MIPI-CSI sensor module (OV5647/IMX219-class ribbon-cable board), matched to STM32MP257's camera input (up to 2 data lanes, 2.5 Gbit/s per lane).
- STM32MP257 has a basic onboard ISP (demosaicing included) — better starting position than the i.MX8M Nano plan, which had no ISP at all and would have pushed all image tuning to software/Hailo.

## Network connectivity

**One Gigabit Ethernet port**, using **RTL8211F-CG** (LCSC C187932, $1.42) + **HR911105A** magnetics-integrated RJ45 jack (LCSC C12074) — confirmed real parts, reused from the EV1's design. The EV1 itself populates **three** RGMII PHYs (confirmed: U7/U8/U9 = RTL8211F-CG, matching STM32MP257's three on-chip Ethernet MACs per ST's product page) — this board only needs one, consistent with the cost-conscious approach throughout this project.

**Circuit, pulled directly from the EV1's real ETH1 schematic sheet:**
- **RGMII bus**: TXD[3:0], RXD[3:0], TX_CTL, RX_CTL, TXC (GTX_CLK), RXC, wired to one of STM32MP257's Ethernet MAC instances (ETH1/ETH2/ETH3 — any one).
- **Series termination**: 22Ω resistors on TXD/RXD/TXC/RXC lines, explicitly noted on the schematic as "22 ohms to place near NPU" (i.e., near the SoC, not the PHY) — a specific placement instruction worth following, not just a value.
- **RX-side pull-ups**: 4.7kΩ on RXD[3:0] and RXCTL — these double as PHY strap-configuration pins sampled at reset.
- **MDIO/MDC**: 1.5kΩ pull-ups on the management bus.
- **Clock**: a 25MHz crystal (with two 10pF load caps) directly on the PHY, **not** an external clock into `EXT_CLK` — matches the safer approach flagged earlier from the ST community thread's documented clocking pitfall on a different reference board (MB1272/STM32MP157).
- **PHY address strap**: set via the RXD/RXCTL pull-up pattern at reset — the EV1 uses address `0x04` for its first port; since this board has only one PHY, the exact address matters less (just needs to be consistent between hardware strap and software/device-tree config) but should still be explicitly chosen, not left to floating defaults.
- **LEDs**: link/activity LEDs (green/orange typical) driven directly from the PHY's LED0/LED1 pins through the RJ45 jack's integrated LEDs — no separate LED driver needed, the HR911105A-class jack has LEDs built into the magnetics module.
- **Power**: RTL8211F-CG's separate rail pins (DVDD33/AVDD33, DVDD10/AVDD10) — the EV1 brings in a real external 1.0V rail rather than relying solely on the PHY's internal LDO option; worth deciding whether to do the same or use the simpler single-3.3V-input internal-LDO configuration discussed earlier, given this board's lower part-count goals.

**Open items**: confirm which STM32MP257 Ethernet MAC instance to use (ETH1/2/3 — functionally equivalent, pick whichever eases pin routing), decide on external 1.0V rail vs. PHY-internal LDO for the lower voltage rails, and set a definite PHY address strap pattern.

## Memory / storage

### DDR selection: DDR4 (switched back from LPDDR4)

**Decision reversed.** LPDDR4 (Samsung K4F8E3S4HD-MGCL) is dropped as the primary memory type. **Rationale**: that part was never on ST's validated-DRAM list (Micron/Nanya/Winbond/ISSI only), and LPDDR4's configuration surface (swizzle mapping, rank/channel structure, CubeMX wizard specifics) is more novel and less forgiving of a hardware/configuration mismatch than standard DDR4 — the ST community's documented PHY training failure (two similar-looking Micron LPDDR4 parts, different internal die structure, one config broke the other) is exactly the failure mode judged not worth the risk here. DDR4 is a more mature, better-understood interface on this SoC family, and reduces the number of open/unverified assumptions the board depends on.

**New pick: Samsung K4A8G165WG-BCWE** (LCSC C41368581) — DDR4, **8Gbit**, **x16**, FBGA-96 package, 1.2V, **confirmed in stock at LCSC**. Matches STM32MP257's "16 bits: single BGA in p2p" DDR4 mode exactly (per AN5723) — same simple point-to-point topology LPDDR4 offered, without LPDDR4's newer/less-traveled configuration surface. Same package family (FBGA-96) as the Micron part used on the actual EV1 (MT40A1G16TB-062E), just single-chip 16-bit here rather than the EV1's two-chip 32-bit.

**Supersedes an earlier pick, K4A4G165WE-BCTD (4Gbit)** — that part could never be confirmed in stock anywhere. K4A8G165WG-BCWE is a genuine upgrade on two axes at once: real, confirmed availability, and double the capacity (8Gbit/1GB vs. 4Gbit/512MB) at no topology cost — same package, same x16 organization, same single-chip point-to-point wiring.

**Verified compatible, not just assumed**: pulled Samsung's actual datasheet for this part family (K4A8G165WC, a closely related die revision sharing the same organization/package/voltage) — confirms 8Gb, x16, 96FBGA, 1.2V, matching every requirement already established. Density change from 4Gbit → 8Gbit is a **CubeMX configuration update only** (the "DDR Density" field in the wizard) — it does not change the physical ball mapping below, since that's protocol-defined (DDR4 signal function per `DDR_Ax` ball), not density-dependent.

**Open item carried forward, now against the correct part**: confirm single-rank status directly from K4A8G165WG-BCWE's own datasheet (not just the sibling WC revision) before finalizing — a monolithic x16 die at this density is essentially always single-rank, and AN5723's "single rank only required... up to 4-Gbyte total density" comfortably covers our 1GB, but this project has been burned before by assuming rather than confirming on memory parts.

**Now that both boards use DDR4 (this board and the EV1), the EV1 is a substantially better reference than it was under the earlier LPDDR4 plan.** The pin-*function* mapping below was already sourced from the EV1's own DDR4 validation wiring (AN5723 Table 17), but with both designs now sharing the same protocol, it's also worth pulling the EV1's actual schematic sheet for: the VTT termination network implementation (topology, resistor values, placement relative to the fly-by bus end), the VREF divider circuit, and PMIC rail sequencing for VDDQ/VPP/VTT/VREF — even though the EV1's topology is 32-bit/two-chip fly-by versus our 16-bit/single-chip point-to-point, the per-signal termination/reference circuit design (not the bus topology itself) should transfer directly. This is a concrete open item below.

### VTT / VREF / VPP / ZQ circuits — fully resolved with real values from the EV1's actual schematic (Sheets 4 and 6)

Pulled directly from `mp257_schematic.pdf` (Power PMIC sheet + 2_DDR4_16bits sheet) — every value below is read straight off the real schematic, not inferred from netlist connectivity alone. This closes out every open item this project had on DDR4 power/termination.

**Rail voltages, confirmed on the Power PMIC sheet:**

| Rail | PMIC source | Voltage |
|---|---|---|
| VDD_DDR (VDDQ) | BUCK6 | 1.2V (with 0.68µH inductor + 22µF output caps) |
| VTT_DDR | LDO3 | **0.6V** |
| VREF_DDR | REFDDR (dedicated PMIC output) | **0.6V** |
| VPP_DDR | BUCK7 | **2.5V** (with 0.68µH inductor) |

All four match AN5723's stated DDR4 requirements exactly (VDDQ 1.2V, VREF 0.6V, VPP 2.5V) — no surprises, just confirmation with real component values attached.

**The VREF_DDR "single resistor" mystery — resolved, and it was never a divider.** R32 sits directly between the PMIC's dedicated **REFDDR** pin and the VREF_DDR net, with a 1µF cap (C52) for filtering. This is a **direct reference output from the PMIC**, not a resistor-divider bias — there was never a missing second resistor to find. `REFDDR` is literally one of AN6198's named PMIC outputs (previously flagged as needing configuration), and this confirms it's wired straight to the DDR4 chips' VREF pins through one small series/filter resistor.

**Termination network — real values, not estimated:**
- **VTT termination resistors: 56Ω each**, one per CA/control signal (A0–A13, BA0/1, BG0, ACT_N, CS_N, CKE, ODT, RAS_N, WE_N) — confirmed directly on the schematic, terminating each signal to VTT_DDR after both DRAM chips (consistent with fly-by topology terminated at the far end).
- **Differential CLK termination (DDR_CLK_P/N): 100Ω** (R56).
- **DDR_RESETN pull resistor: 10kΩ** (R55).
- **ZQ calibration resistors: 240Ω each** — confirmed for all three: R57 (SoC side, on U1), R58 and R59 (one per DRAM chip, U5/U6) — matches standard JEDEC ZQ calibration practice exactly.
- **`DDR_VREF`** (SoC-side pin, distinct from the PMIC's VREF_DDR output): connects only to a test point (TP8) — confirms AN5723's statement that "device side VREF is generated internally" for DDR4; this pin is for measurement, not externally driven.

**Practical note for our single-chip, point-to-point board**: the EV1's 56Ω/240Ω/100Ω/10kΩ values were derived for its 32-bit, two-chip, fly-by topology. Our 16-bit single-chip point-to-point design has a different trace topology (no fly-by multi-drop), so these values are a strong, real starting point — but worth a final check against AN5724's point-to-point-specific termination guidance rather than copying them completely blind, since termination requirements are topology-dependent, not just protocol-dependent.

**Practical takeaway**: the VTT/VPP requirement that looked like it might need extra BOM parts (an additional regulator) turns out to be fully covered by the PMIC already selected — the real addition is the termination *resistor network* (56Ω per signal, 240Ω for ZQ, 100Ω for CLK) and its decoupling capacitors, not a new active component.

**Two real complexity trade-offs, worth stating plainly rather than glossing over — this is the cost of the LPDDR4→DDR4 reversal:**
1. **DDR4 needs an additional VPP = 2.5V supply**, confirmed via AN5723 ("An additional VPP = 2.5V is required"). LPDDR4 needed only VDDQ = 1.1V. This is a new rail to add to the PMIC configuration — check whether STPMIC25 has a spare LDO/buck capable of this, or whether a small additional VPP regulator is needed.
2. **DDR4 needs external VREF and VTT on the PCB for the CA bus** (per AN5723: "External VREF and VTT are used at SDRAM" for DDR4, vs. LPDDR4's "VREF for AC and DQ is generated internally"). This reverses the routing simplification LPDDR4 offered earlier in this project — the VTT termination network and its resistor array are back in scope for layout.

**Confirmed DDR4 electrical/topology parameters (AN5723 Section 7.1):**
- VDDQ = 1.20V, VREF = 0.6V, VPP = 2.5V (external, additional)
- Frequency range: 625–1200MHz (below 625MHz needs DLL off, not validated ≤125MHz)
- RON = 40Ω (device, via ATxImpedance/TxImpedance), ODT = 53Ω (device) / 60Ω (DDR4 side, via MR1/MR5)
- Address mapping: Row > BA1/BA0/BG0 > columns, 10 column bits for a 16-bit device
- **Single rank only required** to support up to 4-Gbyte total density — our 8Gbit (1GB) part is comfortably within this, though the same "confirm against the real datasheet, don't assume" caution applies (see open item above).

**Sourcing note**: given the current DRAM supply crunch discussed throughout this project, order spares once a confirmed-available SKU is locked in.

### DDR4 ball mapping — from AN5723 Table 7 (device-versus-protocol pin mapping) and Table 17 (DDR4 validation board reference wiring)

Same underlying mechanism as LPDDR4: `DDR_A0`–`DDR_A7` are fixed/non-swizzleable; `DDR_A8`–`DDR_A31` can be freely reassigned to ease routing, and whatever assignment is chosen gets fed into the CubeMX DDR tool.

**Fixed, non-swizzleable (`DDR_A0`–`A7`):**

| STM32 ball | DDR4 signal |
|---|---|
| DDR_A0 | CKE0 |
| DDR_A1 | CKE1 (unused — single rank) |
| DDR_A2 | CS_N0 |
| DDR_A3 | ODT0 |
| DDR_A4 | CLK0_T |
| DDR_A5 | CLK0_C |
| DDR_A6 | CS_N1 (unused — single rank) |
| DDR_A7 | ODT1 (unused — single rank) |

**Swizzle-configurable (`DDR_A8`–`A31`) — adopting ST's own DDR4 validation board wiring directly (AN5723 Table 17) as the working assignment, rather than inventing a new one:**

| STM32 ball | DDR4 signal | STM32 ball | DDR4 signal |
|---|---|---|---|
| DDR_A8 | A[3] | DDR_A22 | A[4] |
| DDR_A9 | BA[1] | DDR_A23 | A[10] |
| DDR_A10 | A[12] | DDR_A25 | A[13] |
| DDR_A11 | RAS_N | DDR_A26 | A[5] |
| DDR_A12 | A[6] | DDR_A27 | A[9] |
| DDR_A13 | A[0] | DDR_A28 | A[11] |
| DDR_A14 | A[2] | DDR_A29 | A[7] |
| DDR_A15 | A[8] | DDR_A30 | CAS_N |
| DDR_A17 | ACT_N | DDR_A31 | A[1] |
| DDR_A18 | BG[0] | DDR_A19 | PAR (not used) |
| DDR_A20 | WE_N | DDR_A24 | (pin not used) |
| DDR_A21 | BA[0] | | |

**Also fixed, single-location, from the official VFBGA361 ballout diagram** (unchanged by the DDR4 switch — these are SoC package balls, not protocol-dependent): `DDR_RESETN` (E17), `DDR_VREF` (K17), `DDR_ZQ` (G16), `DDR_DQM0/1` (T17/C19), `DDR_DQS0N/P` (V18/19), `DDR_DQS1N/P` (B18/19), plus `VDDA18DDR` (D16).

**DQ pin mapping**: confirmed via AN5723 Table 8 that the SoC internally swizzles DQ bits between the physical device pin and the internal DFI signal (e.g., device `DQ0` corresponds to internal signal `DQ2`) — this is handled automatically by the CubeMX tool's generated swizzle registers once PCB connections are entered; not something to hand-calculate.

**Next step**: run CubeMX's DDR wizard with DDR4 selected (16-bit, **8Gbit** density, single rank), enter the Table 17-derived PCB connections above, and let the tool generate the real `HWTSWIZZLE*`/`DDRDBG_DDR34_AC_SWIZZLE*` register values — same workflow already used for LPDDR4, just against the DDR4 tool path this time and the updated density.

### Archived: LPDDR4 pinout (preserved in case of reverting)

Kept here for reference, not deleted, in case LPDDR4 is reconsidered later.

**Would-have-been chip**: Samsung K4F8E3S4HD-MGCL (LCSC C2920231), 8Gb, x32, 200-ball FBGA, confirmed genuine LPDDR4 (not X, VDD1/VDD2/VDDQ = 1.8V/1.1V/1.1V), confirmed standard 2Ch×x16 organization — not on ST's validated list, footprint/electrically compatible pending verification only.

**LPDDR4 CA bus (swizzle-configurable), from CubeMX's Mapping tab:**

| LPDDR4 signal | STM32 ball | LPDDR4 signal | STM32 ball |
|---|---|---|---|
| CAA0 | DDR_A2 | CAB0 | DDR_A14 |
| CAA1 | DDR_A3 | CAB1 | DDR_A15 |
| CAA2 | DDR_A8 | CAB2 | DDR_A20 |
| CAA3 | DDR_A9 | CAB3 | DDR_A21 |
| CAA4 | DDR_A10 | CAB4 | DDR_A22 |
| CAA5 | DDR_A11 | CAB5 | DDR_A23 |

**LPDDR4 fixed signals** (CKEA0/1: DDR_A0/1; CLKA_T/C: DDR_A4/5; CSA0/1: DDR_A6/7; Channel B equivalents on DDR_A12/13/16/17/18/19, unused in 16-bit config).

**Why LPDDR4 had been attractive**: simpler routing (on-die termination, no external VTT/VREF needed), and better DRAM-market sourcing position at the time — both genuine advantages, outweighed by the validated-list/configuration-risk concern above.

**Open verification that was never resolved for LPDDR4**: Samsung K4F8E3S4HD-MGCL's actual rank count against its real datasheet — CubeMX's `NUMRANK_DFI0=1` was our configuration choice, not confirmed proof of the physical chip's structure.

### Storage and power

- **Storage: eMMC (primary) + microSD (fallback boot source) — matches the EV1 reference design exactly.** ST's own community confirms the EV1 has physical DIP switches specifically to choose eMMC or SD-card as boot device — this board copies that same dual-source design rather than picking one over the other.
  - **eMMC**: **Kingston EMMC04G-M627-Y02U** — 4GB, **FBGA-153** package (11.5×13×1.0mm), the same standardized footprint Kingston uses across their whole 4GB–256GB eMMC line (and broadly shared across the eMMC industry). Despite 153 physical ball positions, only a compact cluster is active (8-bit data bus D0–D7, CMD, CLK, RST_n, plus VCC/VCCQ/VSS) — the rest are NC/reserved, standard practice so one footprint serves multiple densities/vendors. Genuinely simpler to route than the ball count suggests. Wired to **SDMMC2** (matching the EV1's convention).
  - **microSD**: simple connector addition (~$1–2, no BGA), wired to **SDMMC1** (the EV1's SD-card interface) — a separate SDMMC controller instance, so eMMC and SD don't compete for the same signal set.
  - **Boot source selection**: OTP fuses define which sources are valid boot candidates (a primary + secondary pairing, confirmed supported for two *different* memory kinds like eMMC + SD — not supported for two SD cards), and a physical **DIP switch** (matching the EV1's own approach) or simple strap resistors select which one is actually used at power-up.
  - **Why keep both rather than choosing**: eMMC gives the product-like feel and the confirmed DFU flashing workflow below; microSD gives a genuine fallback if eMMC ever fails, plus fast dev-image swapping without needing the USB/DFU tool at all. Low incremental cost (connector + one more PMIC rail to configure) for real redundancy and dev convenience — not a compromise, since it's exactly what ST's own reference board does.
- **Flashing workflow (primary, via eMMC), confirmed real and standard**: strap `BOOT[3:0] = 0000` to force USB DFU boot mode, connect via USB-C, flash with **STM32CubeProgrammer** (ST's official tool; CLI or GUI) — confirmed directly from ST's own app note (**AN5275**, "Introduction to USB DFU/USART protocols used in STM32MP1 and STM32MP2 MPU bootloaders") and multiple real community threads flashing eMMC on STM32MP257F-DK/EV1 exactly this way. This is the standard production flashing path, not a workaround — AN5727 explicitly describes booting from USB as the intended method for flashing eMMC in production. Decision driver: USB stays connected for power anyway, so DFU flashing adds negligible workflow friction versus a removable card.
- **Boot-select circuit — confirmed from the EV1's actual schematic (BOOT MODE sheet)**: four 1kΩ pull-ups from VDDIO into a 4-pole DIP switch (**DSWB04LHGET, LCSC C99418, $0.21, 4-position, through-hole, 2.54mm pitch — 97,980 in stock**, the best price/availability match on LCSC for a genuine 4-position switch; through-hole is a deliberate choice here, not a compromise, since it's easy to hand-inspect/rework this low-current strap signal during bring-up), one pole per BOOT bit (SW1→BOOT0, SW2→BOOT1, SW3→BOOT2, SW4→BOOT3). Switch open = that bit floats/reads 0; switch closed = pulled to VDDIO, reads 1. Real BOOT[3:0] codes for the A35 master (the core running the main OS):

  | Purpose | BOOT[3:0] | Switch setting |
  |---|---|---|
  | Normal boot — eMMC | `0010` | Close SW2 only |
  | Fallback boot — SD-Card | `0001` | Close SW1 only |
  | Force DFU (USB/UART), for flashing | `0000` | All switches open |

  This single DIP switch circuit serves both the eMMC/USB boot-mode selection and the eMMC-vs-SD fallback selection — one component covers what were two separate open items.
- **PMIC: STPMIC25** (STPMIC2 family, VQFN) — part number confirmed from the STM32MP257F-EV1's actual BOM (designator U26: STPMIC25APQR), replacing the earlier placeholder (PCA9450, carried over from the dropped i.MX8M Nano plan). Not a place to cut cost — multi-rail power-up sequencing errors are easy to mistake for DDR problems when debugging.
- **Variant: STPMIC25A.** Per AN6198: A and D are both wall-adapter (5V) profiles; D's differentiator is enabling LDO7/LDO8 in bypass mode specifically for SD-Card UHS-I boot, while **A is configured for eMMC/USB boot** — matching eMMC as this board's primary boot source. If SD-card boot ever needs to be the *default* rather than fallback, revisit D.
- **New PMIC rail to configure: VDDIO_SDCARD.** Confirmed from the EV1's own netlist as a rail distinct from general VDDIO, connecting directly to both U1 (SoC) and U3 (PMIC) — the microSD slot's I/O voltage needs its own dedicated PMIC output, alongside the BUCK6/VDD_DDR configuration already required below.
- **Critical: none of the three variants (A/B/D) pre-configure DDR power at all.** Checked AN6198's actual NVM register table directly: BUCK6 (VDD_DDR), LDO3 (VTT/VDD1_DDR), and REFDDR (VREF_DDR) are all **OFF, Rank 0, identically across A, B, and D**. This isn't an oversight — DDR voltage depends on DDR type (1.35V/1.2V/1.1V for DDR3L/DDR4/LPDDR4), which varies per board design, so ST can't bake a correct value into a generic profile. **DDR power is a required custom configuration step regardless of which variant is chosen.**
- **For DDR4 specifically, all three DDR-related regulators are needed — this is more, not less, than LPDDR4 would have required.** Per AN5723's actual DDR4 requirements: VDDQ = 1.20V (BUCK6), external VREF = 0.6V for the CA bus (REFDDR), and external VTT at the SDRAM (LDO3) — DDR4 needs the "External VREF and VTT ... used at SDRAM" that LPDDR4 would have let us skip. **Plus VPP = 2.5V**, confirmed required by AN5723 for DDR4 specifically — **confirmed resolved**: the EV1's actual netlist shows both VTT_DDR and VPP_DDR sourced directly from U3 (STPMIC25) — no additional regulator needed, see the "VTT / VREF / VPP / ZQ circuits" section under Memory above.
- **Reconfiguration path confirmed available**: STPMIC25 has a full I2C interface explicitly supporting voltage/mode/rank changes at runtime, so even if this needs correcting after using a variant's default, it's a software/firmware task, not a hardware blocker.
- **Open item**: determine the exact BUCK6/LDO3/REFDDR voltage/rank programming sequence for DDR4 — likely covered in AN5727, not yet pulled in full.

### Board input power: USB-C only, no barrel jack

**No power jack needed** — confirmed directly against the EV1 reference design, which supports both a barrel jack (CN20, 5V/3A adapter) and USB-C (CN21) via a jumper; USB-C alone is one of its two fully valid configurations, not a fallback.

- **Basic USB-C power (chosen), no PD negotiation stack**: bias CC1/CC2 per the USB-C spec to advertise/accept the default 5V/3A (15W) tier — passive components only, no firmware. Per ST's own USB-PD documentation: *"using the Type-C only may be enough, for example, if you need only 5V/3A. In that case, you do not need an MCU with the UCPD peripheral inside."*
- **Rough power budget confirms this tier is sufficient**: STM32MP257 (~2–3W) + LPDDR4 (<1W) + Hailo-8L (~1.5–2.5W) + camera (~0.5W) + eMMC/SD (minor) + regulator overhead ≈ **6–8W total**, comfortably inside the 15W ceiling with real margin.
- **STM32MP257 has UCPD (USB-PD controller) built in** — confirmed via ST's own documentation listing STM32MP2 among the series with embedded UCPD silicon, matching the `UCPD1_CC1`/`UCPD1_CC2` balls already in the ballout. If full PD negotiation is ever wanted later (e.g. requesting 9V for headroom), the hardware capability already exists — it would need firmware plus a companion protection chip, not a SoC change.
- **Optional addition: TCPP0x protection chip** — *superseded, see below*. Reusing a proven subcircuit from a previous board instead.
- **Reused USB-C + protection subcircuit, from a prior board's BOM (`solar-ppm.csv`)** — real, already-proven parts, not a new design:
  - **USB1: TYPE-C16PIN connector** (LCSC C393939) — the USB-C connector itself.
  - **D3: USBLC6-2SC6** (LCSC C2687116) — 2-line USB ESD protection array (SOT-23-6), purpose-built for D+/D− protection. This is the "data protection" piece.
  - **D1: SMBJ5.0A** (LCSC C19077558) — 5V standoff TVS diode (SMB) for VBUS surge/ESD protection.
  - **R1, R2: 5.1kΩ** (LCSC C25905) — CC1/CC2 pull-down resistors, the standard value for a USB-C sink device advertising acceptance of default 5V/3A power. Matches the "basic power tier, no PD stack" approach already decided above.
  - **Scope note**: USBLC6-2SC6 protects D+/D− only, not the CC lines themselves — acceptable given this board uses passive CC pull-downs rather than a full PD negotiation IC, but worth knowing it isn't blanket protection across every USB-C pin.

Also, per the earlier DFU question: `USB3DR_DP`/`USB3DR_DM` (STM32MP257's USB 3.0 dual-role data lines, confirmed present in the official ballout) must be routed to this same USB-C connector's data pins alongside the power wiring above — one connector, one cable, handling both board power and DFU flashing from a laptop.

## Board strategy: full board, not staged minimal

Considered and explicitly rejected: building a stripped "DDR-proof-only" board first (SoC + PMIC + DDR4 + crystal + a USB-based recovery boot mode, no eMMC/SD/camera/M.2) before committing to the full design. That staged approach would isolate DDR bring-up from every other risk and meaningfully lower the cost of a first bad spin — but the decision here is to build the full board (DDR4 + eMMC + camera + M.2/PCIe) in one go and treat a respin as the accepted cost of debugging, rather than sequencing the project into two board revisions. Documented here so the trade-off is visible later, not because it's the "correct" choice — either is defensible, and this project is going with placing everything on the first spin.

## STM32MP257F-EV1 reference design — summary

ST provides full design resources for this evaluation board under an Open Platform License Agreement that explicitly permits reuse (schematics, CAD databases, Gerbers, BOM — via CAD Resources on the STM32MP257F product page). Board MB1936, revision E01 (Dec 2025). Key parts pulled from its real BOM/netlist — PMIC, PCIe clock generator, camera connector — are cited inline above where each is used; see Memory and Storage/power sections. The EV1 uses a different SoC package (FAI3/TFBGA436) and 32-bit DDR4 with two chips in fly-by topology (vs. our FAL3/VFBGA361, 16-bit DDR4 with a single chip in point-to-point) — now that both boards use **DDR4** (after the LPDDR4→DDR4 reversal), the EV1's Table 17 DDR4 pin mapping and general DDR4 rail/topology conventions are a much closer, more directly reusable reference than before.

**Schematic status (own design, ai-vision.net)**: U1 = STM32MP257FAL3, U2 was ISSI IS43LQ32256A-062BLI (LPDDR4, 8Gb, x32) — **needs to be swapped to the new DDR4 pick, Samsung K4A8G165WG-BCWE (LCSC C41368581, 8Gbit, x16, FBGA-96, confirmed in stock)**, and the DDR4 pin mapping above wired in place of the archived LPDDR4 mapping. As of the last LPDDR4-era check, zero DDR nets connected the two; that's unchanged and now applies to the DDR4 net names instead.

## PCB / layout notes

### Stackup — derived from studying the EV1's actual layer table

6 layers, ~1.6mm total, confirmed layer function assignment (from the EV1's stackup table plus follow-up analysis, not just the raw thickness numbers):

| Layer | Function | Reference |
|---|---|---|
| L1 (Top) | DDR signals | Tight 0.072mm gap to L2 (GND) — clean microstrip |
| L2 | Solid GND plane | — |
| L3 | GPIO + VBUS (low-speed/DC content) | Loose 0.5mm gap to L2 — deliberately used for nets that don't need a tight reference |
| L4 | Signal (lower-speed, TBD) | Loose 0.5mm gap to L5 |
| L5 | GND, **with a VDD_DDR pour split in the middle** (island under/near the DRAM footprint) | Acts as AC reference for L6 despite the local power-plane island |
| L6 (Bottom) | DDR signals | Tight 0.072mm gap to L5 — clean microstrip, mirrors L1 |

**Why this works despite L1/L3 sharing one plane (L2)**: a solid, continuous ground plane has low enough impedance that return-current coupling between the two sides is generally much weaker than same-layer crosstalk — this is standard practice, not a flaw, and ST's own partitioning (fast DDR on L1, slow/DC GPIO+VBUS on L3) is a sensible way to manage it: nets with little high-frequency content don't inject much energy into the shared return path in the first place.

**Critical layout constraint — verify before routing**: any DDR trace on L6 must stay entirely within whichever region of L5 it starts over (GND or the VDD_DDR island) — a trace whose return path has to cross from one to the other hits an impedance discontinuity and a return-current detour at exactly that point, a classic DDR layout mistake. Since the SoC and DRAM are placed close together (see placement below), this should be straightforward to satisfy, but check it explicitly during layout rather than assuming. Add decoupling/stitching capacitors between VDD_DDR and GND along that boundary regardless, to give any stray high-frequency return current a short path back to true ground.

### Physical placement

- **STM32MP257 (U1)**: center of the board.
- **DDR4 (U2)**: to the right of the SoC, close enough that the DDR signal footprint on L1/L6 stays contained within the L5 VDD_DDR island's boundary (see constraint above).

### JLCPCB stackup selection — resolved

**Pick: JLC06161H-1080** (not "No requirement"). Compared directly against JLCPCB's published 6-layer stackup options (jlcpcb.com/impedance), narrowed to the simple, symmetric, single-prepreg-per-gap options (several other named options use multi-prepreg stacks or extra embedded cores — more complex, not needed here):

| Option | Outer prepreg (L1–L2 / L5–L6) | Dk (outer) | Inner core | Middle prepreg (L3–L4) | Dk (middle) |
|---|---|---|---|---|---|
| No requirement / JLC06161H-3313 | 3313, 0.0994mm | 4.1 | 0.55mm | 2116, ~0.11mm | 4.16 |
| **JLC06161H-1080 (chosen)** | 1080, **0.0764mm** | **3.91** | 0.55mm | 7628, 0.2104mm | 4.4 |
| JLC06161H-1080A | 1080, 0.0764mm | 3.91 | 0.6mm | 3313, 0.0994mm | 4.1 |
| JLC06161H-7628 | 7628, 0.2104mm | 4.4 | 0.4mm | 7628, 0.2028mm | 4.4 |

**Why**: L1 and L6 carry the DDR interface, routed directly adjacent to two fine-pitch BGAs (STM32MP257 and the DDR4 chip). Outer-layer dielectric thickness directly sets the trace width needed to hit a given target impedance — thinner dielectric allows narrower traces for the same 50Ω/85Ω target, which matters most exactly where BGA escape-routing space is tightest. JLC06161H-1080's 0.0764mm outer prepreg is the thinnest among the simple symmetric options, giving the most escape-routing headroom near the BGAs. Still fully symmetric top-to-bottom, so L1 and L6 (both DDR) compute to the same trace width from one impedance-calculator pass. The thicker/higher-Dk middle prepreg (7628, 0.21mm) is a non-issue since L3/L4 carry lower-speed GPIO/VBUS content that doesn't need tight impedance control.

Workflow once the stackup is locked:
1. Select JLC06161H-1080 explicitly in JLCPCB's ordering tool (not "No requirement" — guarantees consistent dielectric across this run and the likely respin).
2. Run JLCPCB's impedance calculator against this locked stackup, once per signal class: single-ended ~50Ω (DDR CA/DQ), differential ~85–100Ω (DDR CK/DQS, PCIe, MIPI-CSI — each may need its own pass since target impedances differ slightly by interface).
3. Enter the resulting widths into KiCad net classes/diff-pair rules before routing.

### Other notes

- **DDR4 needs external VTT termination and external VREF for the CA bus** (per AN5723) — unlike the LPDDR4 plan this project briefly considered, which would have used on-die termination only. Budget PCB area and a VTT resistor network near the SDRAM for this — see Memory section for the full VPP/VTT/VREF rail discussion.
- Wire up the debug UART (early boot, pre-Linux) from the first prototype — the primary DDR bring-up diagnostic given no in-house DDR controller (unlike the FPGA+DDR3L board).
- PCIe routing to the M.2 connector: length-matching and impedance rules per ST's PCIe layout guidance (pull from the STM32MP257 datasheet/reference manual before layout).
- Follow AN5724 specifically for the DDR4 topology/stackup rules (16-bit single-chip point-to-point, per AN5723's DDR4 section) — this is ST's dedicated DDR routing app note, more targeted than a general hardware developer's guide.

## Bill of materials (rough, prototype quantities)

| Item | Est. cost (qty 1) |
|---|---|
| STM32MP257FAL3 (361-ball package) | $26.72 |
| **PMIC: STPMIC25APQR** (variant A — confirmed match for eMMC/USB boot; see Storage/power notes) | **$7.99** |
| **DDR4: Samsung K4A8G165WG-BCWE, 8Gbit x16, FBGA-96** (LCSC C41368581, confirmed in stock) | **$45.84** |
| VTT/VREF/ZQ termination network — real values confirmed (56Ω×~19 VTT resistors, 100Ω CLK diff termination, 240Ω×3 ZQ, 10kΩ reset pull) from EV1 schematic, see Memory section | ~$2–4 |
| **Storage: Kingston EMMC04G-M627-Y02U** (4GB eMMC, FBGA-153, primary boot) | ~$22.72 |
| microSD connector (fallback boot source, SDMMC1 — matches EV1 reference design) | ~$1–2 |
| **Boot-mode select: DSWB04LHGET 4-pole DIP switch** (LCSC C99418) + 4x 1kΩ pull-ups | ~$0.21 + ~$0.02 (resistors) |
| **USB-C connector: TYPE-C16PIN** (LCSC C393939, reused from prior board) | ~$0.50–1 |
| **USB ESD/protection: USBLC6-2SC6** (LCSC C2687116, D+/D− protection) + **SMBJ5.0A** (LCSC C19077558, VBUS TVS) + **2x 5.1kΩ CC pull-downs** (LCSC C25905) — reused subcircuit | ~$0.50–1 |
| **MIPI-CSI camera connector: Molex 524372271** (22-pin, 0.5mm FFC/FPC — confirmed, ST reference design) | ~$1–3 |
| MIPI-CSI camera module (sensor + FFC cable) | ~$10–25 |
| **Ethernet PHY: RTL8211F-CG** (LCSC C187932) | **$1.42** |
| **RJ45 jack: HR911105A** (LCSC C12074, magnetics + LEDs integrated) | ~$0.50–1 |
| Ethernet supporting passives (22Ω series ×8, 4.7kΩ RX pull-ups, 1.5kΩ MDIO pull-ups, 25MHz crystal + 2×10pF caps) | ~$1–2 |
| M.2 connector (B+M key) | ~$1–3 |
| **PCIe reference clock generator: Microchip DSC557-0344FI0-T** (crystal-less, 100MHz, Gen1/2/3 — confirmed, ST reference design) | ~$2–5 |
| Hailo-8L module | ~$70 (not soldered — one-time, reusable across spins) |
| Passives, connectors, misc | ~$10–20 |
| PCB fab (6-layer, impedance control, ENIG, via-in-pad) + SMT assembly (two BGAs: SoC + eMMC, plus PMIC) | ~$120–220 |

Rough total for one spin (excluding Hailo module, since it's reusable): **~$238–360**, now that the PMIC ($7.99), DDR4 chip ($45.84), and Ethernet PHY ($1.42) are confirmed prices rather than estimates — reflecting eMMC as primary storage plus a microSD fallback (connector + boot-select switch, low incremental cost), one Gigabit Ethernet port (RTL8211F-CG + RJ45), the added second-BGA assembly step (via-in-pad, inspection), and the VTT/VREF termination network the DDR4 switch requires (VPP confirmed covered by the PMIC, no extra regulator needed). A first DDR4 attempt can still need a respin — factor that into the realistic total, plus engineering time.

**For context**: a Raspberry Pi CM5 + IO board + official AI Kit + camera comes in around **$185–255**, fully assembled and working out of the box. This board is not the cheaper option — its value is the DDR/PCIe routing experience and the stronger hardware-portfolio narrative, not cost.

## Open items / risks to resolve before layout

1. **Swap the schematic memory chip symbol from ISSI IS43LQ32256A (LPDDR4) to Samsung K4A8G165WG-BCWE (DDR4, confirmed in stock)** and wire per the new DDR4 ball mapping (see Memory section) — no DDR nets currently connect U1 to U2 regardless of chip, so this is a clean cutover.
2. **Update the CubeMX DDR wizard density setting to 8Gbit** (was 4Gbit under the superseded pick) and confirm single-rank status against K4A8G165WG-BCWE's own datasheet specifically (not just the sibling WC revision referenced so far).
3. **Pull the exact BUCK6/LDO3/REFDDR/BUCK7 NVM programming sequence** (rail voltages now confirmed: VDD_DDR 1.2V, VTT/VREF 0.6V, VPP 2.5V — the remaining piece is the I2C/NVM register sequence to enable them, likely in AN5727, not yet fully reviewed).
4. Device-tree/config PCIe root-complex enablement (clocks, reset, M.2 reference clock — now sourced via the confirmed Microchip DSC557-0344FI0-T clock generator) — real hardware bring-up work tied to this board's specific layout, separate from the Hailo driver itself.
5. Check whether Hailo has any official Yocto/OpenSTLinux integration path for ST chips specifically, given the earlier finding that Hailo's only confirmed official Yocto support was for NXP (i.MX8M Plus).
6. Scope the Hailo-8L Linux driver/firmware/userspace stack as an explicit, time-boxed task (see four-part breakdown above) — treat as original integration work regardless of host chip.
7. Budget for a likely second fab/assembly spin — treat as expected cost, not failure (see "Board strategy" above).
8. **Verify during layout** that no DDR trace on L6 crosses the L5 GND/VDD_DDR pour boundary, given the SoC-center / DRAM-right placement.
9. Decouple `VDDA18DDR` (D16) alongside the existing VDDQDDR/VDD_DDR decoupling plan — a third DDR-related rail found in the official ballout, not yet reflected in a cap placement plan.
10. **Wire SDMMC1 to the microSD connector** (separate from SDMMC2/eMMC) and confirm OTP configuration supports eMMC + SD as a valid primary/secondary boot pair (confirmed supported for two different memory kinds; not supported for two SD cards).
11. **Configure VDDIO_SDCARD on the PMIC** — a rail distinct from general VDDIO, confirmed from the EV1 netlist as directly connecting U1 and U3; needs the same kind of custom NVM/I2C configuration as BUCK6/VDD_DDR.
12. **Route `USB3DR_DP`/`USB3DR_DM`** to the reused USB-C connector's data pins, downstream of the USBLC6-2SC6 protection array — needed for the single-cable power+DFU-flashing workflow to actually work.
13. **Confirm the 56Ω/240Ω/100Ω termination values against AN5724's point-to-point-specific guidance** before finalizing — the EV1's real values are for its 32-bit fly-by topology, and our 16-bit single-chip point-to-point topology may call for different values even though the protocol is identical.

Resolved: **DDR4 rail voltages and termination network fully resolved with real values from the actual EV1 schematic** — VDD_DDR 1.2V (BUCK6), VTT_DDR 0.6V (LDO3), VREF_DDR 0.6V (dedicated PMIC REFDDR output, not a resistor divider — the earlier "missing second resistor" question is resolved because there was never a divider), VPP_DDR 2.5V (BUCK7); VTT termination 56Ω per signal, CLK differential termination 100Ω, ZQ calibration 240Ω (SoC and both DRAM chips), DDR_RESETN pull 10kΩ; **PCIe lane count and generation confirmed — Gen2, 1 lane**, from ST's own feature-summary documentation, matching Hailo-8L exactly with no headroom for a wider-lane module later; **boot-mode select circuit confirmed from the EV1's actual BOOT MODE schematic** — DSWB04LHGET 4-pole DIP switch (LCSC C99418, $0.21, confirmed in stock) + four 1kΩ pull-ups, with real BOOT[3:0] codes for eMMC (`0010`), SD fallback (`0001`), and forced DFU (`0000`), resolving both the strap-wiring and boot-source-selection open items in one circuit; memory type reversed from LPDDR4 back to **DDR4**; specific chip **confirmed in stock and priced** (Samsung K4A8G165WG-BCWE, LCSC C41368581, 8Gbit x16 FBGA-96, $45.84 — supersedes an earlier 4Gbit pick that could never be confirmed available); host-chip pick (STM32MP257, confirmed PCIe + camera + DDR flexibility after the i.MX8M Nano correction); exact package confirmed (FAL3 = VFBGA361, distinct from TFBGA361, both packages genuinely support 16-bit DDR4 per ST's datasheet); complete DDR4 ball mapping (fixed DDR_A0-7 signals plus swizzle-configurable DDR_A8-31 adopted directly from ST's own DDR4 validation board wiring, via AN5723 Tables 7 and 17 — protocol-defined, unaffected by the 4Gbit→8Gbit density change); storage strategy (eMMC as primary boot + microSD as fallback, confirmed to match the EV1's own DIP-switch-selectable dual-boot design exactly, on separate SDMMC2/SDMMC1 controllers); DFU/STM32CubeProgrammer flashing workflow confirmed via AN5275; PMIC part and variant (STPMIC25A — confirmed match for eMMC/USB boot per AN6198); VDDIO_SDCARD configuration requirement identified (requires custom NVM/I2C setup); board input power (USB-C only, no barrel jack — basic 5V/3A tier confirmed sufficient for the ~6–8W power budget, no PD negotiation stack needed); USB-C connector + ESD/data protection subcircuit resolved with real, reused parts (TYPE-C16PIN connector, USBLC6-2SC6 D+/D− protection, SMBJ5.0A VBUS TVS, 5.1kΩ CC pull-downs) from a prior board's proven BOM, replacing the generic TCPP0x placeholder; PCIe reference clock generator identified (Microchip DSC557-0344FI0-T); camera FFC connector identified (Molex 524372271, 22-pin 0.5mm); JLCPCB stackup selection (JLC06161H-1080).

## Relationship to other capstone projects

- Distinct from the FPGA+DDR3L board (own-controller DDR learning) and the original Rockchip AI Vision Board concept (single-chip NPU+DDR+Linux). This board's specific contribution: DDR4 routing + Linux bring-up + PCIe/M.2 integration, with AI inference risk deliberately factored out.
- Serves as a documented example of catching and correcting a real research error mid-project (i.MX8M Nano's missing PCIe) — worth keeping in the writeup/portfolio narrative, since verifying rather than assuming vendor-family capability carryover is itself a demonstrable engineering habit.
