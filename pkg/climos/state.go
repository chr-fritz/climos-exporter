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
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// persistedState is the on-disk form of the register values. A value is kept as
// the raw wire bytes in hex rather than as a number, because its width is what
// decides how the register is read and a number would throw that away.
type persistedState struct {
	SavedAt   time.Time         `json:"savedAt"`
	Registers map[string]string `json:"registers"`
}

// LoadRegisters reads back what a previous run last saw, and reports when that
// was. Most registers reach the bus often enough to be replaced within seconds
// of a start, but a setting appears only when it changes or in the dump after a
// bus restart — without this it is missing from the metrics until one of those
// happens, which can be weeks.
//
// A missing file is the first start and not an error.
func LoadRegisters(path string) ([]RegisterValue, time.Time, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, time.Time{}, nil
	}
	if err != nil {
		return nil, time.Time{}, err
	}

	var state persistedState
	if err := json.Unmarshal(raw, &state); err != nil {
		return nil, time.Time{}, fmt.Errorf("%s: %w", path, err)
	}

	values := make([]RegisterValue, 0, len(state.Registers))
	for id, encoded := range state.Registers {
		value, err := decodeStoredRegister(id, encoded)
		if err != nil {
			return nil, state.SavedAt, fmt.Errorf("%s: %w", path, err)
		}
		values = append(values, value)
	}

	// Ascending order so the identity registers land in the same sequence the
	// restart dump delivers them, which is what updateDeviceInfo expects.
	sort.Slice(values, func(i, j int) bool { return values[i].Register < values[j].Register })
	return values, state.SavedAt, nil
}

// decodeStoredRegister rejects anything the current build cannot place. A file
// written by an older build may name a register that has since been given a
// different width, and restoring that would publish a value this build would
// never have read.
func decodeStoredRegister(id, encoded string) (RegisterValue, error) {
	number, err := strconv.ParseUint(strings.TrimPrefix(id, "0x"), 16, 16)
	if err != nil {
		return RegisterValue{}, fmt.Errorf("%q is not a register id: %w", id, err)
	}
	register := Register(number)

	raw, err := hex.DecodeString(encoded)
	if err != nil {
		return RegisterValue{}, fmt.Errorf("%s: %q is not hexadecimal: %w", register, encoded, err)
	}

	width, known := registerWidths[register]
	if !known {
		return RegisterValue{}, fmt.Errorf("%w: %s", ErrUnknownRegister, register)
	}
	if len(raw) != width {
		return RegisterValue{}, fmt.Errorf("%s holds %d bytes, expected %d", register, len(raw), width)
	}
	return RegisterValue{Register: register, Raw: raw}, nil
}

// SaveRegisters writes the values through a temporary file in the same
// directory and renames it into place, so an interrupted write leaves the
// previous state readable instead of half of a new one.
func SaveRegisters(path string, values map[Register]RegisterValue) error {
	state := persistedState{SavedAt: time.Now(), Registers: make(map[string]string, len(values))}
	for register, value := range values {
		state.Registers[register.String()] = hex.EncodeToString(value.Raw)
	}

	encoded, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, append(encoded, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}
