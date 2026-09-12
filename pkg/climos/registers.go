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
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Register identifies one value in the ventilation unit's register space.
type Register uint16

func (r Register) String() string {
	return fmt.Sprintf("0x%02x", uint16(r))
}

// The registers whose meaning is established. docs/protocol.md records how each
// one was pinned down and lists the ones still unidentified.
const (
	RegLifecycle            = Register(0x08)
	RegArticleMaster        = Register(0x0d)
	RegError                = Register(0x0e)
	RegArticleFan           = Register(0x19)
	RegStatusWord           = Register(0x1a)
	RegArticlePanel         = Register(0x1c)
	RegRunState             = Register(0x1d)
	RegOperatingTime        = Register(0x26)
	RegOperatingMode        = Register(0x28)
	RegFilterInterval       = Register(0x3a)
	RegFilterRemaining      = Register(0x3e)
	RegFanSetpoint          = Register(0x55)
	RegTemperatureIndoorIn  = Register(0x81)
	RegTemperatureOutside   = Register(0x82)
	RegTemperatureIndoorOut = Register(0x83)
	RegTemperatureHouseOut  = Register(0x84)
	RegOperatingTimeSecond  = Register(0x85)
	RegNameFanController    = Register(0x88)
	RegNamePanel            = Register(0x8b)
)

// The widths that carry something other than a plain integer.
const (
	counterWidth = 5
	articleWidth = 11
	nameWidth    = 16
)

// daysPerFirmwareYear is how the firmware rolls the day field of a counter over
// into the year field. Verified against the rollover between the 2026-06-20 and
// 2026-07-20 dumps.
const daysPerFirmwareYear = 365

var (
	// ErrUnknownRegister reports a register id that is not in registerWidths.
	// Its width is unknown, so everything behind it in the payload is lost too.
	ErrUnknownRegister = errors.New("unknown register")
	// ErrTruncatedPayload reports a payload that ends inside a register record.
	ErrTruncatedPayload = errors.New("truncated payload")
)

// UnknownRegisterError names the register that stopped a payload walk. A device
// that only speaks under conditions the recordings never hit — the preheater
// needs outdoor temperatures below the frost protection threshold — shows up
// first as this error, so it carries the id rather than only a message.
type UnknownRegisterError struct {
	Register Register
	Offset   int
}

func (e UnknownRegisterError) Error() string {
	return fmt.Sprintf("%s %s at offset %d", ErrUnknownRegister, e.Register, e.Offset)
}

func (e UnknownRegisterError) Unwrap() error {
	return ErrUnknownRegister
}

// registerWidths gives the byte width of every known register value. The widths
// are not on the wire: they come from the register dump the master emits after
// a bus restart, where the ids ascend so the packet length pins each one down.
var registerWidths = map[Register]int{
	0x00: 3, 0x01: 1, 0x02: 1, 0x08: 1, 0x09: 16,
	0x0d: 11, 0x0e: 1, 0x0f: 1, 0x12: 1, 0x15: 1, 0x18: 1,
	0x19: 11, 0x1a: 1, 0x1b: 2, 0x1c: 11, 0x1d: 1, 0x1e: 1, 0x21: 1,
	0x24: 1, 0x25: 1, 0x26: 5, 0x27: 1, 0x28: 1, 0x29: 1, 0x2a: 1, 0x2b: 1,
	0x2c: 1, 0x2d: 1, 0x2e: 1, 0x2f: 1,
	0x30: 1, 0x31: 1, 0x32: 1, 0x33: 1, 0x34: 1, 0x35: 1, 0x36: 1,
	0x37: 1, 0x38: 1, 0x39: 1, 0x3a: 1, 0x3b: 1, 0x3c: 1,
	0x3d: 5, 0x3e: 5, 0x3f: 5,
	0x40: 1, 0x41: 2, 0x42: 2, 0x43: 1, 0x44: 1, 0x45: 1, 0x46: 1, 0x47: 2,
	0x48: 1, 0x49: 2, 0x4a: 2, 0x4b: 2, 0x4c: 2, 0x4d: 1, 0x4e: 1, 0x4f: 1,
	0x50: 1, 0x51: 1, 0x52: 1, 0x53: 1, 0x54: 1, 0x55: 2, 0x56: 2,
	0x57: 1, 0x58: 1, 0x59: 1, 0x5a: 2, 0x5b: 2, 0x5c: 1, 0x5d: 1, 0x5e: 1,
	0x5f: 2, 0x60: 2, 0x61: 1, 0x63: 2,
	0x64: 8, 0x65: 8, 0x66: 8, 0x67: 8, 0x68: 8, 0x69: 8, 0x6a: 8, 0x6b: 8,
	0x6c: 8, 0x6d: 8, 0x6e: 8, 0x6f: 8, 0x70: 8, 0x71: 8, 0x72: 8, 0x73: 8,
	0x74: 8, 0x75: 8, 0x76: 8, 0x77: 8, 0x78: 8,
	0x79: 1, 0x7a: 1, 0x7b: 1, 0x7c: 1, 0x7d: 1, 0x7e: 1, 0x7f: 1, 0x80: 1,
	0x81: 2, 0x82: 2, 0x83: 2, 0x84: 2, 0x85: 5, 0x86: 1, 0x87: 1,
	0x88: 16, 0x89: 4, 0x8a: 3, 0x8b: 16, 0x8c: 4, 0x8d: 3, 0x8e: 1,
	0x91: 1, 0x94: 1,
}

