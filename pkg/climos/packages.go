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

// Package is a small structure that represents a message that was delivered from the ventilation system.
type Package struct {
	TargetAddress Address
	Command       Command
	valid         bool
	Header        []byte
	Payload       []byte
	Data          []byte
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
		return "Status(0x" + command + ")"
	case BroadcastRequest:
		return "BroadcastRequest(0x" + command + ")"
	case BroadcastAnswer:
		return "BroadcastAnswer(0x" + command + ")"
	case Alive:
		return "Alive(0x" + command + ")"
	case GetSet:
		return "GetSet(0x" + command + ")"
	case Ask:
		return "Ask(0x" + command + ")"
	case Other:
		return "Other(0x" + command + ")"
	default:
		return "Unknown(0x" + command + ")"
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

func newPackage(data []byte) *Package {
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
	}
}

func extractPackage(data []byte) ([]byte, uint, error) {
	dataLength := uint(len(data))
	if dataLength < 6 {
		return []byte{}, 0, ErrorToShort
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
			return possiblePackage, expectedEnd - 1, nil
		}
	}
	return []byte{}, 0, ErrorToShort
}

func expectedDataLength(data []byte) uint {
	if data[3] >= 0x80 {
		return uint(data[3] - 0x80)
	}
	return uint(data[3])
}
