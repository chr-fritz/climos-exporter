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

var ErrorToShort = errors.New("data is to short")
var nullPackage = []byte{0, 0, 0, 0, 0, 0}

type Package struct {
    TargetAddress Address
    Command       Command
    Valid         bool
    Header        []byte
    Payload       []byte
    Data          []byte
}

type Address uint16

func (c Address) String() string {
    bs := make([]byte, 2)
    binary.LittleEndian.PutUint16(bs, uint16(c))

    return asHex(bs)
}

type Command byte

func (c Command) String() string {
    return asHex([]byte{byte(c)})
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
        return &Package{Valid: false, Data: data}
    }
    return &Package{
        TargetAddress: Address(uint16(data[0])<<8 + uint16(data[1])),
        Command:       Command(data[2]),
        Valid:         true,
        Header:        data[0:4],
        Payload:       data[6:],
        Data:          data,
    }
}

func extractPackage(data []byte) ([]byte, int, error) {
    dataLength := len(data)
    if dataLength < 6 {
        return []byte{}, 0, ErrorToShort
    }

    for i := 0; i < dataLength-6; i++ {
        expectedLength := 6 + expectedDataLength(data[i:])
        expectedEnd := i + expectedLength

        if expectedEnd < i {
            // invalid length
            continue
        }

        if expectedEnd > dataLength {
            // to short
            continue
        }

        possiblePackage := data[i:expectedEnd]
        if bytes.Equal(possiblePackage, nullPackage) {
            // package is valid but contains only null
            continue
        }

        if ValidateCrc(possiblePackage) {
            return possiblePackage, expectedEnd - 1, nil
        }
    }
    return []byte{}, 0, ErrorToShort
}

func expectedDataLength(data []byte) int {
    if data[3] >= 0x80 {
        return int(data[3] - 0x80)
    }
    return int(data[3])
}
