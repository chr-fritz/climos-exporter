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

func TestParsePackage(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		want    ParsedPackage
		wantErr bool
	}{
		{"Invalid Package", []byte{0x00, 0x00, 0x00, 0x0e, 0x7b, 0x01, 0x01, 0x00, 0x05, 0x17, 0x06, 0x12, 0x02, 0x00, 0x37, 0x25, 0x13, 0x04, 0x00, 0x00}, nil, true},
		{
			"Valid Temperatures",
			[]byte{0x01, 0x00, 0x85, 0x13, 0xd9, 0xaa, 0x44, 0x00, 0x8c, 0x81, 0x00, 0xdc, 0x00, 0x82, 0x00, 0x7d, 0x00, 0x83, 0x00, 0xeb, 0x00, 0x84, 0x00, 0x8c, 0x00},
			&TemperaturePackage{
				IndoorInTemperature:  22,
				OutsideTemperature:   12.5,
				IndoorOutTemperature: 23.5,
				HouseOutTemperature:  14,
			},
			false,
		},
		{
			"Unknown Temperatures",
			[]byte{0x01, 0x00, 0x85, 0x21, 0x08, 0xd3, 0x26, 0x00, 0x3a, 0x10, 0x74, 0x00, 0x01, 0x44, 0x00, 0x8c, 0x81, 0x00, 0xdc, 0x00, 0x82, 0x00, 0x78, 0x00, 0x83, 0x00, 0xeb, 0x00, 0x84, 0x00, 0x8c, 0x00, 0x85, 0x00, 0x38, 0x00, 0x74, 0x00, 0x01},
			nil,
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParsePackage(t.Context(), newPackage(tt.data))
			if (err != nil) != tt.wantErr {
				t.Errorf("ParsePackage() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParsePackage() got = %v, want %v", got, tt.want)
			}
		})
	}
}
