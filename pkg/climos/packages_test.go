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
	"reflect"
	"testing"
)

func Test_extractPackage(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		want    []byte
		want1   uint
		wantErr bool
	}{
		{
			"at beginning",
			[]byte{0x00, 0x00, 0x00, 0x0e, 0x7b, 0x01, 0x01, 0x00, 0x05, 0x17, 0x06, 0x12, 0x02, 0x00, 0x37, 0x25, 0x13, 0x04, 0x00, 0x00, 0, 0},
			[]byte{0x00, 0x00, 0x00, 0x0e, 0x7b, 0x01, 0x01, 0x00, 0x05, 0x17, 0x06, 0x12, 0x02, 0x00, 0x37, 0x25, 0x13, 0x04, 0x00, 0x00},
			19, false,
		},

		{
			"to short",
			[]byte{0x00, 0x00, 0x00, 0x0e, 0x7b, 0x01, 0x01, 0x00, 0x05, 0x17, 0x06, 0x12, 0x02, 0x00, 0x37, 0x25, 0x13, 0x04},
			[]byte{},
			0, true,
		},
		{
			"to short",
			[]byte{0x00, 0x00, 0x00, 0x0e, 0x7b},
			[]byte{},
			0, true,
		},
		{
			"length to great",
			[]byte{0x00, 0x00, 0x00, 0xff, 0x7b, 0x02, 0x02},
			[]byte{},
			0, true,
		},
		{
			"valid package in middle",
			[]byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x0e, 0x7b, 0x01, 0x01, 0x00, 0x05, 0x17, 0x06, 0x12, 0x02, 0x00, 0x37, 0x25, 0x13, 0x04, 0x00, 0x00, 0, 0},
			[]byte{0x00, 0x00, 0x00, 0x0e, 0x7b, 0x01, 0x01, 0x00, 0x05, 0x17, 0x06, 0x12, 0x02, 0x00, 0x37, 0x25, 0x13, 0x04, 0x00, 0x00},
			22, false,
		},
		{
			"valid package in middle",
			[]byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x0e, 0x01, 0x04, 0x84, 0x00, 0x28, 0x7d, 0x37, 0x25, 0x13, 0x04, 0x00, 0x00, 0, 0},
			[]byte{0x01, 0x04, 0x84, 0x00, 0x28, 0x7d},
			12, false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1, err := extractPackage(tt.data)
			if (err != nil) != tt.wantErr {
				t.Errorf("extractPackage() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("extractPackage() got = %v, want %v", got, tt.want)
			}
			if got1 != tt.want1 {
				t.Errorf("extractPackage() got1 = %v, want %v", got1, tt.want1)
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
			if got := newPackage(tt.data); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("newPackage() = %v, want %v", got, tt.want)
			}
		})
	}
}
