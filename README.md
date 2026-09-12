# Zehnder Climos 200 Prometeus Exporter

Liest den RS-485-Bus einer Paul Novus 300 / Zehnder ClimOS mit und stellt die
Werte als Prometheus-Metriken bereit.

[`docs/protocol.md`](docs/protocol.md) beschreibt das Protokoll: Frameformat,
die Registerkarte mit allen bekannten Breiten und Bedeutungen, und woran die
Bedeutungen jeweils festgemacht sind.

## Metriken

| Metrik                                  | Register        | Bedeutung                                              |
| --------------------------------------- | --------------- | ------------------------------------------------------ |
| `climos_temperatures_indoor_in`          | `0x81`          | Zuluft in °C                                           |
| `climos_temperatures_outside`            | `0x82`          | Außenluft in °C                                        |
| `climos_temperatures_indoor_out`         | `0x83`          | Abluft in °C                                           |
| `climos_temperatures_house_out`          | `0x84`          | Fortluft in °C                                         |
| `climos_fan_setpoint_percent`            | `0x55`          | Lüfter-Sollwert, entspricht dem 0–10-V-Eingang         |
| `climos_operating_mode`                  | `0x28`          | am Bedienteil gewählte Betriebsart                     |
| `climos_filter_remaining_seconds`        | `0x3e`          | Restzeit bis zum Filterwechsel                         |
| `climos_filter_interval_seconds`         | `0x3a`          | eingestelltes Wechselintervall                         |
| `climos_operating_seconds{counter=…}`    | `0x26`, `0x85`  | Betriebszeit                                           |
| `climos_run_state`                       | `0x1d`          | Laufzustand                                            |
| `climos_status_word`                     | `0x1a`          | Statuswort                                             |
| `climos_lifecycle_state`                 | `0x08`          | Start- und Abschaltmarke                               |
| `climos_error_code`                      | `0x0e`          | Fehlercode                                             |
| `climos_register{register=…}`            | alle            | Rohwert jedes numerischen Registers                    |
| `climos_device_info{…}`                  | Identität       | Artikelnummern und Gerätenamen                         |
| `climos_bus_restarts_total`              | —               | Neustarts, einer je Adressscan                         |
| `climos_frames_total{result=…}`          | —               | gelesene Frames, nach Framing-Ergebnis                 |
| `climos_unknown_registers_total{register=…}` | —          | verworfene Records mit unbekannter Registerbreite       |
| `climos_last_package_timestamp_seconds`  | —               | Zeitpunkt des letzten gelesenen Frames                 |

Steigt `climos_unknown_registers_total`, meldet ein Gerät ein Register, dessen
Breite hier nicht hinterlegt ist — dann geht der Rest dieses Frames verloren.
[`docs/protocol.md`](docs/protocol.md) beschreibt, wie man die Breite aus dem
nächsten Registerabzug nachträgt.

Ein Register, das noch nie gesehen wurde, meldet `NaN` statt 0. Die einmal pro
Minute gesendeten und die nur nach einem Neustart gesendeten Register fehlen
nach dem Start also für eine Weile.

## Aufzeichnungen auswerten

Mit `--stream-log-dir` schreibt der Reader den rohen Bytestrom tageweise mit.
Eine solche Datei lässt sich gegen den aktuellen Decoder laufen lassen:

```sh
CLIMOS_DUMP=$PWD/dumps/2026-08-05.bin go test ./pkg/climos/ -run TestReplayDump -v
```