// RegisterValue is one register record out of a package payload.
type RegisterValue struct {
	Register Register
	Raw      []byte
}

// decodeRegisters splits a payload into its register records. Records decoded
// before an unknown id are returned along with the error: a data frame usually
// carries the interesting values before the one that stops the walk.
func decodeRegisters(payload []byte) ([]RegisterValue, error) {
	values := make([]RegisterValue, 0, len(payload)/4)
	pos := 0
	for pos+2 <= len(payload) {
		register := Register(binary.LittleEndian.Uint16(payload[pos:]))
		width, known := registerWidths[register]
		if !known {
			return values, UnknownRegisterError{Register: register, Offset: pos}
		}
		if pos+2+width > len(payload) {
			return values, fmt.Errorf("%w: %s needs %d bytes at offset %d", ErrTruncatedPayload, register, width, pos)
		}
		values = append(values, RegisterValue{Register: register, Raw: payload[pos+2 : pos+2+width]})
		pos += 2 + width
	}
	if pos != len(payload) {
		return values, fmt.Errorf("%w: %d trailing bytes", ErrTruncatedPayload, len(payload)-pos)
	}
	return values, nil
}

// IsNumeric reports whether Int carries a meaningful value. The wider registers
// hold counters, text or bit fields instead.
func (v RegisterValue) IsNumeric() bool {
	return len(v.Raw) >= 1 && len(v.Raw) <= 4
}

// Int reads the raw bytes as a signed little endian integer.
func (v RegisterValue) Int() int64 {
	if len(v.Raw) == 0 || len(v.Raw) > 8 {
		return 0
	}
	var value uint64
	for i := len(v.Raw) - 1; i >= 0; i-- {
		value = value<<8 | uint64(v.Raw[i])
	}
	shift := 64 - 8*len(v.Raw)
	return int64(value<<shift) >> shift
}

// Tenths reads a register that scales by ten, which covers both the
// temperatures in °C and the fan setpoint in percent.
func (v RegisterValue) Tenths() float64 {
	return float64(v.Int()) / 10
}

// Duration reads a five byte counter: minute, hour, days uint16, years.
func (v RegisterValue) Duration() time.Duration {
	if len(v.Raw) != counterWidth {
		return 0
	}
	days := int(binary.LittleEndian.Uint16(v.Raw[2:4])) + int(v.Raw[4])*daysPerFirmwareYear
	return time.Duration(days)*24*time.Hour +
		time.Duration(v.Raw[1])*time.Hour +
		time.Duration(v.Raw[0])*time.Minute
}

// Text reads an ASCII register. The sixteen byte names carry a device type byte
// in front of the name itself.
func (v RegisterValue) Text() string {
	switch len(v.Raw) {
	case articleWidth:
		return strings.TrimSpace(string(v.Raw))
	case nameWidth:
		return strings.TrimSpace(string(v.Raw[1:]))
	default:
		return ""
	}
}
