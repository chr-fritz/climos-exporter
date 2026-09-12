# Das RS-485-Protokoll der Lüftung

Rekonstruiert aus 12 Tagesdumps des rohen Bytestroms (Februar bis September 2026),
abgeglichen mit `knx_Lueftung_Stellwert{type="actual"}` und
`shelly_power_w{instance="Lüftung"}` aus Prometheus sowie mit der Vorarbeit aus
`Paul_Novus300_RS485.xlsx`.

Geräte am Bus: Master `ETA0036E31E`, Fan controller `SWZ0025B27A`,
Touch TFT 1 `ST00064E26E`.

## Inhalt

- [Der Bytestrom und sein Defekt](#der-bytestrom-und-sein-defekt)
- [Frameformat](#frameformat)
- [Adressen und Kommandos](#adressen-und-kommandos)
- [Nutzdaten sind Registersätze](#nutzdaten-sind-registersätze)
- [Registerkarte](#registerkarte)
- [Wie die Bedeutungen belegt sind](#wie-die-bedeutungen-belegt-sind)
- [Vorheizregister](#vorheizregister)
- [Neustarts](#neustarts)
- [Offene Punkte](#offene-punkte)

## Der Bytestrom und sein Defekt

56 % aller Bytes im Dump sind `0x00`. Das ist die Leerlaufleitung: sie liegt im
Space-Zustand statt auf Mark, der Empfänger liest daraus einen Dauerstrom von
Nullzeichen. Beginnt danach ein echtes Frame, ist der Empfänger mitten in so
einem Phantomzeichen und verschluckt das erste reale Byte — es erscheint als
`0x00`. Alle weiteren Bytes des Frames kommen unversehrt an.

Da jede Adresse auf diesem Bus mit `0x01` beginnt, lässt sich das Byte
rekonstruieren: schlägt die CRC fehl und ist das erste Byte `0x00`, wird
dieselbe Fensterbreite noch einmal mit `0x01` geprüft. Ohne diese Reparatur
verwirft der Framer 88 % des Verkehrs:

| Frametyp      | ohne Reparatur | tatsächlich gesendet | Ausbeute | Inhalt                            |
| ------------- | -------------: | -------------------: | -------: | --------------------------------- |
| `01 00 85 13` |         16 216 |               41 626 |   39,0 % | die vier Temperaturen             |
| `01 00 85 03` |        295 875 |            1 419 124 |   20,8 % | einzelnes Register                |
| `01 01 87 83` |          7 482 |               29 472 |   25,4 % | Quittungen                        |
| `01 04 84 00` |            372 |              424 001 |    0,1 % | Slave-Poll                        |
| `01 00 85 20` |              0 |                1 438 |    0,0 % | Filterreststand, Lüfter-Sollwert  |
| `01 00 85 21` |              2 |                1 437 |    0,1 % | Betriebsstundenzähler             |
| **gesamt**    |    **320 939** |        **2 691 263** | **11,9 %** | gemessen am 2026-09-11          |

Frames, die unmittelbar an ein vorheriges anschließen, behalten ihr erstes Byte.
Nur deshalb kamen die Temperaturen bisher überhaupt durch.

Elektrisch beheben ließe sich das mit Fail-Safe-Vorspannung auf dem A/B-Paar,
dann verschwindet auch die Nullflut. Die Reparatur in Software funktioniert
unabhängig davon. `climos_frames_total{result="repaired"}` zeigt, wie viele
Frames sie rettet; geht der Anteil gegen null, ist die Leitung in Ordnung.

## Frameformat

```
 0    1    2     3     4    5    6 …
+----+----+-----+-----+----+----+-------------+
| Adresse | Cmd | Len | CRC     | Nutzdaten   |
+----+----+-----+-----+----+----+-------------+
```

- **Adresse** 2 Byte big endian, immer `0x01xx`.
- **Len** zählt nur die Nutzdaten. Bit 7 ist ein Flag, keine Länge: `0x83`
  bedeutet drei Nutzbytes. Quittungen setzen es immer.
- **CRC** 16 Bit CCITT über Header ohne die CRC-Bytes plus Nutzdaten, abgelegt
  little endian. Implementierung in [`crc.go`](../pkg/climos/crc.go).

## Adressen und Kommandos

| Adresse            | Bedeutung                                             | Vorkommen               |
| ------------------ | ----------------------------------------------------- | ----------------------- |
| `0x0100`           | Registerraum, Ziel aller Datenframes                  | dauernd                 |
| `0x0101`           | Master                                                | Identität, alle Quittungen |
| `0x0104`           | Slave                                                 | ~424 000 Polls/Tag      |
| `0x0108`, `0x0109` | weitere Teilnehmer                                    | ~320 000 / 350 000/Tag  |
| `0x0102` … `0x010c` | voller Adressraum, beim Start je zweimal gescannt    | nur beim Neustart       |
| `0x01ff`           | Broadcast-Poll                                        | ~9 600/Tag              |

Beim Startscan antworten nur `0x0104`, `0x0108` und `0x0109`; jedes dieser
Geräte meldet dabei eine Artikelnummer — `0x0104` liefert `0x0d`
(`ETA0036E31E`), `0x0108` liefert `0x19` (`SWZ0025B27A`), `0x0109` liefert
`0x1c` (`ST00064E26E`). Die übrigen Adressen werden zweimal angefragt und
bleiben stumm. Die Adresszuordnung aus dem Tabellenblatt `Adressen` passt also
nicht auf diese Firmware: dort stehen `0x0105` und `0x0106` für Defroster und
Elektroheizregister, hier antwortet an beiden Adressen nichts.

Die Datenframes lassen sich nicht auf einzelne Geräte zurückführen. Sie gehen
alle an `0x0100`, und welches Register darin steht, hängt nicht davon ab, wer
zuletzt gepollt wurde: nach einem Poll an `0x0104` folgt in 38 % der Fälle
`0x0e`, in 31 % `0x1d`, nach `0x0109` in 65 % `0x1d`. Der Master sammelt und
meldet gebündelt.

| Kommando      | Bedeutung                                                                  |
| ------------- | -------------------------------------------------------------------------- |
| `0x80` / `0x81` | Anmeldung: Anfrage und Antwort, Nutzdaten `00 00 01 07 01` plus Artikelnummer |
| `0x84` / `0x86` | Keep-alive-Poll und Datenanfrage, immer ohne Nutzdaten                   |
| `0x85`        | Registerdaten an `0x0100` — alles, was exportiert wird                      |
| `0x87`        | Quittung an den Master, Nutzdaten `00` plus die CRC des quittierten Frames  |

## Nutzdaten sind Registersätze

Die Nutzdaten sind kein Struct mit festen Offsets, sondern eine Folge von
Datensätzen `<Register-ID uint16 little endian><Wert>`. Die Breite des Werts
steht nicht auf der Leitung; sie ist je Register fest.

```
44 00 88   81 00 18 01   82 00 fa 00   83 00 18 01   84 00 ff 00
└─0x44─┘   └───0x81───┘  └───0x82───┘  └───0x83───┘  └───0x84───┘
   136        28,0 °C       25,0 °C       28,0 °C       25,5 °C
```

Die Breitentabelle stammt aus dem vollständigen Registerabzug, den der Master
nach jedem Neustart sendet: dort laufen die IDs aufsteigend, und die Paketlänge
legt jede Breite eindeutig fest. Ein Decoder muss beim ersten unbekannten
Register abbrechen — ohne die richtige Breite verschiebt sich alles Folgende.

Werttypen:

- **1 und 2 Byte** vorzeichenbehaftete Ganzzahl, little endian.
- **5 Byte** Zähler: Minute, Stunde, Tage uint16, Jahre. Ein Jahr sind
  genau 365 Tage, nachgerechnet am Überlauf zwischen dem 20.06. und dem 20.07.
- **11 und 16 Byte** ASCII. Die 16 Byte langen beginnen mit einem Typbyte,
  danach 15 Zeichen Name.
- **8 Byte** Bitfeld, siehe Zeitprogramm.

## Registerkarte

### Laufende Messwerte

| Reg    | Breite | Bedeutung                        | Takt   | beobachtet        | Sicherheit  |
| ------ | -----: | -------------------------------- | -----: | ----------------- | ----------- |
| `0x81` |      2 | Zuluft in 0,1 °C                 |  3,5 s | 20,4 – 31,0 °C    | belegt      |
| `0x82` |      2 | Außenluft in 0,1 °C              |  3,5 s | −1,5 – 29,4 °C    | belegt      |
| `0x83` |      2 | Abluft in 0,1 °C                 |  3,5 s | 24,5 – 30,5 °C    | belegt      |
| `0x84` |      2 | Fortluft in 0,1 °C               |  3,5 s | 5,5 – 30,7 °C     | belegt      |
| `0x55` |      2 | Lüfter-Sollwert in 0,1 %         |  2,1 s | 24,6 – 70,0 %     | belegt      |
| `0x1d` |      1 | Laufzustand                      |  0,1 s | 0, 1, 3, 4        | erschlossen |
| `0x1a` |      1 | Statuswort                       |  0,3 s | 0, 1, 13          | erschlossen |
| `0x08` |      1 | Start- und Abschaltmarke         | Ereignis | 0, 1, 3         | erschlossen |
| `0x0e` |      1 | Fehlercode, in 12 Tagen nie ≠ 0  | 0,25 s | 0                 | erschlossen |
| `0x28` |      1 | Betriebsart, nur bei Änderung    | Ereignis | 4, 6            | belegt      |
| `0x44` |      1 | unidentifiziert, 136 – 141       |  3,5 s | siehe unten       | offen       |
| `0x25` |      1 | konstant 0x36                    |   60 s | 54                | offen       |
| `0x09` |     16 | leerer Namensslot                |   60 s | `00` + 15 Leerzeichen | belegt |

`0x28` trägt die Betriebsartcodes aus dem Tabellenblatt `Lüfterstufen`:
1 – 3 Stufe, 4 Stoßlüftung, 5 Abwesend, 6 Automatik. `0x27` und `0x29` laufen
parallel dazu und standen in allen Dumps auf 6.

`0x1d` steht im Normalbetrieb auf 1, fällt beim geordneten Abschalten auf 0 und
meldet in den ersten Sekunden nach dem Start 3. `0x08` meldet 0 unmittelbar vor
dem Abschalten und 1, dann 3 beim Hochlauf.

### Zähler, 5 Byte

| Reg    | Bedeutung                     | 20.06.2026    | 11.09.2026    | Lauf              |
| ------ | ----------------------------- | ------------- | ------------- | ----------------- |
| `0x26` | Betriebszeit gesamt           | 3 J 342 T 21:04 | 4 J 58 T 06:06 | +1 min je Laufminute |
| `0x85` | zweiter Zähler, fest 20 T 13:37 hinter `0x26` | 3 J 322 T 07:27 | 4 J 37 T 16:29 | wie oben |
| `0x3e` | Restzeit bis Filterwechsel    | 145 T 17:34   | 63 T 08:34    | −1 Tag je Tag     |
| `0x3d` | Zähler, nur im Startabzug     | 160 T 00:00   | 160 T 00:00   | —                 |
| `0x3f` | Zähler, nur im Startabzug     | 11 T 00:00    | 11 T 00:00    | —                 |

Das Filterintervall steht in `0x3a` = 100 Tage und deckt sich mit der Spanne,
über die `0x3e` herunterzählt. Beide Betriebszähler laufen nur, solange das
Gerät Strom hat; die Differenz zur Wanduhr ist also aufsummierte Stillstandszeit.

### Identität, nur beim Startscan

| Reg             | Breite | Wert                    | Bedeutung                     |
| --------------- | -----: | ----------------------- | ----------------------------- |
| `0x0d`          |     11 | `ETA0036E31E`           | Artikelnummer Master          |
| `0x19`          |     11 | `SWZ0025B27A`           | Artikelnummer                 |
| `0x1c`          |     11 | `ST00064E26E`           | Artikelnummer                 |
| `0x88`          |     16 | `c2` + `Fan controller` | Typbyte und Gerätename        |
| `0x8b`          |     16 | `c7` + `Touch TFT 1`    | Typbyte und Gerätename        |
| `0x89`, `0x8c`  |      4 | `06 08 0a 17`           | auf beiden Geräten gleich, vermutlich Firmwarestempel |
| `0x8a`, `0x8d`  |      3 | `09 2f 0c`, `24 2e 0c`  | je Gerät verschieden          |

### Einstellungen, nur im Startabzug

Über alle zwölf Dumps hinweg unverändert.

| Reg           | Wert                            | Bedeutung                                              |
| ------------- | ------------------------------- | ------------------------------------------------------ |
| `0x2d` – `0x30` | 17, 17, 47, 85                | Lüfterstufen in %                                      |
| `0x31` – `0x33` | −5, −5, −5                    | Debalancing je Stufe                                   |
| `0x34` – `0x3a` | 17, 25, 38, 47, 56, 64, 100   | Siebenstufenleiter des LED-Bedienteils                 |
| `0x4d` – `0x50` | 17, 17, 100, 100              | 0–10 V: Umin 1,7 V, n_min 17 %, Umax 10,0 V, n_max 100 % |
| `0x51` – `0x54` | 50, 20, 190, 80               | 4–20 mA: Imin, n_min, Imax, n_max                      |
| `0x4a` – `0x4c` | −2,0, −3,0, −3,0 °C           | Frostschutzschwellen                                   |
| `0x56`, `0x60`  | 22,0 °C                       | obere Bypasstemperatur                                 |
| `0x64` – `0x78` | 21 Register à 8 Byte          | Wochenzeitprogramm                                     |

Aus `0x4d` – `0x50` folgt die Kennlinie des Analogeingangs:
`n% = 17 + (U − 1,7) · (100 − 17) / (10 − 1,7)`, und weil `83 / 8,3 = 10` ist,
vereinfacht sich das zu `n% = 10 · U`. Der KNX-Stellwert in Prozent landet also
eins zu eins als Lüfter-Sollwert im Gerät — genau das misst `0x55`.

### Wochenzeitprogramm

21 Register à 8 Byte sind 7 Tage × 24 h mit 2 Bit je Viertelstunde. Drei
Register decken einen Tag ab: 00:00–08:00, 08:00–16:00, 16:00–24:00.

```
Mo–Fr   aa aa aa aa aa aa aa aa · 5f 55 55 55 55 55 55 55 · aa aa aa aa aa aa aa aa
Sa–So   aa aa aa aa aa aa aa aa · aa aa aa aa aa aa aa aa · aa aa aa aa aa aa aa aa
```

`0xaa` ist `10` wiederholt, also Stufe 2; `0x55` ist `01`, also Stufe 1. Das
hinterlegte Programm fährt werktags tagsüber Stufe 1 und am Wochenende
durchgehend Stufe 2, mit einer Stunde Stufe 3 gegen 08:00 (das `0x5f`).

Aktiv ist es nicht: das Gerät hängt am 0–10-V-Eingang, und `0x55` folgt dem
KNX-Stellwert auch dann, wenn das Programm etwas anderes vorsähe.

## Wie die Bedeutungen belegt sind

**Die vier Temperaturen** über die Energiebilanz, unabhängig von jeder
Beschriftung. Am 05.03.2026: außen 1,5 °C, Abluft 25,5 °C, Zuluft 21,0 °C,
Fortluft 5,5 °C. Rückgewinnung zuluftseitig (21,0 − 1,5)/(25,5 − 1,5) = 81 %,
fortluftseitig (25,5 − 5,5)/(25,5 − 1,5) = 83 %. Am 06.08.2026 ergibt dieselbe
Rechnung 85 % und 84 %. Nur diese Zuordnung liefert eine plausible,
ausgeglichene Gegenstrombilanz.

**Der Lüfter-Sollwert `0x55`** über den KNX-Stellwert. Am 14.08.2026 fuhr die
CO₂-Regelung den Analogaktor von 25 % auf 70 % und zurück:

| Uhrzeit | `knx_Lueftung_Stellwert` | `shelly_power_w` | `0x55` | `0x44` |
| ------- | -----------------------: | ---------------: | -----: | -----: |
| 14:14   |                   25,1 % |          15,6 W  | 24,6 % |    138 |
| 14:29   |                   40,4 % |          30,4 W  | 43,7 % |    138 |
| 14:39   |                   60,0 % |          69,1 W  | 61,3 % |    138 |
| 14:49   |                   70,2 % |          98,3 W  | 70,0 % |    138 |
| 15:19   |                   39,2 % |          29,2 W  | 35,3 % |    137 |
| 15:39   |                   29,4 % |          15,6 W  | 29,1 % |    137 |

Am 09.07.2026 wiederholt sich das mit einem Ausschlag auf 45,5 %, wo `0x55` auf
458 geht. `0x44` bleibt in beiden Fällen unbewegt — es ist damit weder
Volumenstrom noch Drehzahl.

**Die Betriebsart `0x28`** am 20.07.2026: um 08:25:11 meldet sie 4, um 08:54:19
wieder 6 — jemand hat am Bedienteil Stoßlüftung gedrückt und das Gerät ist nach
29 Minuten in den Automatikbetrieb zurückgefallen. Der Lüfter-Sollwert `0x55`
blieb in diesen 29 Minuten unverändert auf 24,7 %, und die Leistungsaufnahme
ebenso. Die am Bedienteil gewählte Betriebsart hat in dieser Installation also
keine Wirkung: der 0–10-V-Eingang übersteuert sie.

Für „wann war Stoßlüftung aktiv" heißt das: `climos_operating_mode` zeigt, was
am Bedienteil gewählt wurde, `climos_fan_setpoint_percent` zeigt, was die
Lüftung tatsächlich getan hat. Eine über KNX gefahrene Stoßlüftung ist ein
kurzer Ausschlag von `0x55` nach oben, eine Abwesenheitsschaltung ein
anhaltend niedriger Wert; die Betriebsart bleibt dabei auf 6.

**Die Quittungen `0x87`** dadurch, dass ihre drei Nutzbytes `00` plus die
CRC-Bytes des unmittelbar davor gesendeten Datenframes sind.

## Vorheizregister

Es ist verbaut, es lief in den aufgezeichneten Tagen nur nie. In der
Leistungsaufnahme ist es unübersehbar: vom 04. bis 08. und vom 20. bis 24.
Januar 2026 steigt `shelly_power_w{instance="Lüftung"}` vom Grundlastniveau
17 W auf Stundenmittel um 250 W, mit einer Spitze von 271 W am 05.01. Der
Lüfter allein erreicht selbst bei 70 % Sollwert nur rund 98 W.

Der Auslöser ist die Außentemperatur. An diesen Januartagen lag das
Tagesminimum zwischen −2 °C und −4 °C, und die Frostschutzschwellen der
Konfiguration stehen auf `0x4a` = −2,0 °C sowie `0x4b` und `0x4c` = −3,0 °C.

Die Dumps beginnen am 21.02.2026, und ab da fiel das Tagesminimum nie unter
−1 °C; die höchste Leistungsaufnahme zwischen Februar und April war 41 W und
erklärt sich vollständig aus dem Lüfter. Deshalb taucht der Vorheizer in keinem
Dump auf, und deshalb steht in der Registerkarte kein Register dafür.

Wenn er das nächste Mal anspringt, meldet er sich vermutlich über ein Register,
das hier noch nicht steht. Der Decoder bricht beim ersten unbekannten Register
ab und verliert den Rest des Frames, zählt das aber mit:

```promql
sum by (register) (increase(climos_unknown_registers_total[1h])) > 0
```

Die fehlende Breite lässt sich anschließend aus dem nächsten Registerabzug
nach einem Neustart ableiten, weil die IDs dort aufsteigen und die Paketlänge
jede Breite festlegt.

## Neustarts

Ein Neustart ist am Adressscan über `0x0102` … `0x010c` erkennbar, auf den der
vollständige Registerabzug folgt. Zwei Arten kommen vor:

- **Geordnetes Abschalten.** `0x08` geht auf 0, dann meldet `0x1d` viermal über
  sechs Sekunden 0, danach Stille. So war es am 05.08.2026: das Gerät lief ab
  Dumpbeginn 7 h 40, stand dann von 09:39 bis 19:53 still und lief danach
  weiter. 13 h 46 Laufzeit plus 10 h 14 Stillstand ergeben genau 24 h.
- **Spannungseinbruch.** Am 06.08.2026 gegen 00:15 lief der Verkehr noch 1,6 s
  vorher normal, es gibt keine Abschaltsequenz, `0x55` steht im Startabzug auf
  80, und es folgen zwei vollständige Adressscans im Abstand von sieben
  Sekunden.

Die Dumps laufen jeweils von 01:59 bis 01:59 und schließen lückenlos aneinander
an. Da kein Zeitstempel im Strom steht, datiert man einen Dump über `0x3e`:
der Zähler tickt je Laufminute einmal herunter.

## Offene Punkte

- **`0x44`.** Liegt bei 136 – 141, im Winter etwa zwei Einheiten höher als im
  Sommer, ist innerhalb eines Tages exakt konstant, reagiert nicht auf den
  Lüfter-Sollwert und steigt nach dem Einschalten in rund 135 Sekunden linear
  von 10 auf den Endwert. Das Rampenverhalten spricht für einen Stellwert, die
  Unabhängigkeit vom Sollwert gegen alles Luftbezogene. Bis das geklärt ist,
  wird es als `climos_register{register="0x44"}` roh exportiert.
- **`0x85`.** Läuft exakt parallel zu `0x26`, 20 T 13:37 dahinter.
  Lüfterstunden gegen Gerätestunden würde den Versatz erklären, belegt ist es
  nicht.
- **Bitreihenfolge im Zeitprogramm**, davon hängt ab, ob die Stufe-3-Stunde von
  08:00 bis 08:30 oder von 08:30 bis 09:00 läuft.
- **`0x64` – `0x78` als Zeitprogramm** ruht auf der Byteanzahl und der
  Aufteilung Werktag gegen Wochenende. Ein geänderter Slot am Bedienteil und
  ein neuer Dump würden es in einer Minute bestätigen.
- **Welches Register das Vorheizregister meldet.** Dafür braucht es einen Dump
  von einem Tag unter −2 °C; der erste solche Tag nach dem 21.02.2026 steht
  noch aus.
- **Der Lesepuffer in `reader.go`** wächst unbegrenzt, solange sich kein Frame
  extrahieren lässt. In den Dumps tritt das nicht auf, weil bei abgeschaltetem
  Gerät gar keine Bytes ankommen.
