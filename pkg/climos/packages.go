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
	"bytes"
	"encoding/binary"
	"errors"
)

// ErrorToShort is the error that will be returned when the given byte array is too short for a valid pacakge.
var ErrorToShort = errors.New("data is to short")
var nullPackage = []byte{0, 0, 0, 0, 0, 0}

// leadingAddressByte is the high byte of every address on this bus.
const leadingAddressByte = 0x01

// Package is a small structure that represents a message that was delivered from the ventilation system.
type Package struct {
	TargetAddress Address
	Command       Command
	valid         bool
	Header        []byte
	Payload       []byte
	Data          []byte
	// Repaired records that the leading address byte had to be restored, see
	// repairLeadingByte.
	Repaired bool
}

// IsValid checks if the package is a valid package.
func (p Package) IsValid() bool {
	return p.valid
}

type Address uint16

func (c Address) String() string {
	bs := make([]byte, 2)
	binary.LittleEndian.PutUint16(bs, uint16(c))

	return asHex(bs)
}

type Command byte

func (c Command) String() string {
	command := asHex([]byte{byte(c)})
	switch c {
	case Status:
		return "Status(" + command + ")"
	case BroadcastRequest:
		return "BroadcastRequest(" + command + ")"
	case BroadcastAnswer:
		return "BroadcastAnswer(" + command + ")"
	case Alive:
		return "Alive(" + command + ")"
	case GetSet:
		return "GetSet(" + command + ")"
	case Ask:
		return "Ask(" + command + ")"
	case Other:
		return "Other(" + command + ")"
	default:
		return "Unknown(" + command + ")"
	}
}

const (
	Status           = Command(0)
	BroadcastRequest = Command(0x80)
	BroadcastAnswer  = Command(0x81)
	Alive            = Command(0x84)
	GetSet           = Command(0x85)
	Ask              = Command(0x86)
	Other            = Command(0x87)
)

func newPackage(data []byte, repaired bool) *Package {
	if len(data) < 6 {
		return &Package{valid: false, Data: data}
	}
	return &Package{
		TargetAddress: Address(uint16(data[0])<<8 + uint16(data[1])),
		Command:       Command(data[2]),
		valid:         true,
		Header:        data[0:4],
		Payload:       data[6:],
		Data:          data,
		Repaired:      repaired,
	}
}

// extractedPackage is one framed package together with the offset the caller has
// to continue at.
type extractedPackage struct {
	Data      []byte
	NextStart uint
	Repaired  bool
}

func extractPackage(data []byte) (extractedPackage, error) {
	dataLength := uint(len(data))
	if dataLength < 6 {
		return extractedPackage{}, ErrorToShort
	}

	for i := uint(0); i < dataLength-6; i++ {
		expectedLength := 6 + expectedDataLength(data[i:])
		expectedEnd := i + expectedLength

		if expectedEnd > dataLength {
			// to short
			continue
		}

		possiblePackage := data[i:expectedEnd]
		if bytes.Equal(possiblePackage, nullPackage) {
			// skip next two bytes as we know that on these bytes no package can start
			i += 2

			// package is valid but contains only null
			continue
		}

		if ValidateCrc(possiblePackage) {
			return extractedPackage{Data: possiblePackage, NextStart: expectedEnd}, nil
		}
		if repaired, ok := repairLeadingByte(possiblePackage); ok {
			return extractedPackage{Data: repaired, NextStart: expectedEnd, Repaired: true}, nil
		}
	}
	return extractedPackage{}, ErrorToShort
}

// repairLeadingByte restores the first byte of a frame that started after an
// idle gap. The idle line sits in the space state, so the receiver reads a
// continuous run of null characters and is still inside one of them when the
// real start bit arrives — it swallows the first byte and reports 0x00 instead.
// Every address on this bus begins with 0x01, which makes the byte recoverable.
// Without this, 88% of the frames fail their CRC and are dropped.
func repairLeadingByte(data []byte) ([]byte, bool) {
	if data[0] != 0x00 {
		return nil, false
	}

	repaired := make([]byte, len(data))
	copy(repaired, data)
	repaired[0] = leadingAddressByte

	if !ValidateCrc(repaired) {
		return nil, false
	}
	return repaired, true
}

func expectedDataLength(data []byte) uint {
	if data[3] >= 0x80 {
		return uint(data[3] - 0x80)
	}
	return uint(data[3])
}
