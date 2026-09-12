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
	"encoding/hex"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_extractPackage(t *testing.T) {
	tests := []struct {
		name         string
		data         []byte
		want         []byte
		wantNext     uint
		wantRepaired bool
		wantErr      bool
	}{
		{
			"at beginning",
			[]byte{0x00, 0x00, 0x00, 0x0e, 0x7b, 0x01, 0x01, 0x00, 0x05, 0x17, 0x06, 0x12, 0x02, 0x00, 0x37, 0x25, 0x13, 0x04, 0x00, 0x00, 0, 0},
			[]byte{0x00, 0x00, 0x00, 0x0e, 0x7b, 0x01, 0x01, 0x00, 0x05, 0x17, 0x06, 0x12, 0x02, 0x00, 0x37, 0x25, 0x13, 0x04, 0x00, 0x00},
			20, false, false,
		},

		{
			"to short",
			[]byte{0x00, 0x00, 0x00, 0x0e, 0x7b, 0x01, 0x01, 0x00, 0x05, 0x17, 0x06, 0x12, 0x02, 0x00, 0x37, 0x25, 0x13, 0x04},
			[]byte{},
			0, false, true,
		},
		{
			"to short",
			[]byte{0x00, 0x00, 0x00, 0x0e, 0x7b},
			[]byte{},
			0, false, true,
		},
		{
			"length to great",
			[]byte{0x00, 0x00, 0x00, 0xff, 0x7b, 0x02, 0x02},
			[]byte{},
			0, false, true,
		},
		{
			"valid package in middle",
			[]byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x0e, 0x7b, 0x01, 0x01, 0x00, 0x05, 0x17, 0x06, 0x12, 0x02, 0x00, 0x37, 0x25, 0x13, 0x04, 0x00, 0x00, 0, 0},
			[]byte{0x00, 0x00, 0x00, 0x0e, 0x7b, 0x01, 0x01, 0x00, 0x05, 0x17, 0x06, 0x12, 0x02, 0x00, 0x37, 0x25, 0x13, 0x04, 0x00, 0x00},
			23, false, false,
		},
		{
			"valid package in middle",
			[]byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x0e, 0x01, 0x04, 0x84, 0x00, 0x28, 0x7d, 0x37, 0x25, 0x13, 0x04, 0x00, 0x00, 0, 0},
			[]byte{0x01, 0x04, 0x84, 0x00, 0x28, 0x7d},
			13, false, false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractPackage(tt.data)
			if (err != nil) != tt.wantErr {
				t.Errorf("extractPackage() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			if !reflect.DeepEqual(got.Data, tt.want) {
				t.Errorf("extractPackage() data = %v, want %v", got.Data, tt.want)
			}
			if got.NextStart != tt.wantNext {
				t.Errorf("extractPackage() nextStart = %v, want %v", got.NextStart, tt.wantNext)
			}
			if got.Repaired != tt.wantRepaired {
				t.Errorf("extractPackage() repaired = %v, want %v", got.Repaired, tt.wantRepaired)
			}
		})
	}
}

func TestAddress_String(t *testing.T) {
	tests := []struct {
		c    Address
		want string
	}{
		{Address(0), "0x0000"},
		{Address(0x8080), "0x8080"},
		{Address(0x8081), "0x8180"},
		{Address(0xf0f1), "0xf1f0"},
	}
	for _, tt := range tests {
		t.Run(asHex([]byte{byte(tt.c)}), func(t *testing.T) {
			if got := tt.c.String(); got != tt.want {
				t.Errorf("String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_newPackage(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want *Package
	}{
		{"invalid", []byte{}, &Package{valid: false, Data: []byte{}}},
		{
			"valid no payload",
			[]byte{0x01, 0x04, 0x84, 0x00, 0x28, 0x7d},
			&Package{
				valid:         true,
				TargetAddress: Address(0x0104),
				Command:       Command(0x84),
				Header:        []byte{0x01, 0x04, 0x84, 0x00},
				Payload:       []byte{},
				Data:          []byte{0x01, 0x04, 0x84, 0x00, 0x28, 0x7d},
			},
		},
		{
			"valid no payload",
			[]byte{0x01, 0x04, 0x84, 0x00, 0x28, 0x7d, 0xab},
			&Package{
				valid:         true,
				TargetAddress: Address(0x0104),
				Command:       Command(0x84),
				Header:        []byte{0x01, 0x04, 0x84, 0x00},
				Payload:       []byte{0xab},
				Data:          []byte{0x01, 0x04, 0x84, 0x00, 0x28, 0x7d, 0xab},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := newPackage(tt.data, false); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("newPackage() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Test_extractPackage_realCapture runs the framer over 90 bytes taken verbatim
// from dumps/2026-08-05.bin at offset 134. Six of the eight frames in there lost
// their leading address byte to the idle line; only the two that follow their
// predecessor without a gap arrive intact.
func Test_extractPackage_realCapture(t *testing.T) {
	const capture = "000000000000000085033b390e000000088400490801008500000000000000" +
		"098400793f0100850329331d0001000000000000000085033b390e00000008" +
		"8400490801008500000000000000098400793f0100850329331d0001"

	data := decodeHex(t, capture)
	var addresses []string
	repaired := 0

	for {
		extracted, err := extractPackage(data)
		if err != nil {
			break
		}
		p := newPackage(extracted.Data, extracted.Repaired)
		addresses = append(addresses, p.TargetAddress.String())
		if extracted.Repaired {
			repaired++
		}
		data = data[extracted.NextStart:]
	}

	require.Equal(t, []string{
		"0x0001", "0x0801", "0x0901", "0x0001",
		"0x0001", "0x0801", "0x0901", "0x0001",
	}, addresses)
	assert.Equal(t, 6, repaired, "only the frames that follow a gap need repairing")
}

func crcOf(data []byte) (byte, byte) {
	crc := calculateCrc(append(append([]byte{}, data[0:4]...), data[6:]...))
	return crc[0], crc[1]
}

func decodeHex(t *testing.T, s string) []byte {
	t.Helper()
	data, err := hex.DecodeString(stripSpaces(s))
	require.NoError(t, err)
	return data
}

func stripSpaces(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != ' ' {
			out = append(out, s[i])
		}
	}
	return string(out)
}
