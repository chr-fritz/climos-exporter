# climos-exporter

[![Go build](https://github.com/chr-fritz/climos-exporter/actions/workflows/go.yaml/badge.svg)](https://github.com/chr-fritz/climos-exporter/actions/workflows/go.yaml)
[![Quality gate](https://sonarcloud.io/api/project_badges/measure?project=chr-fritz_climos-exporter&metric=alert_status)](https://sonarcloud.io/summary/new_code?id=chr-fritz_climos-exporter)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)

Reads the internal RS-485 bus of a Zehnder Climos 200 heat recovery ventilation
unit and exposes what it carries as Prometheus metrics. The same bus is used by
the Paul Novus 300, which shares the platform.

The unit has no documented interface. The bus protocol was reverse engineered
from recorded bytestreams and is written down in
[`docs/protocol.md`](docs/protocol.md), together with the evidence behind each
register's meaning and the points that are still open.

## What you need

A passive RS-485 to USB adapter on the unit's bus — an FTDI FT232R works. The
bus runs at 9600 baud, 8 data bits, space parity, one stop bit; the exporter
configures that itself, you only pass the device node.

Reading is passive. The exporter never writes to the bus.

> **Note on wiring.** The bus idles in the space state, which the receiver reads
> as a continuous run of null characters. That costs the first byte of most
> frames; the exporter reconstructs it, see
> [the protocol notes](docs/protocol.md#the-bytestream-and-its-defect). Watch
> `climos_frames_total{result="repaired"}` — if fail-safe bias is present on the
> A/B pair, that share drops towards zero.

## Running it

```sh
docker run --rm --device /dev/ttyUSB0 -p 8080:8080 \
  ghcr.io/chr-fritz/climos-exporter:latest run -d /dev/ttyUSB0
```

Container images cover `linux/amd64` and `linux/arm64` at
[ghcr.io/chr-fritz/climos-exporter](https://ghcr.io/chr-fritz/climos-exporter).
Every [release](https://github.com/chr-fritz/climos-exporter/releases) also
carries static binaries for Linux on `amd64`, `arm64` and `armv7`, each with an
SBOM. A systemd unit and its environment file sit in
[`scripts/systemd`](scripts/systemd) for running it straight on a host.

Metrics are served at `/metrics`, plus `/live` and `/ready` for probes.

### Options

| Flag              | Default          | Meaning                                                |
| ----------------- | ---------------- | ------------------------------------------------------ |
| `-d`, `--device`  | `/dev/ttyUSB0`   | serial device the unit is connected to                 |
| `-p`, `--port`    | `8080`           | port the metrics endpoint listens on                   |
| `--stream-dir`    | *(off)*          | directory for raw bytestream recordings, one per day   |
| `--parity`        | `space`          | `space`, `mark` or `none`, see below                   |
| `--log_level`     | `info`           | `trace`, `debug`, `info`, `warn`, `error`              |
| `--log_format`    | `text`           | `text` or `json`                                       |
| `--config`        | `~/.climos-exporter.yaml` | config file                                   |

`--parity` selects which half of the bus traffic the receiver accepts and is an
experiment rather than an operating mode. The line sends eleven bit characters,
nine of them data, and the ninth marks a character as an address on this
controller family; space parity passes the characters whose ninth bit is zero
and is the only setting that yields whole frames. Mark parity passes exactly the
complement, which is how a recording answers whether the marking is still there
— and if it is, framing no longer has to be guessed. A recording taken at
anything other than space parity is written to `<date>-<parity>.bin` so it never
lands in the archive of ordinary recordings.

`--device`, `--port`, `--stream-dir` and `--parity` can also come from the
environment as `EXPORTER_DEVICE`, `EXPORTER_PORT`, `EXPORTER_STREAM_DIR` and
`EXPORTER_PARITY`, or from the config file, which is read from `~/.climos-exporter.yaml` unless `--config` points
elsewhere. The two logging flags take their value from the command line only.

```yaml
exporter:
  device: /dev/ttyUSB0
  port: 8080
  stream:
    dir: /var/lib/climos-exporter
```

## Metrics

| Metric                                       | Register       | Meaning                                            |
| -------------------------------------------- | -------------- | -------------------------------------------------- |
| `climos_temperatures_indoor_in`              | `0x81`         | supply air entering the house, °C                  |
| `climos_temperatures_outside`                | `0x82`         | outdoor air, °C                                    |
| `climos_temperatures_indoor_out`             | `0x83`         | extract air leaving the rooms, °C                  |
| `climos_temperatures_house_out`              | `0x84`         | exhaust air leaving the house, °C                  |
| `climos_fan_setpoint_percent`                | `0x55`         | fan setpoint, mirrors the 0-10V control input      |
| `climos_operating_mode`                      | `0x28`         | mode selected on the panel                         |
| `climos_filter_remaining_seconds`            | `0x3e`         | time left until the filter change is due           |
| `climos_filter_interval_seconds`             | `0x3d`         | configured interval between filter changes         |
| `climos_operating_seconds{counter=…}`        | `0x26`, `0x85` | operating time, total and fan, as the panel counts it |
| `climos_run_state`                           | `0x1d`         | 1 running, 0 shutting down, 3 shortly after a start |
| `climos_status_word`                         | `0x1a`         | status word, 13 during normal operation            |
| `climos_lifecycle_state`                     | `0x08`         | 0 before stopping, 1 then 3 while starting         |
| `climos_error_code`                          | `0x0e`         | error code, 0 when healthy                         |
| `climos_register{register=…}`                | all 1-2 byte   | unsigned wire value of every numeric register      |
| `climos_device_info{…}`                      | identity       | bus version and the attached nodes' article numbers |
| `climos_bus_restarts_total`                  | —              | restarts, one per address scan                     |
| `climos_frames_total{result=…}`              | —              | frames read, by framing result                     |
| `climos_unknown_registers_total{register=…}` | —              | records dropped for want of a known width          |
| `climos_last_package_timestamp_seconds`      | —              | when the last frame arrived                        |

A register that has not been seen reports `NaN` rather than 0 — the
once-a-minute registers and the ones that only appear after a restart are
legitimately absent for a while after start.

A rising `climos_unknown_registers_total` means a device reports a register
whose width is not in the table, and the rest of that frame is lost with it.
[`docs/protocol.md`](docs/protocol.md#preheater) explains how to work the width
out from the next restart dump.

## Dashboard

[`dashboards/climos-exporter.json`](dashboards/climos-exporter.json) shows every
metric on one page: the air temperatures with the heat recovery ratio derived
from them, the fan setpoint, the state registers as a timeline, both operating
counters, the filter countdown, and a bus section with the framing results and
the raw value of every register. Import it and pick the Prometheus data source
when asked. It queries the metrics this repository produces, so a version older
than the dashboard can leave a panel empty — `climos_operating_seconds` gained
its `fan` counter after v0.1.0, for instance.

The bus section is the one to look at when something is off: a rising repaired
share means the line is losing bytes, and a rising unknown register count means
a device reports something this exporter cannot place.

## Writing to the bus

The exporter only listens and has no write path. Whether one could is worked
through in [`docs/protocol.md`](docs/protocol.md#writing-to-the-bus): the frames
that set each operating mode are known and verified, but who is allowed to write
to the register space is not, and the bus is busy enough that a sender has to
wait for a gap.

## Recording and replaying the bus

With `--stream-dir` the reader writes the raw bytestream to one file per day.
The `replay` command runs such a file back through the same framer and decoder
the exporter uses, without touching a serial port.

```sh
climos-exporter replay dumps/2026-08-05.bin
```

By default it lists every register the recording carried with how often it moved
— the quickest check of a new firmware or a rewired bus against what the
protocol notes describe.

```sh
climos-exporter replay dumps/2026-07-20.bin --changes
```

`--changes` lists the individual value changes in order, dropping the registers
that move constantly, so an operating mode selected on the panel stands out from
the temperatures. Times come from the filter countdown, the only monotone clock
on the bus; `--start 02:00` puts a wall clock next to it.

```sh
climos-exporter replay dumps/after.bin --against dumps/before.bin
```

`--against` compares two recordings and reports the registers whose value
differs. This is the one that identifies a setting: the settings registers reach
the bus only in the register dump the master emits after a restart, so a change
made on the panel becomes visible once the unit has restarted, and both
recordings have to carry a restart for the comparison to see anything.

`--register 0x44,0x55` restricts any of the three to named registers.

## Development

```sh
make build     # binary into build/
make ci-check  # tests, coverage, vet and lint as CI runs them
make generate  # regenerate the mocks
```

## License

Apache License 2.0, see [LICENSE](LICENSE).
