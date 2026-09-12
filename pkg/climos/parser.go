/*
 * Copyright © 2023-2026 Christian Fritz <mail@chr-fritz.de>
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
	"errors"
	"fmt"
	"log/slog"
	"strings"
)

// ErrNoRegisterData reports a package whose command carries no register records.
var ErrNoRegisterData = errors.New("package carries no register data")

type ParsedPackage interface {
	GetPackageType() string
	String() string
}

// DataPackage holds the register values that one data frame carried.
type DataPackage struct {
	Values []RegisterValue
}

func (d *DataPackage) GetPackageType() string {
	return "data"
}

func (d *DataPackage) String() string {
	parts := make([]string, 0, len(d.Values))
	for _, value := range d.Values {
		parts = append(parts, fmt.Sprintf("%s=%s", value.Register, asHex(value.Raw)))
	}
	return "Got registers: " + strings.Join(parts, " ")
}

// ParsePackage decodes a package's payload. A payload that stops at an unknown
// register yields both the records decoded before it and the error, so a caller
// can keep those values and still report the gap.
func ParsePackage(ctx context.Context, p *Package) (ParsedPackage, error) {
	if !p.IsValid() {
		return nil, fmt.Errorf("package is invalid")
	}

	switch p.Command {
	case BroadcastRequest, BroadcastAnswer, GetSet:
		return parseDataCommand(ctx, p)
	default:
		logCommandWithoutData(ctx, p)
		return nil, fmt.Errorf("%w: %s", ErrNoRegisterData, p.Command)
	}
}

// parseDataCommand keeps the records decoded before a failure: an unknown
// register at the end of a payload must not cost us the values in front of it.
func parseDataCommand(ctx context.Context, p *Package) (ParsedPackage, error) {
	values, err := decodeRegisters(p.Payload)
	if err != nil {
		slog.With(
			"command", p.Command.String(),
			"address", p.TargetAddress.String(),
			"payload", asHex(p.Payload),
			"error", err,
		).DebugContext(ctx, "Could not decode the whole payload")
	}

	if len(values) == 0 {
		if err == nil {
			err = fmt.Errorf("%w: empty payload", ErrNoRegisterData)
		}
		return nil, err
	}
	return &DataPackage{Values: values}, err
}

// logCommandWithoutData builds its attributes only when they will be written:
// the polls it reports are by far the most frequent frames on the bus.
func logCommandWithoutData(ctx context.Context, p *Package) {
	if !slog.Default().Enabled(ctx, slog.LevelDebug) {
		return
	}
	slog.With(
		"command", p.Command.String(),
		"address", p.TargetAddress.String(),
		"payload", asHex(p.Payload),
		"data", asHex(p.Data),
	).DebugContext(ctx, "Got command without register data")
}

func asHex(data []byte) string {
	return fmt.Sprintf("0x%x", data)
}
