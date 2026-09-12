# The RS-485 protocol of the ventilation unit

Reconstructed from twelve daily recordings of the raw bytestream (February to
September 2026), cross-checked against `knx_Lueftung_Stellwert{type="actual"}`
and `shelly_power_w{instance="Lüftung"}` in Prometheus, against the groundwork
collected in `Paul_Novus300_RS485.xlsx`, and against the operating manual, which
is the only manufacturer document that describes this device rather than a
relative of it.

The unit is a PAUL CLIMOS F 200 Comfort in the R (rechts, Typ A) form: a
cross-counterflow enthalpy exchanger, which transfers moisture along with heat,
two volume-flow-constant fans and an integrated PTC defroster. It reports itself
as `CLIMOS200` running bus version 1.7.1. Its panel names the nodes as master
`SWZ0024B32B`, fan slave `SWZ0025B27A`, TFT 1 `ETA0036E31E` and defroster
`ST00064E26E`; a post-heater and an EWT damper are configurable but not fitted
here. Those four screens under Information are what several readings below are
checked against.

Because the exchanger moves moisture too, a recovery efficiency computed from
temperatures alone — as it is below — is the sensible part of the recovery only.

## Contents

- [The bytestream and its defect](#the-bytestream-and-its-defect)
- [Eleven bits per character](#eleven-bits-per-character)
- [Frame format](#frame-format)
- [Addresses and commands](#addresses-and-commands)
- [Payloads are register records](#payloads-are-register-records)
- [Register map](#register-map)
- [How the meanings were established](#how-the-meanings-were-established)
- [The predecessor generation](#the-predecessor-generation)
- [The ComfoAir protocol is a different bus](#the-comfoair-protocol-is-a-different-bus)
- [Preheater](#preheater)
- [Summer ventilation, and why there is no bypass register](#summer-ventilation-and-why-there-is-no-bypass-register)
- [Writing to the bus](#writing-to-the-bus)
- [Restarts](#restarts)
- [What the manufacturer documents](#what-the-manufacturer-documents)
- [Open points](#open-points)
- [Sources](#sources)

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
| `01 01 87 83` |          7 482 |        29 472 |     25.4 % | device answers to the master   |
| `01 04 84 00` |            372 |       424 001 |      0.1 % | slave poll                     |
| `01 00 85 20` |              0 |         1 438 |      0.0 % | filter countdown, fan setpoint |
| `01 00 85 21` |              2 |         1 437 |      0.1 % | operating hour counters        |
| **total**     |    **320 939** | **2 691 263** | **11.9 %** | measured on 2026-09-11         |

Frames that follow their predecessor without a gap keep their first byte. That
is the only reason the temperatures came through at all.

The cause is electrical and would be fixed by fail-safe bias on the A/B pair,
which also removes the flood of nulls. The sniffers built for the predecessor
generation used 120 Ω across A and B and 2.2 kΩ to each rail, which is the
arrangement that holds an undriven line at mark. The repair in software works
either way. `climos_frames_total{result="repaired"}` shows how many frames it
saves; as that share approaches zero, the line is healthy.

## Eleven bits per character

The exporter reads the line as eight data bits with space parity and one stop
bit, in [`reader.go`](../pkg/climos/reader.go). That setting is load-bearing
rather than a detail: it makes a character eleven bits long, and the only reason
clean frames come out at all is that the line sends eleven bit characters. 8N1
would be ten bits and would desynchronise on the first byte.

Where the eleventh bit comes from is documented for the predecessor generation.
On a Novus 300 it was traced with a logic analyser as 9600 baud, nine data bits
and one stop bit — the multi-processor communication mode of the Atmel
controllers inside — and the ninth bit marks a character as an address rather
than as data. A PC UART reads such a line by putting its parity bit where the
ninth data bit sits, which is what space parity does; the people who did it used
8S1 and 8M1 to select the two halves of the traffic.

Whether this generation still sets that marker is open, and a single recording
decides it, because mark parity passes exactly the characters space parity
rejects. The answer is worth having either way: a receiver that sees the marker
does not need the repair heuristic, since the marker *is* the frame boundary.

One thing the recordings already settle: the first byte is not being lost to a
parity error. A marked character would fail space parity, and the driver hands a
character that fails parity over as `0x00` — so a marked first byte would be
`0x00` every single time. It is not. Counted over 2026-09-11 it arrives as
`0x01` in 39 % of the temperature frames, 29 % of the single register frames and
34 % of the device answers, and those are exactly the frames that follow their
predecessor without an idle gap; the slave poll, which always opens a cycle
after idle time, keeps it 372 times out of 424 028. So either the marking is
gone on this generation or the adapter drops the ninth bit without reporting it,
and the first byte is lost to the misalignment above rather than to the marker.
Which of the two it is, the mark parity recording answers.

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
| `0x0101`            | master                                          | identity, every device answer  |
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

The address space itself is inherited. The predecessor uses the same
`0x0100` … `0x01ff` range, with `0x0101` the master, `0x0102` the control panel
and `0x0104` the fan slave, and its startup scan also asks every address twice.
What moved is which device type sits where — here the panel answers at `0x0104`
and the fan slave at `0x0108`. The six device types the panel lists are the same
six as there: master, control unit, fan slave, defroster, heating register, EWT
damper. Addresses belong to a type rather than being handed out freely: on the
predecessor a device that registered as a panel at `0x0112` made the master
expect a fan slave at `0x0114` and a defroster at `0x0115`, and report a
defroster communication fault when neither answered.

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
| `0x87`          | device answer to the master, payload `00` plus a two byte value, see below           |

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
| `0x81` |     2 | supply air, T2, 0.1 °C           | 3.5 s  | 20.4 – 31.0 °C   | measured   |
| `0x82` |     2 | outdoor air, T1, 0.1 °C          | 3.5 s  | −1.5 – 29.4 °C   | measured   |
| `0x83` |     2 | extract air, T3, 0.1 °C          | 3.5 s  | 24.5 – 30.5 °C   | measured   |
| `0x84` |     2 | exhaust air, T4, 0.1 °C          | 3.5 s  | 5.5 – 30.7 °C    | measured   |
| `0x55` |     2 | fan setpoint, 0.1 %              | 2.1 s  | 24.6 – 70.0 %    | measured   |
| `0x1d` |     1 | run state                        | 0.1 s  | 0, 1, 3, 4       | inferred   |
| `0x1a` |     1 | status word                      | 0.3 s  | 0, 1, 13         | inferred   |
| `0x08` |     1 | event register, see below        | event  | 0, 1, 3          | open       |
| `0x0e` |     1 | error code, never ≠ 0 in 12 days | 0.25 s | 0                | inferred   |
| `0x62` |     1 | fires when a setting is changed  | event  | 0, 11, 12        | open       |
| `0x27` |     1 | operating mode running, on change | event | 4, 5, 6          | measured   |
| `0x28` |     1 | operating mode running, on change | event | 4, 5, 6          | measured   |
| `0x29` |     1 | operating mode to fall back to    | event | 5, 6             | measured   |
| `0x44` |     1 | unidentified, 136 – 141          | 3.5 s  | see below        | open       |
| `0x25` |     1 | constant 0x36                    | 60 s   | 54               | open       |
| `0x09` |    16 | name slot of the third node, which reports none | 60 s | `00` + 15 spaces | measured |

`0x27`, `0x28` and `0x29` carry the mode codes from the `Lüfterstufen` sheet:
1 – 3 fan stage, 4 boost, 5 away, 6 automatic. `0x27` and `0x28` move together
and hold the mode that is running; `0x29` holds the one to fall back to.

The manual says what the two special modes are, which the recordings could only
describe from the outside. Boost runs at the air volume of fan stage 3 for a
duration set between 15 and 120 minutes in five minute steps, and it interrupts
the current mode rather than replacing it. Away is a humidity protection
programme, not a low speed: it runs fan stage 1 for 15, 30 or 45 minutes of
every hour and stops in between. Both durations are on the bus: `0x3b` holds the
boost duration and `0x3c` the away interval. The 30 minutes `0x3c` held until
2026-09-12 is why away looked like equal spells of running and standing still.

`0x1d` reads 1 in normal operation, drops to 0 during an orderly shutdown and
reports 3 for the first seconds after a start.

`0x1a` read 13 through all twelve recordings and now reads 37, which is bit 3
giving way to bit 5 at the moment the summer ventilation started. During the
switch it passed through 45, which is both bits at once. Bit 3 and bit 5 read as
the two ways the unit can move air: through the exchanger, or past it with the
exhaust fan stopped.

`0x62` had never appeared in twelve days of recordings. It arrived on
2026-09-12 in the same second that a fan stage was changed on the panel, took 11
and later 12, and fell back to 0 both times. It is not in the restart dump, so
its width comes from the frame that carried it behind `0x2e`: one byte. What the
value counts is open — an index of which setting changed is the obvious guess.

`0x08` was read here as a start and shutdown marker, which is too narrow. It
does report 0 immediately before a controlled shutdown and then 1 and 3 while
coming up, but on 2026-07-20, a day with no restart at all, it reported 1 on its
own at 10:17, at 20:17 and at 00:29, and live on 2026-09-12 it went from 3 to 1
during steady operation. What it marks besides a start is open. The metric is
still called `climos_lifecycle_state`, a name from the narrower reading.

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

Unchanged across all twelve recordings — and *only* in the restart dump, which
is not a limitation of the recordings but of the bus. Of the nine daily dumps,
three carry a restart and report all 124 registers; the other six report 15 to
18, and every settings register is missing from them. A setting changed at the
panel therefore does not reach the bus when it is changed. It becomes visible
the next time the master emits its register dump, which is after a restart. Any
attempt to identify a settings register has to be built around that:

```
climos-exporter replay <after>.bin --against <before>.bin
```

with both recordings carrying a restart.

| Reg             | Value                       | Meaning                                                    |
| --------------- | --------------------------- | ---------------------------------------------------------- |
| `0x1b`          | bit field                   | feature releases, bit 7 frees the summer ventilation        |
| `0x2d` – `0x30` | 17, 18, 47, 85              | `0x2e` – `0x30` are fan stages 1 to 3 in percent            |
| `0x31` – `0x33` | −5, −5, −5                  | debalancing per stage                                      |
| `0x3b`          | 35 min                      | boost duration                                             |
| `0x3c`          | 45 min/h                    | away interval, minutes of stage 1 per hour                 |
| `0x41`          | 24.0 °C                     | t_som, summer ventilation threshold                        |
| `0x43`          | 1.0 K                       | H_som, hysteresis on t_som                                 |
| `0x49`          | 13.0 °C                     | t_aul_min, below which summer ventilation stays off        |
| `0x34` – `0x39` | 17, 25, 38, 47, 56, 64      | the step ladder the sheet attributes to an LED panel       |
| `0x3a`          | 100                         | unidentified                                               |
| `0x4d` – `0x50` | 17, 17, 100, 100            | 0-10 V: Umin 1.7 V, n_min 17 %, Umax 10.0 V, n_max 100 %   |
| `0x51` – `0x54` | 50, 20, 190, 80             | 4-20 mA: Imin, n_min, Imax, n_max                          |
| `0x4a` – `0x4c` | −2.0, −3.0, −3.0 °C         | frost protection thresholds, one per mode                  |
| `0x56`, `0x60`  | 22.0 °C                     | unidentified                                               |
| `0x64` – `0x78` | 21 registers of 8 bytes     | weekly schedule                                            |

The manual names three frost protection modes — *eco*, *sicher* and *Feuchte-WT*,
each with its own threshold, the last being the standard for an enthalpy
exchanger and the only one that applies to this device. Three modes with one
threshold each is what `0x4a` – `0x4c` are, rather than three unrelated limits.
At the threshold the fans are switched off for a while; on a Comfort the PTC
defroster is energised first and the fans follow only if that is not enough.

The fan stages are settled. The panel's Lüfterstufen screen shows 18 %, 47 % and
85 %, and when stage 1 was moved from 17 % to 18 % it was `0x2e` that followed,
so `0x2e` – `0x30` are the three stages and the `0x2d` in front of them is
something else. The manual's constraint of 20 % < LS1 does not hold in this
firmware, which accepts and displays 18 %.

The filter interval is settable from 30 to 180 days in steps of ten, which the
160 days in `0x3d` fits. The panel also counts how far the interval has been
exceeded, and that counter has no register here yet — `0x3f`, which holds
11 days and appears only in the restart dump, is the obvious candidate.

`0x4d` – `0x50` give the characteristic of the analog input:
`n% = 17 + (U − 1.7) · (100 − 17) / (10 − 1.7)`, and because `83 / 8.3 = 10`
that reduces to `n% = 10 · U`. The KNX setpoint in percent therefore lands in
the unit one to one as the fan setpoint — which is exactly what `0x55` measures.

Both endpoints are confirmed twice over. The manual describes exactly this pair
of points — a start value p1 and an end value p2 with a straight line between
them, over a fan speed range of 17 % to 100 % — and Novus owners were told by
Paul's service to put the analog input into the sensor automatic mode and then
read 1.7 V as 17 % and 10 V as 100 %, which is what these four registers hold.
The current input has a plausibility check on top: below 3 mA for more than a
second is a fault, cleared again above 3.5 mA for a second.
On the predecessor the analog value was mapped internally onto seven fan stages;
here it is not, and `0x55` follows the setpoint to the tenth of a percent. The
input reaches nothing else either: the bypass cannot be driven from outside, and
without a control panel on the bus the unit falls back to a reduced emergency
programme.

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

**The operating mode `0x27`, `0x28` and `0x29`** were watched live on
2026-09-12 while the modes were selected on the panel one after another, with
`shelly_power_w` alongside:

| Mode selected | `0x27` | `0x28` | `0x29` | Power draw |
| ------------- | -----: | -----: | -----: | ---------: |
| automatic     |      6 |      6 |      6 |     19 W   |
| boost         |      4 |      4 |  **6** |    103 W   |
| away          |      5 |      5 |  **5** |     16 W   |
| fan stage 2   |      2 |      2 |      2 |            |

Both modes take effect, boost upward and away downward. Away running stage 1 for
part of every hour is what the four minutes caught above cannot show and the
manual states outright. `0x27` and `0x28` carry
the mode that is running; `0x29` follows every selection except the boost, where
it holds at 6. That fits a field holding the mode to return to: a boost expires
by itself, while away and a fixed fan stage last until they are changed. The same boost is in the
recordings: on 2026-07-20 `0x28` read 4 from 08:25:11 to 08:54:19 and the draw
sat at 94 to 97 W for exactly those 29 minutes.

What does *not* move through any of it is `0x55`, which held 24.7 % throughout,
and `0x44`, which held 139. So `0x55` reports the 0-10 V input and nothing else
— not the fan's actual output — and no register in this map reports that output.
For "when was boost active" the answer is therefore `0x27`/`0x28` for the panel
and `0x55` for KNX: a boost driven over KNX is a short upward excursion of
`0x55` while the mode stays at 6, one pressed on the panel is the mode going to
4 while `0x55` stays put.

**The settings registers** through the panel, because they reach the bus only in
the restart dump. On 2026-09-12 six settings were changed on the panel and
photographed, the unit was restarted, and the dump from that day was held against
the last one carrying a restart:

```
climos-exporter replay 2026-09-12.bin --against dumps/2026-08-21.bin
```

Six registers differed and each one matched a photograph: `0x2e` 17 → 18 for fan
stage 1, `0x3b` 30 → 35 for the boost duration, `0x3c` 30 → 45 for the away
interval, `0x43` 5 → 10 for the summer ventilation hysteresis, and `0x1b` and
`0x1a` for the summer ventilation itself. Nothing else in the 124 registers
moved, which is what makes the assignment tight: a changed setting and a changed
register, with no third candidate to choose between.

The photographs stay out of the repository and sit beside the checkout in
`fotos/`, numbered in the order they were taken so that the before and after of
each screen sit next to each other. What each one showed is written into the
tables here, which is what a later reader can work from.

**The device answers `0x87`** are *not* settled. They were recorded here as
acknowledgements carrying the CRC of the frame just sent, which held for the
handful of frames examined during a restart but falls apart at scale: across
2026-07-20 only 4 165 of 29 491 of them match that rule, 14 %. Their payload is
`00` plus a two byte value: 2 616 distinct values across the 33 098 answers of
2026-09-11, but with fifteen of them covering 85 % of the traffic and `00 00`
the most frequent. A handful of states plus a long tail is what a fingerprint of
the sending device's state looks like — and the `Befehlsbeschreibung`
sheet describes exactly that handshake for the older firmware, where a device
answers a poll with "nothing changed" or "yes, my configuration changed" and the
master then asks for it. The matches during a restart fit that too: right after
the master pushes a configuration, the device's fingerprint is the CRC of what
it just received. None of this is proven.

## The predecessor generation

`Paul_Novus300_RS485.xlsx` is not a manufacturer document. It was assembled in
January 2016 in the knx-user-forum thread on the ComfoAir plugin, where two
people logged a Novus 300 and a Focus 200 with an ATmega reading the bus in nine
bit mode and published six successive versions of the sheet. That settles both
why its structure fits and why its details do not: it describes the generation
before this one.

Its frame format is a different one:

```
<address, one nine bit character> <len> <type> <data …> <checksum>
```

The checksum is a plain XOR over every byte starting from `0xFE`, so that the
XOR across a complete frame including its checksum is zero. This unit uses a two
byte header, a command byte, a length byte whose bit 7 is a flag, and CRC-16
CCITT. Nothing written for the old format reads these frames.

What does carry over is the conversation. The master owns the bus and nobody
speaks unasked. It polls each device in turn, and a device answers either with
its standard reply or with the same reply carrying bit 7 set, which means
"something of mine changed"; the master then asks for the changed record and the
device sends it. Captured on a Novus 300 while a fan stage was selected on the
panel:

| Direction      | Frame                                          | Meaning                       |
| -------------- | ---------------------------------------------- | ----------------------------- |
| master → panel | `02 00 01 fd`                                  | poll                          |
| panel → master | `01 02 02 02 10 ed`                            | nothing changed               |
| panel → master | `01 02 02 82 10 6d`                            | something changed, bit 7 set  |
| master → panel | `02 02 03 00 10 ed`                            | send me what changed          |
| panel → master | `01 11 04 00 02 18 22 11 04 07 08 0f 03 01 …`  | the record, `03` is the stage |

That is the handshake the `Befehlsbeschreibung` sheet describes and the one the
`0x87` answers here are suspected to be, now with a byte level capture behind
it. Bit 7 as a flag on a byte that otherwise carries a number is the same
convention this unit's length byte uses.

The same thread pins down what the fan slave is. The four NTC temperature
sensors, a Hall sensor, the motor control and the bypass control all hang on it,
and its status byte carried bits for whether the fans run, whether the target
speed is reached and, probably, whether the bypass is open. None of it reached
the bus as a number there: the slave answered polls and sent no temperatures at
all, which is why that thread ended without them. This unit publishes all four,
so the newer master asks for more than the old one did.

## The ComfoAir protocol is a different bus

Zehnder's ComfoAir units — and the Paul Santos and Wernig G90 that are the same
machine — speak a documented RS-232 protocol with working implementations for
FHEM, smarthome.py and the Wiregate. It is not this protocol and it does not
reach this unit. There a frame is `07 f0 <command, two bytes> <len> <data …>
<checksum> 07 0f`, acknowledged with `07 f3`, the checksum is the byte sum plus
173 modulo 256, a `0x07` inside the data is doubled, and a temperature is stored
as `(°C + 20) × 2`. The author of the smarthome.py plugin states plainly that
the Novus 300 has RS-485 instead and that his plugin does not work with it.

Its command table is still worth reading as a list of what a unit of this family
exposes, because several of those values have no register here yet: the bypass
position as one byte with three states — open, closed, stopped — along with its
summer mode, factor and correction; separate supply and extract percentages per
fan stage; the preheater state; and a comfort temperature settable from 12 °C to
28 °C.

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

## Summer ventilation, and why there is no bypass register

This unit has no bypass flap. Free cooling is done differently, and the manual
calls it *Sommerlüftung ohne Bypass*: the exhaust fan is switched off, so the
extract air stops giving its heat to the supply air, and it is switched back on
for two minutes every hour to re-read the temperatures and check whether the
conditions still hold.

The switching condition is written out in the manual in terms of the sensor
numbers, and the panel holds the three parameters it needs:

```
active when   T1 < T3   and   T1 > t_aul_min   and   T3 > t_som + H_som
```

| Parameter   | Panel    | Register | Value on the wire |
| ----------- | -------- | -------- | ----------------- |
| `t_som`     | 24.0 °C  | `0x41`   | 240               |
| `H_som`     | 1.0 °C   | `0x43`   | 10                |
| `t_aul_min` | 13.0 °C  | `0x49`   | 130               |

`0x43` is certain, because it moved from 5 to 10 in the recording at the moment
the hysteresis was changed from 0.5 °C to 1.0 °C on the panel. `0x41` and `0x49`
are matched by value against the same screen rather than by a change, so they
are one step weaker.

For twelve recordings the function never ran, and the reason was not the
threshold. The panel has two boxes: *Sommerlüftung möglich*, which was ticked all
along because the device has no flap, and *Sommerlüftung aktiv*, which was not.
Ticking the second one on 2026-09-12 at 19:46 turned it on, and the bus shows
what happened:

- `0x1b` went from `0x0073` to `0x00f3`, which is bit 7 of its low byte. The
  register appears only in the restart dump and holds no number, so it is read as
  the block of feature releases with that bit freeing the summer ventilation.
- `0x1a`, the status word, went from 13 to 37 and stayed there.
- The supply air fell to the outdoor air. Recovery had sat between 0.80 and 0.91
  all afternoon; within half an hour of the change it read 0.45 and then 0.10.

That last line is the one that matters, because it is measured downstream of
every reading above: with the exhaust fan stopped, the heat exchanger no longer
transfers anything, and the house gets outdoor air at outdoor temperature. Before
the change the unit was warming 17 °C outdoor air to 25 °C while the rooms stood
at 27 °C, and it did that for 89 % of the preceding thirty days.

## Writing to the bus

Nothing here writes to the bus; the exporter only listens. But the recordings
answer part of the question of whether one could, so it is written down.

Changing the operating mode is a single frame to the register space. These were
built from the CRC routine alone and then checked against the bytes captured
while the modes were selected on the panel on 2026-09-12 — they match exactly:

| Mode         | Frame                                 |
| ------------ | ------------------------------------- |
| fan stage 1  | `01 00 85 03 7c 1d 28 00 01`          |
| fan stage 2  | `01 00 85 03 1f 2d 28 00 02`          |
| fan stage 3  | `01 00 85 03 3e 3d 28 00 03`          |
| boost        | `01 00 85 03 d9 4d 28 00 04`          |
| away         | `01 00 85 03 f8 5d 28 00 05`          |
| automatic    | `01 00 85 03 9b 6d 28 00 06`          |

The frame carries a target address and no source, no authentication and no
sequence number, so nothing in it distinguishes a frame of ours from one of the
master's.

What the recordings cannot answer is who is allowed to write to `0x0100`. The
mode frame appears before the master polls the fan slave and publishes `0x27`
and `0x29`, which reads like the panel writing and the master reconciling
afterwards. Against that stands the unresolved `0x87` answer above: there is a
device to master channel here that is not understood, and the panel may well be
using it, in which case the master owns `0x0100` and an injected frame would be
ignored or overwritten on the next cycle.

The predecessor generation was tried and supplies the failure modes.
Transmitting works: an Arduino answered the startup scan at `0x0103`, was
accepted and received the master's complete register push. Being accepted turned
out not to be enough — after the configuration the master never polled it again.
Registering at `0x0113` was worse: the master went on to configure `0x0112`, got
no answer, and all traffic on the bus stopped. What did work there was a man in
the middle, an AVR relaying every frame between master and panel, setting the
change flag in the panel's answer and editing the fan stage in the record that
followed. Two people independently concluded that the old bus cannot be driven
from outside at all.

This unit is not that generation — the mode frames above are real writes to
`0x0100` captured from the panel, a mechanism the old bus did not have — so the
verdict does not transfer. The failure mode does: a wrong answer at the wrong
address takes the whole bus down, not just the injected frame.

Three practical obstacles regardless of that:

- The adapter has to be able to transmit at all, which needs driver enable
  control on the transceiver.
- The first byte problem runs both ways. Sending the frame twice back to back
  covers it, because a frame that follows another without an idle gap keeps its
  first byte — that is exactly what the capture shows. Setting the same mode
  twice does no harm.
- The bus is busy. Measured over 1 373 447 gaps on 2026-09-12: the median gap is
  zero, the 75th percentile 9 idle bytes and the 95th 30. One frame needs 10.3 ms
  at 9600 baud with eleven bits per character, two need 20.6 ms, so a frame fits
  into 27.6 % of the gaps and a doubled frame into 13.7 %. A sender has to wait
  for a gap rather than transmit blind, or it collides with a poll.

A staged way to find out, each step observable and reversible: transmit anything
and check that the exporter's own receiver sees it, which tests wiring and
timing without asking a device to act; then write the mode that is already set,
which is a no-op if it is accepted and equally a no-op if it is not; then the
mode that is wanted. `climos_error_code`, the panel's message log and a dip in
`climos_frames_total` are what a collision or a rejected write would show up in.

The 0-10 V input is not a substitute for the away mode. It reproduces a fan
setpoint, and away runs the fan intermittently, so many minutes on and as many
off; a constant setpoint cannot express that.

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

## What the manufacturer documents

Zehnder's help centre carries a section on the Paul units that covers a few of
the things guessed at here. The pages sit behind Cloudflare but the help centre
API serves them:

- [Störungsübersicht](https://zehnder-systems-de.zendesk.com/hc/de/articles/4409019411345)
  lists every fault the panel can show, for the LED unit as an LED pattern and
  for the TFT as text: sensor faults 1 to 4, supply air too cold, outdoor air too
  cold for longer than 30 minutes, fan 1 and fan 2 reporting no speed, bypass
  without an end position, communication faults for the fan slave, the defroster,
  the heater, a general one and one for the control unit, plus a numeric
  `092` for "no communication between the master board and the TFT panel". Those
  are the names to expect once `0x0e` ever leaves zero, and they match the
  wording in this unit's own message log.
- [Zeitautomatik](https://zehnder-systems-de.zendesk.com/hc/de/articles/4409557287825)
  describes the weekly programme from the operator's side and corroborates the
  reading of `0x64` – `0x78` independently: a quarter hour slot takes one of
  exactly four states — fans stopped, reduced, nominal, intensive — which is two
  bits, and the panel edits the days in the groups Mo-Fr and Sa-So, which is
  precisely the split the registers show.
- [Beiträge allgemein Paul](https://zehnder-systems-de.zendesk.com/hc/de/sections/15868648209821)
  is the section those sit in, seventeen articles including one per fault and one
  on resetting the filter runtime.

What the manufacturer does not document is this bus. Paul's service declined to
release the protocol to the people who asked for it, and a Loxone support ticket
came back with the statement that the RS-485 interface serves internal
diagnostics and is not approved for third parties. Everything above is
reconstructed, and the controller boards are not even Paul's own — they come
from KD Elektroniksysteme.

Zehnder's fault list carries a caveat that does not apply here: it says the duct
a sensor number sits in depends on whether the unit is the LINKS or the RECHTS
variant. That is written for the range as a whole. The CLIMOS manual names T1 as
the outdoor air and T3 as the extract air with no reference to the form, and the
form only mirrors which duct connection sits where. T2 and T4 are the remaining
two by elimination, each being the far end of the air path its odd numbered
partner starts: T1 to T2 across the supply side, T3 to T4 across the exhaust
side. The energy balance rules out the alternative reading, which would put the
exhaust air at 5.5 °C while the outdoor air was 21 °C.

That matters for fault handling rather than for the values: a panel message
naming *Sensor 2* points at `0x81`, and looking at `0x82` would send the search
to the wrong sensor.

## Open points

- **`0x44`.** Sits between 136 and 141, about two units higher in winter than in
  summer, is exactly constant within a day and does not react to the fan
  setpoint and not to a boost pressed on the panel either, although the fan
  audibly and measurably ramps for that one, which rules out anything air
  related. After a start it climbs from
  10 and decelerates as it goes: 127 after four minutes, 136 after twenty-two,
  137 only after five hours. That is a heavily filtered measurement approaching
  a value, not an actuator being driven. It is exported raw as
  `climos_register{register="0x44"}`.

  The climb also settles how the byte is read. It passes 127 and 128 without a
  break, so the register is unsigned; sign extension would turn that step into a
  jump from 127 to −128.

  One candidate comes out of what these units do that their competitors do not:
  they hold the set air volume constant as the filters load, which the passive
  house certificate for the Novus 450 lists as automatic volume flow balance and
  the comparable Zehnder does not have. A controller doing that carries an
  estimate of the resistance it is working against, and such an estimate settles
  after a start, stays put over a day, drifts with the season and ignores the
  setpoint, because it describes the duct rather than the demand. That fits
  every observation above except one: a resistance estimate should creep as the
  filter loads, and `0x44` is exactly constant within a day.
- **What the `0x87` answers carry.** A two byte value, fifteen of which cover
  85 % of the answers with a tail of 2 600 more, matching the CRC of the
  preceding frame in 14 % of cases and unexplained in the rest. The predecessor's
  poll and change handshake is the best fit so far.
- **What `0x08` marks.** Beyond a start and a controlled shutdown it fires on
  its own a few times a day, and neither the operating mode nor the fan setpoint
  moves with it.
- **`0x3a`** = 100. It was read as the filter interval until the panel showed
  that interval to be 160 days, which is `0x3d`. What `0x3a` counts is open. The
  panel keeps one counter this document has no register for — how far the filter
  interval has been exceeded — but `0x3f` at 11 days fits that better than 100
  does.
- **Bit order in the weekly schedule**, which decides whether the stage 3 hour
  runs from 08:00 to 08:30 or from 08:30 to 09:00. The fourth state, fans
  stopped, has not been seen in any recording.
- **`0x64` – `0x78` as the schedule** is corroborated by the manufacturer's
  description of the weekly programme, which has four states per quarter hour
  and edits in the groups Mo-Fr and Sa-So. What stays open is only which bit
  pattern means which state, and in which order the slots run.
- **Which register reports the fan's actual output.** `0x55` is the 0-10 V input
  and stays put while a boost from the panel drives the draw from 18 W to 95 W,
  so something must carry it, and nothing in the map moves with it. The fan
  slave carries a Hall sensor, so the speed is measured inside the device, and
  on the predecessor its status byte reported whether the target speed had been
  reached.
- **What the rest of `0x1b` releases.** Bit 7 frees the summer ventilation. The
  remaining bits of `0x0073` are unread, and the panel offers a post-heater and
  an EWT damper that are not fitted here, which is where the others most likely
  belong.
- **What `0x62` counts.** It fires only when a setting is changed and took 11 and
  12 on the one occasion recorded.
- **`0x56` and `0x60`** both hold 22.0 °C. They were read as the bypass
  temperature and then as the summer ventilation threshold; the panel shows that
  threshold to be 24.0 °C in `0x41`, so both readings were wrong and what these
  two carry is open.
- **Which register carries the defroster's state and its bus temperature.** The
  node answers every poll, but nothing it sends changes while it is idle. That
  needs a recording from a day below −2 °C; the first such day after 2026-02-21
  is still to come.
- **The read buffer in `reader.go`** grows without bound as long as no frame can
  be extracted. It does not happen in the recordings, because a unit without
  power delivers no bytes at all.

## Sources

The manual is the manufacturer's own and describes this device. Everything
attributed above to the predecessor generation, to Paul's service or to other
owners comes from the threads below it; none of those describes this unit.

- **Betriebsanleitung CLIMOS F 200, Version 2.0_03/2019**, PAUL Wärmerückgewinnung
  GmbH, 64 pages, served from Zehnder's media library as
  [HyzMFCuC](https://zehnder.picturepark.com/v/HyzMFCuC). It is the source for the
  sensor numbering, the summer ventilation and its switching condition, the frost
  protection modes, the boost and away definitions, the filter interval range,
  the analog characteristic and the constant volume flow. Its own legal notice
  reserves republication, so it is named here rather than committed: a share link
  can expire, and the title and version above are what finds it again.

- [Neues Plugin ComfoAir (KWL Wohnraumlüftung Zehnder, Paul, Wernig)](https://knx-user-forum.de/forum/supportforen/smarthome-py/31291-neues-plugin-comfoair-kwl-wohnrauml%C3%BCftung-zehnder-paul-wernig)
  — the ComfoAir RS-232 plugin on pages 1 to 5, then from page 6 on the RS-485
  reverse engineering of the Novus 300 and Focus 200: the nine bit framing, the
  frame format and its XOR checksum, the poll and change handshake, the
  registration experiments, and the six versions of `Paul_Novus300_RS485.xlsx`.
- [Neues Modul für ComfoAir, Paul Santos und Lüftungen mit kompatibler Steuerung](https://forum.fhem.de/index.php?topic=23373.15)
  — the FHEM side of the ComfoAir protocol, including the bypass byte and the
  readings those units expose; it also names RS-485 as the other line.
- [Integration KWL Paul Novus 300](https://www.loxforum.com/forum/german/software-konfiguration-programm-und-visualisierung/14821-integration-kwl-paul-novus-300)
  — the 1.7 V to 10 V characteristic from an owner, the terminals for the analog
  input and the external enable, and Loxone's support answer on the RS-485
  interface.
- [Steuerung der PAUL Wohnraumlüftung über KNX](https://knx-user-forum.de/forum/%C3%B6ffentlicher-bereich/knx-eib-forum/15749-steuerung-der-paul-wohnrauml%C3%BCftung-%C3%BCber-knx)
  — Paul's answers on what the analog input can and cannot do, and on the
  emergency programme without a control panel.
- [Lüftungsanlage Paul Novus (F) 450 oder Zehnder ComfoAir 550](https://knx-user-forum.de/forum/%C3%B6ffentlicher-bereich/geb%C3%A4udetechnik-ohne-knx-eib/821295-l%C3%BCftungsanlage-paul-novus-f-450-oder-zehnder-comfoair-550)
  — the constantflow comparison and the passive house certificates behind it.
- [Reparatur Elektronik Paul Thermos Lüftungsgeräte](https://www.haustechnikdialog.de/Forum/t/234416/Reparatur-Elektronik-Paul-Thermos-Lueftungsgeraete)
  — not reachable; the site answers every request with HTTP 403.
- [Protokollbeschreibung ComfoAir](http://www.see-solutions.de/sonstiges/Protokollbeschreibung_ComfoAir.pdf)
  — the specification the ComfoAir implementations are built from.
