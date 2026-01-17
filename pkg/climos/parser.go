/*
 * Copyright © 2023 Christian Fritz <mail@chr-fritz.de>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package climos

import (
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"math"

	"github.com/yerden/go-util/bcd"
)

type SubCommand int

func (c SubCommand) String() string {
	bs := make([]byte, 4)
	binary.LittleEndian.PutUint32(bs, uint32(c))

	command := asHex(bs)
	switch c {
	case SetLanguage:
		return "set language(0x" + command + ")"
	case SetFanSpeed:
		return "set fan speed(0x" + command + ")"
	case TimeLeftToFilterReplacement:
		return "filter replacement(0x" + command + ")"
	case Bypass:
		return "bypass(0x" + command + ")"
	case OperatingHours:
		return "operating hours(0x" + command + ")"
	case Temperatures:
		return "temperatures(0x" + command + ")"
	default:
		return "unknown(0x" + command + ")"
	}
}

const (
	SetLanguage                 = SubCommand(0x06)
	SetFanSpeed                 = SubCommand(0x08)
	TimeLeftToFilterReplacement = SubCommand(0x09)
	Bypass                      = SubCommand(0x1a)
	OperatingHours              = SubCommand(0x26)
	Temperatures                = SubCommand(0x44)
)

type ParsedPackage interface {
	GetPackageType() string
	String() string
}

func ParsePackage(p *Package) (ParsedPackage, error) {
	if !p.IsValid() {
		return nil, fmt.Errorf("package is invalid")
	}

	switch p.Command {
	case Status:
		return parseOtherCommand(p)
	case BroadcastRequest:
		return parseOtherCommand(p)
	case BroadcastAnswer:
		return parseOtherCommand(p)
	case Alive:
		return parseOtherCommand(p)
	case GetSet:
		return parseGetSetCommand(p)
	case Ask:
		return parseOtherCommand(p)
	case Other:
		return parseOtherCommand(p)
	default:
		slog.With(
			"command", p.Command,
			"address", p.TargetAddress,
			"payload", asHex(p.Payload),
		).
			Debug("Got unknown command")
		return nil, fmt.Errorf("unknown command %x", p.Command)
	}
}

func parseGetSetCommand(p *Package) (ParsedPackage, error) {
	subCmd := SubCommand(p.Payload[0])
	logger := slog.With(
		"command", p.Command,
		"subCommand", subCmd,
		"address", p.TargetAddress,
		"payload", asHex(p.Payload),
	)
	switch subCmd {
	case Temperatures:
		return parseTemperatures(p)
	case TimeLeftToFilterReplacement:
		logger.With(
			"hex-value", asHex(p.Payload[23:27]),
			"value", binary.BigEndian.Uint32(p.Payload[23:27]),
		).Info("Got message with reaming time for filters")

		return nil, fmt.Errorf("missing impl for filter time")
	default:
		decoder := bcd.NewDecoder(bcd.Standard)
		bytes := make([]byte, 2*len(p.Payload))
		n, _ := decoder.Decode(bytes, p.Payload)

		slog.With(
			"command", p.Command,
			"address", p.TargetAddress,
			"payload", asHex(p.Payload),
			"data", asHex(p.Data),
			"payload-as-bcd", string(bytes[:n]),
		).
			Debug("Got unknown command")
		return nil, fmt.Errorf("unknown sub command %x of command 0x85", subCmd)
	}
}

type TemperaturePackage struct {
	IndoorInTemperature  float64
	OutsideTemperature   float64
	IndoorOutTemperature float64
	HouseOutTemperature  float64
}

func (t *TemperaturePackage) GetPackageType() string {
	return "temperature"
}

func (t *TemperaturePackage) String() string {
	return fmt.Sprintf("Got temperatures:\n"+
		"\tOutside:\t%5.2f"+
		"\tIndoor In:\t%5.2f"+
		"\tIndoor Out:\t%5.2f"+
		"\tHouse Out:\t%5.2f",
		t.OutsideTemperature,
		t.IndoorInTemperature,
		t.IndoorOutTemperature,
		t.HouseOutTemperature)
}

var noTemperatures = &TemperaturePackage{
	math.NaN(),
	math.NaN(),
	math.NaN(),
	math.NaN(),
}

func parseTemperatures(p *Package) (ParsedPackage, error) {
	t := &TemperaturePackage{
		extractTemperature(p.Data[9:13]),
		extractTemperature(p.Data[13:17]),
		extractTemperature(p.Data[17:21]),
		extractTemperature(p.Data[21:25]),
	}

	logger := slog.With(
		"outside", t.OutsideTemperature,
		"indoor_in", t.IndoorInTemperature,
		"indoor_out", t.IndoorOutTemperature,
		"house_out", t.HouseOutTemperature,
	)
	if logger.Enabled(context.Background(), slog.LevelDebug) {
		logger = logger.With(
			"command", p.Command,
			"address", p.TargetAddress,
			"payload", asHex(p.Payload),
			"data", asHex(p.Data),
		)
	}
	logger.Info("got temperatures")
	return t, nil
}

func extractTemperature(b []byte) float64 {
	// Implement extractTemp function here
	temp := float64(b[2]) / 10.0
	if b[3] > 128 {
		temp -= float64(256-int(b[3])) * 25.6
	} else {
		temp += float64(int(b[3])) * 25.6
	}
	return temp
}

func parseOtherCommand(p *Package) (ParsedPackage, error) {
	slog.With(
		"command", p.Command,
		"address", p.TargetAddress,
		"payload", asHex(p.Payload),
		"data", asHex(p.Data),
	).
		Debug("Got known but not implemented command")
	return nil, fmt.Errorf("unknown command")
}

func asHex(data []byte) string {
	// Implement asHex function here
	return fmt.Sprintf("%x", data)
}
