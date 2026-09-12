# The RS-485 protocol of the ventilation unit

Reconstructed from twelve daily recordings of the raw bytestream (February to
September 2026), cross-checked against `knx_Lueftung_Stellwert{type="actual"}`
and `shelly_power_w{instance="Lüftung"}` in Prometheus, and against the
groundwork collected in `Paul_Novus300_RS485.xlsx`.

The unit reports itself as a `CLIMOS200` running bus version 1.7.1. Its panel
names the nodes as master `SWZ0024B32B`, fan slave `SWZ0025B27A`, TFT 1
`ETA0036E31E` and defroster `ST00064E26E`; a post-heater and an EWT damper are
configurable but not fitted here. Those four screens under Information are what
several readings below are checked against.

## Contents

- [The bytestream and its defect](#the-bytestream-and-its-defect)
- [Frame format](#frame-format)
- [Addresses and commands](#addresses-and-commands)
- [Payloads are register records](#payloads-are-register-records)
- [Register map](#register-map)
- [How the meanings were established](#how-the-meanings-were-established)
- [Preheater](#preheater)
- [Restarts](#restarts)
- [Open points](#open-points)

## The bytestream and its defect

56 % of all bytes in a recording are `0x00`. That is the idle line: it sits in
the space state rather than mark, so the receiver reads a continuous run of null
characters out of it. When a real frame starts, the receiver is still inside one
of those phantom characters and swallows the first real byte — it arrives as
`0x00`. Every other byte of the frame is intact.

Because every address on this bus begins with `0x01`, the byte is recoverable:
when the CRC fails and the first byte is `0x00`, the same window is checked once
more with `0x01` substituted. Without that repair the framer discards 88 % of
the traffic:

| Frame type    | without repair | actually sent |       kept | Carries                        |
| ------------- | -------------: | ------------: | ---------: | ------------------------------ |
| `01 00 85 13` |         16 216 |        41 626 |     39.0 % | the four temperatures          |
| `01 00 85 03` |        295 875 |     1 419 124 |     20.8 % | a single register              |
| `01 01 87 83` |          7 482 |        29 472 |     25.4 % | acknowledgements               |
| `01 04 84 00` |            372 |       424 001 |      0.1 % | slave poll                     |
| `01 00 85 20` |              0 |         1 438 |      0.0 % | filter countdown, fan setpoint |
| `01 00 85 21` |              2 |         1 437 |      0.1 % | operating hour counters        |
| **total**     |    **320 939** | **2 691 263** | **11.9 %** | measured on 2026-09-11         |

Frames that follow their predecessor without a gap keep their first byte. That
is the only reason the temperatures came through at all.

The cause is electrical and would be fixed by fail-safe bias on the A/B pair,
which also removes the flood of nulls. The repair in software works either way.
`climos_frames_total{result="repaired"}` shows how many frames it saves; as that
share approaches zero, the line is healthy.

## Frame format

```
 0    1    2     3     4    5    6 …
+----+----+-----+-----+----+----+-------------+
| Address | Cmd | Len | CRC     | Payload     |
+----+----+-----+-----+----+----+-------------+
```

- **Address** two bytes big endian, always `0x01xx`.
- **Len** counts the payload only. Bit 7 is a flag, not length: `0x83` means
  three payload bytes. Acknowledgements always set it.
- **CRC** 16 bit CCITT over the header without the CRC bytes plus the payload,
  stored little endian. Implemented in [`crc.go`](../pkg/climos/crc.go).

## Addresses and commands

| Address             | Meaning                                         | Seen                           |
| ------------------- | ----------------------------------------------- | ------------------------------ |
| `0x0100`            | register space, target of every data frame      | continuously                   |
| `0x0101`            | master                                          | identity, all acknowledgements |
| `0x0104`            | slave                                           | ~424 000 polls/day             |
| `0x0108`, `0x0109`  | further participants                            | ~320 000 / 350 000 per day     |
| `0x0102` … `0x010c` | full address range, scanned twice each at start | only at a restart              |
| `0x01ff`            | broadcast poll                                  | ~9 600 per day                 |

Only `0x0104`, `0x0108` and `0x0109` answer the startup scan, and each reports
an article number while doing so. The panel names those numbers, which pins each
address to a device:

| Address  | Article       | Panel calls it            |
| -------- | ------------- | ------------------------- |
| `0x0104` | `ETA0036E31E` | TFT 1, the control panel  |
| `0x0108` | `SWZ0025B27A` | fan slave                 |
| `0x0109` | `ST00064E26E` | defroster                 |

The bus names two of them in full — `0x88` reads `Fan controller` and `0x8b`
reads `Touch TFT 1`, which is the panel's own wording for `ETA0036E31E` — while
`0x09` stays empty for the third. The master itself is `SWZ0024B32B` and never
appears on the bus: it announces the others but not itself. The remaining addresses are asked twice and stay
silent, which matches the panel's list of attached devices — master, control
unit, fan slave and defroster are ticked, the heater and the EWT damper are not.
The address map in the `Adressen` sheet does not apply to this firmware: it
assigns `0x0105` and `0x0106` to the defroster and the electric heater, and
nothing answers at either address here.

Data frames cannot be traced back to individual devices. They all go to
`0x0100`, and which register they carry does not depend on who was polled last:
after a poll to `0x0104`, 38 % of the following frames carry `0x0e` and 31 %
carry `0x1d`; after `0x0109`, 65 % carry `0x1d`. The master collects and reports
in bulk.

| Command         | Meaning                                                                            |
| --------------- | ---------------------------------------------------------------------------------- |
| `0x80` / `0x81` | enumeration request and answer, payload `00 00 01 07 01` plus article number         |
| `0x84` / `0x86` | keep-alive poll and data request, never carry a payload                             |
| `0x85`          | register data to `0x0100` — everything worth exporting                              |
| `0x87`          | acknowledgement to the master, payload `00` plus the CRC of the frame acknowledged   |

## Payloads are register records

A payload is not a struct at fixed offsets but a run of records
`<register id uint16 little endian><value>`. The width of a value is not on the
wire; it is fixed per register.

```
44 00 88   81 00 18 01   82 00 fa 00   83 00 18 01   84 00 ff 00
└─0x44─┘   └───0x81───┘  └───0x82───┘  └───0x83───┘  └───0x84───┘
   136        28.0 °C       25.0 °C       28.0 °C       25.5 °C
```

The width table comes from the complete register dump the master emits after
every restart: the ids ascend there, so the packet length pins each width down
unambiguously. A decoder has to stop at the first unknown register — without its
width, everything behind it shifts.

Value types:

- **1 and 2 bytes** integer, little endian, and the only widths that hold a
  number. The width carries no sign of its own and the bus mixes both readings,
  so `climos_register` publishes the unsigned wire value and only the registers
  whose sign is established — the four temperatures, which go below zero every
  winter — are also published interpreted under their own metric.
- **3 and 4 bytes** version and firmware stamps, printed as `1.7.1` and left out
  of `climos_register`, where they would read as large nonsense integers.
- **5 bytes** counter: minute, hour, days uint16, years. A year is exactly 365
  days, checked against the rollover between the 2026-06-20 and 2026-07-20
  recordings.
- **11 and 16 bytes** ASCII. The 16 byte ones start with a device type byte
  followed by 15 characters of name.
- **8 bytes** bit field, see the weekly schedule.

## Register map

### Live values

| Reg    | Width | Meaning                          |   Rate | Observed         | Confidence |
| ------ | ----: | -------------------------------- | -----: | ---------------- | ---------- |
| `0x81` |     2 | supply air, 0.1 °C               | 3.5 s  | 20.4 – 31.0 °C   | measured   |
| `0x82` |     2 | outdoor air, 0.1 °C              | 3.5 s  | −1.5 – 29.4 °C   | measured   |
| `0x83` |     2 | extract air, 0.1 °C              | 3.5 s  | 24.5 – 30.5 °C   | measured   |
| `0x84` |     2 | exhaust air, 0.1 °C              | 3.5 s  | 5.5 – 30.7 °C    | measured   |
| `0x55` |     2 | fan setpoint, 0.1 %              | 2.1 s  | 24.6 – 70.0 %    | measured   |
| `0x1d` |     1 | run state                        | 0.1 s  | 0, 1, 3, 4       | inferred   |
| `0x1a` |     1 | status word                      | 0.3 s  | 0, 1, 13         | inferred   |
| `0x08` |     1 | start and shutdown marker        | event  | 0, 1, 3          | inferred   |
| `0x0e` |     1 | error code, never ≠ 0 in 12 days | 0.25 s | 0                | inferred   |
| `0x28` |     1 | operating mode, only on change   | event  | 4, 6             | measured   |
| `0x44` |     1 | unidentified, 136 – 141          | 3.5 s  | see below        | open       |
| `0x25` |     1 | constant 0x36                    | 60 s   | 54               | open       |
| `0x09` |    16 | name slot of the third node, which reports none | 60 s | `00` + 15 spaces | measured |

`0x28` carries the mode codes from the `Lüfterstufen` sheet: 1 – 3 fan stage,
4 boost, 5 away, 6 automatic. `0x27` and `0x29` run alongside it and read 6 in
every recording.

`0x1d` reads 1 in normal operation, drops to 0 during an orderly shutdown and
reports 3 for the first seconds after a start. `0x08` reports 0 immediately
before stopping, then 1 and 3 while coming up.

### Counters, five bytes

| Reg    | Meaning                                        |      2026-06-20 |     2026-09-11 | Advance                   |
| ------ | ---------------------------------------------- | --------------: | -------------: | ------------------------- |
| `0x26` | operating time, total                | 3 y 342 d 21:04 | 4 y 58 d 06:06 | +1 min per running minute |
| `0x85` | operating time, fan                  | 3 y 322 d 07:27 | 4 y 37 d 16:29 | as above                  |
| `0x3e` | time left until the filter change    |     145 d 17:34 |     63 d 08:34 | −1 day per day            |
| `0x3d` | configured filter interval           |     160 d 00:00 |    160 d 00:00 | —                         |
| `0x3f` | counter, only in the restart dump    |      11 d 00:00 |     11 d 00:00 | —                         |

The panel's Betriebsstunden screen shows both counters side by side and settles
what they are: on 2026-09-12 at 12:15 it read 4 y 59 d 17:17 under *insgesamt*
and 4 y 39 d 03:40 under *Lüfter*, which is 131 303 820 s and 129 526 800 s. The
exporter reported 131 303 880 and 129 526 860 one tick later. The 20 d 13:37 gap
between the two counters is simply the time the unit has been powered without
the fan running.

The Filterlaufzeit screen does the same for the filter: 160 days configured,
62 days left. That is `0x3d`, not `0x3a` = 100, which was the earlier reading
here and is wrong — `0x3a` is unidentified again. Both operating counters run
only while the unit has power, so their distance from wall clock time is
accumulated downtime.

### Identity, only during the startup scan

| Reg            | Width | Value                   | Meaning                                    |
| -------------- | ----: | ----------------------- | ------------------------------------------ |
| `0x00`         |     3 | `01 07 01`              | bus version, shown as 1.7.1 on the panel   |
| `0x0d`         |    11 | `ETA0036E31E`           | article number of TFT 1                    |
| `0x19`         |    11 | `SWZ0025B27A`           | article number of the fan slave            |
| `0x1c`         |    11 | `ST00064E26E`           | article number of the defroster            |
| `0x88`         |    16 | `c2` + `Fan controller` | type byte and name of the fan slave        |
| `0x8b`         |    16 | `c7` + `Touch TFT 1`    | type byte and name of TFT 1                |
| `0x89`, `0x8c` |     4 | `06 08 0a 17`           | identical on both, likely a firmware stamp |
| `0x8a`, `0x8d` |     3 | `09 2f 0c`, `24 2e 0c`  | differs per device                         |

### Settings, only in the restart dump

Unchanged across all twelve recordings.

| Reg             | Value                       | Meaning                                                    |
| --------------- | --------------------------- | ---------------------------------------------------------- |
| `0x2d` – `0x30` | 17, 17, 47, 85              | fan stages in percent                                      |
| `0x31` – `0x33` | −5, −5, −5                  | debalancing per stage                                      |
| `0x34` – `0x39` | 17, 25, 38, 47, 56, 64      | the step ladder the sheet attributes to an LED panel       |
| `0x3a`          | 100                         | unidentified                                               |
| `0x4d` – `0x50` | 17, 17, 100, 100            | 0-10 V: Umin 1.7 V, n_min 17 %, Umax 10.0 V, n_max 100 %   |
| `0x51` – `0x54` | 50, 20, 190, 80             | 4-20 mA: Imin, n_min, Imax, n_max                          |
| `0x4a` – `0x4c` | −2.0, −3.0, −3.0 °C         | frost protection thresholds                                |
| `0x56`, `0x60`  | 22.0 °C                     | upper bypass temperature                                   |
| `0x64` – `0x78` | 21 registers of 8 bytes     | weekly schedule                                            |

`0x4d` – `0x50` give the characteristic of the analog input:
`n% = 17 + (U − 1.7) · (100 − 17) / (10 − 1.7)`, and because `83 / 8.3 = 10`
that reduces to `n% = 10 · U`. The KNX setpoint in percent therefore lands in
the unit one to one as the fan setpoint — which is exactly what `0x55` measures.

### Weekly schedule

21 registers of 8 bytes are exactly 7 days × 24 h at 2 bits per quarter hour, as
the sheet guessed. Three registers cover one day: 00:00–08:00, 08:00–16:00,
16:00–24:00.

```
Mon-Fri   aa aa aa aa aa aa aa aa · 5f 55 55 55 55 55 55 55 · aa aa aa aa aa aa aa aa
Sat-Sun   aa aa aa aa aa aa aa aa · aa aa aa aa aa aa aa aa · aa aa aa aa aa aa aa aa
```

`0xaa` is `10` repeated, so stage 2; `0x55` is `01`, so stage 1. The stored
programme drops to stage 1 during the working day from Monday to Friday and
holds stage 2 at the weekend, with one hour of stage 3 around 08:00 (the `0x5f`).

It is not in effect: the unit runs off the 0-10 V input, and `0x55` follows the
KNX setpoint even where the programme would ask for something else.

## How the meanings were established

**The four temperatures** through an energy balance, independent of any label.
On 2026-03-05: outdoor 1.5 °C, extract 25.5 °C, supply 21.0 °C, exhaust 5.5 °C.
Recovery on the supply side (21.0 − 1.5)/(25.5 − 1.5) = 81 %, on the exhaust
side (25.5 − 5.5)/(25.5 − 1.5) = 83 %. On 2026-08-06 the same calculation gives
85 % and 84 %. Only this assignment yields a plausible, balanced counterflow.

**The fan setpoint `0x55`** through the KNX setpoint. On 2026-08-14 the CO₂
control drove the analog actuator from 25 % to 70 % and back:

| Time  | `knx_Lueftung_Stellwert` | `shelly_power_w` |  `0x55` | `0x44` |
| ----- | -----------------------: | ---------------: | ------: | -----: |
| 14:14 |                   25.1 % |           15.6 W |  24.6 % |    138 |
| 14:29 |                   40.4 % |           30.4 W |  43.7 % |    138 |
| 14:39 |                   60.0 % |           69.1 W |  61.3 % |    138 |
| 14:49 |                   70.2 % |           98.3 W |  70.0 % |    138 |
| 15:19 |                   39.2 % |           29.2 W |  35.3 % |    137 |
| 15:39 |                   29.4 % |           15.6 W |  29.1 % |    137 |

On 2026-07-09 the same happens with an excursion to 45.5 %, where `0x55` reads
458. `0x44` does not move in either case, which rules it out as air volume or
fan speed.

**The operating mode `0x28`** on 2026-07-20: at 08:25:11 it reports 4, at
08:54:19 it reports 6 again — somebody pressed boost on the panel and the unit
fell back to automatic 29 minutes later. The fan setpoint `0x55` stayed at
24.7 % throughout those 29 minutes, and so did the power draw. The mode selected
on the panel therefore has no effect in this installation: the 0-10 V input
overrides it.

For "when was boost active" that means: `climos_operating_mode` shows what was
selected on the panel, `climos_fan_setpoint_percent` shows what the unit
actually did. A boost driven over KNX is a short upward excursion of `0x55`, an
away setting a sustained low value; the mode stays at 6 throughout.

**The acknowledgements `0x87`** because their three payload bytes are `00` plus
the CRC bytes of the data frame sent immediately before.

## Preheater

The defroster is on the bus at `0x0109` and polled around 350 000 times a day;
it simply never had reason to run during any of the recordings. In the power
draw it is unmistakable: from 4 to 8 and from 20 to 24 January 2026,
`shelly_power_w{instance="Lüftung"}` rises from its 17 W baseline to hourly
averages around 250 W, peaking at 271 W on 5 January. The fan alone reaches only
about 98 W, and that at a 70 % setpoint.

The trigger is the outdoor temperature. On those January days the daily minimum
sat between −2 °C and −4 °C, and the frost protection thresholds in the
configuration are `0x4a` = −2.0 °C with `0x4b` and `0x4c` = −3.0 °C.

The recordings begin on 2026-02-21, and from then on the daily minimum never
fell below −1 °C; the highest power draw between February and April was 41 W and
is fully explained by the fan. So while the device is there and answering polls,
nothing in the recordings shows it heating, and the register map holds no
register for that state.

The panel's message log names the shape such a register takes: on 2024-08-12 at
20:35 it recorded *Fehler am Eingang — TempFromBusDEFR*, a temperature the
defroster reports over the bus. When it next comes on it will most likely report
through a register that is not in the table yet. The decoder stops at the first unknown register and loses the
rest of that frame, but it counts the event:

```promql
sum by (register) (increase(climos_unknown_registers_total[1h])) > 0
```

The missing width can then be derived from the next restart dump, because the
ids ascend there and the packet length fixes every width.

## Restarts

A restart is recognisable by the address scan across `0x0102` … `0x010c`,
followed by the complete register dump. Two kinds occur:

- **Orderly shutdown.** `0x08` goes to 0, then `0x1d` reports 0 four times over
  six seconds, then silence. That is what happened on 2026-08-05: the unit ran
  for 7 h 40 from the start of the recording, stood still from 09:39 to 19:53,
  and ran on afterwards. 13 h 46 of runtime plus 10 h 14 of standstill make
  exactly 24 h.
- **Brownout.** On 2026-08-06 around 00:15 traffic was still normal 1.6 s
  beforehand, there is no shutdown sequence, `0x55` reads 80 in the restart
  dump, and two complete address scans follow seven seconds apart.

Each recording runs from 01:59 to 01:59 and they join without a gap. Since the
stream carries no timestamp, a recording is dated through `0x3e`: that counter
ticks down once per running minute.

## Open points

- **`0x44`.** Sits between 136 and 141, about two units higher in winter than in
  summer, is exactly constant within a day and does not react to the fan
  setpoint, which rules out anything air related. After a start it climbs from
  10 and decelerates as it goes: 127 after four minutes, 136 after twenty-two,
  137 only after five hours. That is a heavily filtered measurement approaching
  a value, not an actuator being driven. It is exported raw as
  `climos_register{register="0x44"}`.

  The climb also settles how the byte is read. It passes 127 and 128 without a
  break, so the register is unsigned; sign extension would turn that step into a
  jump from 127 to −128.
- **`0x3a`** = 100. It was read as the filter interval until the panel showed
  that interval to be 160 days, which is `0x3d`. What `0x3a` counts is open.
- **Bit order in the weekly schedule**, which decides whether the stage 3 hour
  runs from 08:00 to 08:30 or from 08:30 to 09:00.
- **`0x64` – `0x78` as the schedule** rests on the byte count and on the split
  between weekdays and weekend. Changing one slot on the panel and taking a
  fresh recording would confirm it in a minute.
- **Which register carries the defroster's state and its bus temperature.** The
  node answers every poll, but nothing it sends changes while it is idle. That
  needs a recording from a day below −2 °C; the first such day after 2026-02-21
  is still to come.
- **The read buffer in `reader.go`** grows without bound as long as no frame can
  be extracted. It does not happen in the recordings, because a unit without
  power delivers no bytes at all.
