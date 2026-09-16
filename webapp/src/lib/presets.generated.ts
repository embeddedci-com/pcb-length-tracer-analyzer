/**
 * The vendors' published DDR rules, for the list of supported chips.
 *
 * Generated from ddr/presets.go by cmd/gen-presets; run `make presets` after
 * changing a preset. server/presets_ts_test.go fails while this is stale.
 *
 * The rules form does not use this: it reads /defaults, so the numbers it
 * applies are the ones the engine will judge the board by. This copy exists so
 * the catalogue is in the statically rendered page.
 */

import type { Preset } from './analyzerApi'

export const PRESET_CATALOG: Preset[] = [
  {
    "id": "st-stm32mp25-ddr4",
    "name": "STM32MP25x, DDR4",
    "vendor": "STMicroelectronics",
    "parts": [
      "STM32MP25"
    ],
    "memory": "DDR4",
    "source": "ST DDR4 length equalization sheet for STM32MP25xxAI, with AN5724",
    "url": "https://www.st.com/resource/en/application_note/an5724-stm32mp25xx-hardware-design-guidelines-stmicroelectronics.pdf",
    "note": "Package lengths per ball come with this part, so lengths include the wiring inside the package. AN5724 sets no pair limit: it forbids equalizing inside a pair and takes the mean of N and P as the pair's length, so the tool's default stands there.",
    "params": {
      "data_to_strobe_mm": 1.4223999999999999,
      "data_to_strobe_ps": 0,
      "intra_pair_mm": 0.127,
      "intra_pair_ps": 0,
      "address_to_clock_mm": 3.556,
      "address_to_clock_ps": 0,
      "strobe_to_clock_mm": 12.065,
      "strobe_to_clock_ps": 0,
      "max_chip_delta_mm": 35.0012,
      "clock_offset_percent": 0
    },
    "unstated": [
      "intra_pair_mm"
    ]
  },
  {
    "id": "stm32mp1-ddr3l",
    "name": "STM32MP1 Series, DDR3L and LPDDR3",
    "vendor": "STMicroelectronics",
    "parts": [
      "STM32MP151",
      "STM32MP153",
      "STM32MP157"
    ],
    "memory": "DDR3/DDR3L/LPDDR2/LPDDR3",
    "source": "ST AN5122 Rev 3 (2019-02), STM32MP1 Series DDR memory routing guidelines, sections 6.1 to 6.3",
    "url": "https://www.st.com/resource/en/application_note/an5122-stm32mp1-series-ddr-memory-routing-guidelines-stmicroelectronics.pdf",
    "note": "The note's windows are one sided: address and command must be 0 to 40 mil shorter than the clock, and the strobe 0 to 590 mil shorter, with the clock the longest trace of all. This tool centers both on the clock, so it checks the size of the difference but not its direction. Like AN5724 it sets no pair limit, taking the mean of N and P instead, so the tool's default stands there. The chip to chip figure is for the two-device fly-by case.",
    "params": {
      "data_to_strobe_mm": 1.016,
      "data_to_strobe_ps": 0,
      "intra_pair_mm": 0.127,
      "intra_pair_ps": 0,
      "address_to_clock_mm": 1.016,
      "address_to_clock_ps": 0,
      "strobe_to_clock_mm": 14.985999999999999,
      "strobe_to_clock_ps": 0,
      "max_chip_delta_mm": 33.02,
      "clock_offset_percent": 0
    },
    "unstated": [
      "intra_pair_mm"
    ]
  },
  {
    "id": "rk3588-lpddr5-hdi",
    "name": "RK3588 and RK3588S, LPDDR5 (10-layer HDI)",
    "vendor": "Rockchip",
    "parts": [
      "RK3588",
      "RK3588S"
    ],
    "memory": "LPDDR5",
    "source": "RK3588 Hardware Design Guide V1.0 (2022-01-06), table 3-7",
    "url": "https://github.com/FanX-Tek/RK3588_hardware/blob/master/RK3588/01_Official%20Release/01_Common%20Document/RK3588%20Hardware%20Design%20Guide-V1.0.pdf",
    "note": "For the 10-layer HDI stackup, where the guide matches by length. WCLK is held to the same limit as DQS. Rockchip strongly recommends its own DDR template.",
    "params": {
      "data_to_strobe_mm": 0.635,
      "data_to_strobe_ps": 0,
      "intra_pair_mm": 0.127,
      "intra_pair_ps": 0,
      "address_to_clock_mm": 1.016,
      "address_to_clock_ps": 0,
      "strobe_to_clock_mm": 5.08,
      "strobe_to_clock_ps": 0,
      "max_chip_delta_mm": 35.0012,
      "clock_offset_percent": 0
    },
    "unstated": [
      "max_chip_delta_mm"
    ]
  },
  {
    "id": "rk3588-lpddr4-hdi",
    "name": "RK3588 and RK3588S, LPDDR4/LPDDR4X (10-layer HDI)",
    "vendor": "Rockchip",
    "parts": [
      "RK3588",
      "RK3588S"
    ],
    "memory": "LPDDR4/LPDDR4X",
    "source": "RK3588 Hardware Design Guide V1.0 (2022-01-06), tables 3-8 and 3-9",
    "url": "https://github.com/FanX-Tek/RK3588_hardware/blob/master/RK3588/01_Official%20Release/01_Common%20Document/RK3588%20Hardware%20Design%20Guide-V1.0.pdf",
    "note": "For the 10-layer HDI stackup, where the guide matches by length.",
    "params": {
      "data_to_strobe_mm": 0.635,
      "data_to_strobe_ps": 0,
      "intra_pair_mm": 0.127,
      "intra_pair_ps": 0,
      "address_to_clock_mm": 1.016,
      "address_to_clock_ps": 0,
      "strobe_to_clock_mm": 6.35,
      "strobe_to_clock_ps": 0,
      "max_chip_delta_mm": 35.0012,
      "clock_offset_percent": 0
    },
    "unstated": [
      "max_chip_delta_mm"
    ]
  },
  {
    "id": "rk3588-lpddr4-8layer",
    "name": "RK3588 and RK3588S, LPDDR4/LPDDR4X (8-layer, equal delay)",
    "vendor": "Rockchip",
    "parts": [
      "RK3588",
      "RK3588S"
    ],
    "memory": "LPDDR4/LPDDR4X",
    "source": "RK3588 Hardware Design Guide V1.0 (2022-01-06), tables 3-10 and 3-11",
    "url": "https://github.com/FanX-Tek/RK3588_hardware/blob/master/RK3588/01_Official%20Release/01_Common%20Document/RK3588%20Hardware%20Design%20Guide-V1.0.pdf",
    "note": "For the 8-layer through-hole stackup. The guide gives these as delays, because surface and inner layers differ in speed, so the limits here are in picoseconds.",
    "params": {
      "data_to_strobe_mm": 0,
      "data_to_strobe_ps": 16,
      "intra_pair_mm": 0,
      "intra_pair_ps": 1,
      "address_to_clock_mm": 0,
      "address_to_clock_ps": 16,
      "strobe_to_clock_mm": 0,
      "strobe_to_clock_ps": 40,
      "max_chip_delta_mm": 35.0012,
      "clock_offset_percent": 0
    },
    "unstated": [
      "max_chip_delta_mm"
    ]
  },
  {
    "id": "rk3568-lpddr4",
    "name": "RK3566 and RK3568, LPDDR4/LPDDR4X",
    "vendor": "Rockchip",
    "parts": [
      "RK3566",
      "RK3568"
    ],
    "memory": "LPDDR4/LPDDR4X",
    "source": "RK3568 High Speed PCB Design Guide V1.0 (2021-04-12), tables 33 and 34",
    "url": "https://github.com/hqnicolas/RK3568-hardware-design/blob/main/01_Common%20Document/Rockchip_RK3568_High_Speed_PCB_Design_Guide_V10_EN_2021-4-12.pdf",
    "note": "The guide says DQ and DM need not match DQS, only stay under 600 mil and be as short as possible, so the data limit here is that bound rather than a matching requirement.",
    "params": {
      "data_to_strobe_mm": 15.24,
      "data_to_strobe_ps": 0,
      "intra_pair_mm": 0.30479999999999996,
      "intra_pair_ps": 0,
      "address_to_clock_mm": 12.7,
      "address_to_clock_ps": 0,
      "strobe_to_clock_mm": 30.48,
      "strobe_to_clock_ps": 0,
      "max_chip_delta_mm": 35.0012,
      "clock_offset_percent": 0
    },
    "unstated": [
      "max_chip_delta_mm"
    ]
  },
  {
    "id": "rk3568-ddr4",
    "name": "RK3566 and RK3568, DDR4",
    "vendor": "Rockchip",
    "parts": [
      "RK3566",
      "RK3568"
    ],
    "memory": "DDR4",
    "source": "RK3568 High Speed PCB Design Guide V1.0 (2021-04-12), tables 27 to 29",
    "url": "https://github.com/hqnicolas/RK3568-hardware-design/blob/main/01_Common%20Document/Rockchip_RK3568_High_Speed_PCB_Design_Guide_V10_EN_2021-4-12.pdf",
    "note": "Address and command are held to the guide's 600 mil for the controller-to-memory run. The guide holds CSn, CKE and ODT to 30 mil instead, which this tool does not separate, and the branches after a memory device to 20 mil of each other.",
    "params": {
      "data_to_strobe_mm": 15.24,
      "data_to_strobe_ps": 0,
      "intra_pair_mm": 0.30479999999999996,
      "intra_pair_ps": 0,
      "address_to_clock_mm": 15.24,
      "address_to_clock_ps": 0,
      "strobe_to_clock_mm": 38.1,
      "strobe_to_clock_ps": 0,
      "max_chip_delta_mm": 35.0012,
      "clock_offset_percent": 0
    },
    "unstated": [
      "max_chip_delta_mm"
    ]
  },
  {
    "id": "rk3399-ddr3",
    "name": "RK3399, DDR3/DDR3L",
    "vendor": "Rockchip",
    "parts": [
      "RK3399"
    ],
    "memory": "DDR3/DDR3L",
    "source": "RK3399 Design Guide V1.0 (2017-04-20), tables 4-5 to 4-8",
    "url": "https://github.com/cnchens/RK3399_Hardware_Design_Reference/blob/master/RK3399_Design_Guide_V1.0_20170420.pdf",
    "note": "The guide gives these as delays. Its 5 ps applies inside a byte lane; between byte lanes the clock-to-strobe limit of 150 ps governs.",
    "params": {
      "data_to_strobe_mm": 0,
      "data_to_strobe_ps": 5,
      "intra_pair_mm": 0,
      "intra_pair_ps": 1,
      "address_to_clock_mm": 0,
      "address_to_clock_ps": 10,
      "strobe_to_clock_mm": 0,
      "strobe_to_clock_ps": 150,
      "max_chip_delta_mm": 35.0012,
      "clock_offset_percent": 0
    },
    "unstated": [
      "max_chip_delta_mm"
    ]
  },
  {
    "id": "rk3399-lpddr3",
    "name": "RK3399, LPDDR3",
    "vendor": "Rockchip",
    "parts": [
      "RK3399"
    ],
    "memory": "LPDDR3",
    "source": "RK3399 Design Guide V1.0 (2017-04-20), tables 4-1 to 4-4",
    "url": "https://github.com/cnchens/RK3399_Hardware_Design_Reference/blob/master/RK3399_Design_Guide_V1.0_20170420.pdf",
    "note": "The guide gives these as delays. Command and control are both held to 5 ps against the clock.",
    "params": {
      "data_to_strobe_mm": 0,
      "data_to_strobe_ps": 5,
      "intra_pair_mm": 0,
      "intra_pair_ps": 1,
      "address_to_clock_mm": 0,
      "address_to_clock_ps": 5,
      "strobe_to_clock_mm": 0,
      "strobe_to_clock_ps": 150,
      "max_chip_delta_mm": 35.0012,
      "clock_offset_percent": 0
    },
    "unstated": [
      "max_chip_delta_mm"
    ]
  },
  {
    "id": "imx93-imx8mp-imx8mn-lpddr4",
    "name": "i.MX 93, i.MX 8M Plus and i.MX 8M Nano, LPDDR4/LPDDR4X",
    "vendor": "NXP",
    "parts": [
      "i.MX 93",
      "i.MX 8M Plus",
      "i.MX 8M Nano"
    ],
    "memory": "LPDDR4/LPDDR4X",
    "source": "NXP i.MX 93 Hardware Design Guide Rev. 1 (2023-04-24), table 16, i.MX 8M Plus Hardware Developer's Guide Rev. 1 (2024-03-26), table 18, and i.MX 8M Nano Hardware Developer's Guide Rev. 1 (2020-11), table 22",
    "url": "https://community.nxp.com/pwmxy87654/attachments/pwmxy87654/imx-processors/225641/1/IMX93HDG.pdf",
    "note": "All three guides hold these limits, at 3733, 4000 and 3200 MT/s. They also ask subgroups of CA to match within 2 ps of each other, and the i.MX 93 holds CS0 and CS1 to 1 ps of the clock, neither of which this tool separates from address and command. The i.MX 93 strobe window is not centered (125 ps short, 75 ps long) and the tighter side is used.",
    "params": {
      "data_to_strobe_mm": 0,
      "data_to_strobe_ps": 50,
      "intra_pair_mm": 0,
      "intra_pair_ps": 1,
      "address_to_clock_mm": 0,
      "address_to_clock_ps": 50,
      "strobe_to_clock_mm": 0,
      "strobe_to_clock_ps": 75,
      "max_chip_delta_mm": 35.0012,
      "clock_offset_percent": 0
    },
    "unstated": [
      "max_chip_delta_mm"
    ]
  },
  {
    "id": "imx8m-lpddr4",
    "name": "i.MX 8M Mini and i.MX 8M Quad, LPDDR4",
    "vendor": "NXP",
    "parts": [
      "i.MX 8M Mini",
      "i.MX 8M Quad",
      "i.MX 8M QuadLite",
      "i.MX 8M Dual"
    ],
    "memory": "LPDDR4",
    "source": "NXP i.MX 8M Mini Hardware Developer's Guide Rev. 1 (2019-08), table 21, and i.MX 8MDQLQ Hardware Developer's Guide Rev. 2 (2019-06), table 16",
    "url": "https://community.nxp.com/pwmxy87654/attachments/pwmxy87654/imx-processors/218788/1/IMX8MMHDG-NEW.pdf",
    "note": "Both guides hold these limits, at 3000 MT/s on the Mini and 3200 on the Quad. They also cap the clock's own flight time, which this tool does not check.",
    "params": {
      "data_to_strobe_mm": 0,
      "data_to_strobe_ps": 10,
      "intra_pair_mm": 0,
      "intra_pair_ps": 1,
      "address_to_clock_mm": 0,
      "address_to_clock_ps": 25,
      "strobe_to_clock_mm": 0,
      "strobe_to_clock_ps": 85,
      "max_chip_delta_mm": 35.0012,
      "clock_offset_percent": 0
    },
    "unstated": [
      "max_chip_delta_mm"
    ]
  },
  {
    "id": "imx8m-ddr3l",
    "name": "i.MX 8M Mini and i.MX 8M Quad, DDR3L-1600",
    "vendor": "NXP",
    "parts": [
      "i.MX 8M Mini",
      "i.MX 8M Quad",
      "i.MX 8M QuadLite",
      "i.MX 8M Dual"
    ],
    "memory": "DDR3L",
    "source": "NXP i.MX 8M Mini Hardware Developer's Guide Rev. 1 (2019-08), table 28, and i.MX 8MDQLQ Hardware Developer's Guide Rev. 2 (2019-06), table 21",
    "url": "https://community.nxp.com/pwmxy87654/attachments/pwmxy87654/imx-processors/218788/1/IMX8MMHDG-NEW.pdf",
    "note": "The guides bound the strobe against the clock at one clock period rather than a window, which is 1250 ps at 1600 MT/s, and a strobe shorter than the clock is allowed.",
    "params": {
      "data_to_strobe_mm": 0,
      "data_to_strobe_ps": 10,
      "intra_pair_mm": 0,
      "intra_pair_ps": 1,
      "address_to_clock_mm": 0,
      "address_to_clock_ps": 25,
      "strobe_to_clock_mm": 0,
      "strobe_to_clock_ps": 1250,
      "max_chip_delta_mm": 35.0012,
      "clock_offset_percent": 0
    },
    "unstated": [
      "max_chip_delta_mm"
    ]
  },
  {
    "id": "imx8mq-ddr4",
    "name": "i.MX 8M Quad, DDR4-2400",
    "vendor": "NXP",
    "parts": [
      "i.MX 8M Quad",
      "i.MX 8M QuadLite",
      "i.MX 8M Dual"
    ],
    "memory": "DDR4",
    "source": "NXP i.MX 8MDQLQ Hardware Developer's Guide Rev. 2 (2019-06), table 21",
    "url": "https://www.mouser.com/pdfdocs/NXP_MCIMX8M-EVK_HD.pdf",
    "note": "The strobe is bounded against the clock at one clock period, 833 ps at 2400 MT/s. Not for the i.MX 8M Mini with DDR4: its own guide asks address and command to run 75 to 125 ps shorter than the clock, an offset this tool cannot center.",
    "params": {
      "data_to_strobe_mm": 0,
      "data_to_strobe_ps": 10,
      "intra_pair_mm": 0,
      "intra_pair_ps": 1,
      "address_to_clock_mm": 0,
      "address_to_clock_ps": 25,
      "strobe_to_clock_mm": 0,
      "strobe_to_clock_ps": 833,
      "max_chip_delta_mm": 35.0012,
      "clock_offset_percent": 0
    },
    "unstated": [
      "max_chip_delta_mm"
    ]
  },
  {
    "id": "imx8mn-ddr4",
    "name": "i.MX 8M Nano, DDR4-2400",
    "vendor": "NXP",
    "parts": [
      "i.MX 8M Nano"
    ],
    "memory": "DDR4",
    "source": "NXP i.MX 8M Nano Hardware Developer's Guide Rev. 1 (2020-11), table 25",
    "url": "https://www.readkong.com/page/i-mx-8m-nano-hardware-developer-s-guide-nxp-7536664",
    "note": "The Nano holds address and command to 10 ps of the clock, tighter than the 8M Quad's 25 ps. The strobe is bounded against the clock at one clock period, 833 ps at 2400 MT/s.",
    "params": {
      "data_to_strobe_mm": 0,
      "data_to_strobe_ps": 10,
      "intra_pair_mm": 0,
      "intra_pair_ps": 1,
      "address_to_clock_mm": 0,
      "address_to_clock_ps": 10,
      "strobe_to_clock_mm": 0,
      "strobe_to_clock_ps": 833,
      "max_chip_delta_mm": 35.0012,
      "clock_offset_percent": 0
    },
    "unstated": [
      "max_chip_delta_mm"
    ]
  },
  {
    "id": "am62x-lpddr4",
    "name": "AM62x and AM62Lx, LPDDR4",
    "vendor": "Texas Instruments",
    "parts": [
      "AM62x",
      "AM62Lx",
      "AM625",
      "AM623"
    ],
    "memory": "LPDDR4",
    "source": "TI AM62x, AM62Lx DDR Board Design and Layout Guidelines SPRAD06C (2025-03), tables 3-6 and 3-7",
    "url": "https://www.ti.com/lit/pdf/sprad06",
    "note": "For LPDDR4-1600, the one rate the guide covers. Per-bit deskew in the PHY is why these are loose. TI matches within a byte lane only, and asks the clock to be the longer of the two, by up to three clock periods. The pair limit is the clock pair's 0.75 ps, tighter than the strobe pair's 1.5 ps. Data may run 150 ps longer than its strobe but only 49 ps shorter, and the tighter side is used.",
    "params": {
      "data_to_strobe_mm": 0,
      "data_to_strobe_ps": 49,
      "intra_pair_mm": 0,
      "intra_pair_ps": 0.75,
      "address_to_clock_mm": 0,
      "address_to_clock_ps": 312.5,
      "strobe_to_clock_mm": 0,
      "strobe_to_clock_ps": 3750,
      "max_chip_delta_mm": 35.0012,
      "clock_offset_percent": 0
    },
    "unstated": [
      "max_chip_delta_mm"
    ]
  },
  {
    "id": "am64x-lpddr4",
    "name": "AM64x and AM243x, LPDDR4",
    "vendor": "Texas Instruments",
    "parts": [
      "AM64x",
      "AM243x",
      "AM6442",
      "AM2434"
    ],
    "memory": "LPDDR4",
    "source": "TI AM64x\\AM243x DDR Board Design and Layout Guidelines SPRACU1A (2021-06), tables 3-6 and 3-7",
    "url": "https://www.ti.com/lit/an/spracu1a/spracu1a.pdf",
    "note": "For LPDDR4-1600, the one rate the guide covers. Far tighter than the AM62x, whose PHY deskews per bit. The pair limit is 0.4 ps for both the clock and the strobe. The guide sets no strobe against clock limit at all, matching only within a byte lane, so the tool's default stands there.",
    "params": {
      "data_to_strobe_mm": 0,
      "data_to_strobe_ps": 2,
      "intra_pair_mm": 0,
      "intra_pair_ps": 0.4,
      "address_to_clock_mm": 0,
      "address_to_clock_ps": 3,
      "strobe_to_clock_mm": 12.065,
      "strobe_to_clock_ps": 0,
      "max_chip_delta_mm": 35.0012,
      "clock_offset_percent": 0
    },
    "unstated": [
      "strobe_to_clock_mm",
      "max_chip_delta_mm"
    ]
  },
  {
    "id": "sama5d3-ddr2",
    "name": "SAMA5D3, DDR2/LPDDR2",
    "vendor": "Microchip",
    "parts": [
      "SAMA5D3",
      "ATSAMA5D3"
    ],
    "memory": "DDR2/LPDDR2",
    "source": "Microchip SAMA5D3 Layout Recommendations, Atmel-11284B (2016-04-11), section 3.3",
    "url": "https://ww1.microchip.com/downloads/en/AppNotes/Atmel-11284-32-bit-Cortex-A5-Microcontroller-SAMA5D3-Layout-Recommendations_Application-Note.pdf",
    "note": "The note gives these as lengths in mils, as prose rather than a table. The 20 mil pair figure is for both the strobe and the clock.",
    "params": {
      "data_to_strobe_mm": 2.54,
      "data_to_strobe_ps": 0,
      "intra_pair_mm": 0.508,
      "intra_pair_ps": 0,
      "address_to_clock_mm": 5.08,
      "address_to_clock_ps": 0,
      "strobe_to_clock_mm": 10.16,
      "strobe_to_clock_ps": 0,
      "max_chip_delta_mm": 35.0012,
      "clock_offset_percent": 0
    },
    "unstated": [
      "max_chip_delta_mm"
    ]
  },
  {
    "id": "sama5d2-ddr3l",
    "name": "SAMA5D2, DDR3L/LPDDR3",
    "vendor": "Microchip",
    "parts": [
      "SAMA5D2",
      "ATSAMA5D2"
    ],
    "memory": "DDR3L/LPDDR3",
    "source": "Microchip SAMA5D2 Layout Recommendations, AN2814 Rev. A (2018-10), section 2.3",
    "url": "https://ww1.microchip.com/downloads/en/Appnotes/SAMA5D2-Layout-Recommendations-Application%20Note-DS00002814A.pdf",
    "note": "Revision A deleted the strobe against clock bound the earlier revision carried, so the tool's default stands there. It also loosened data to strobe from 50 mil to 100 mil.",
    "params": {
      "data_to_strobe_mm": 2.54,
      "data_to_strobe_ps": 0,
      "intra_pair_mm": 0.508,
      "intra_pair_ps": 0,
      "address_to_clock_mm": 5.08,
      "address_to_clock_ps": 0,
      "strobe_to_clock_mm": 12.065,
      "strobe_to_clock_ps": 0,
      "max_chip_delta_mm": 35.0012,
      "clock_offset_percent": 0
    },
    "unstated": [
      "strobe_to_clock_mm",
      "max_chip_delta_mm"
    ]
  }
]
