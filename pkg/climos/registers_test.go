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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func Test_decodeRegisters(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    []Register
		wantErr error
	}{
		{"empty", "", nil, nil},
		{"single one byte value", "1d0001", []Register{RegRunState}, nil},
		{"temperatures", "44008c8100dc0082007d008300eb0084008c00",
			[]Register{0x44, RegTemperatureIndoorIn, RegTemperatureOutside, RegTemperatureIndoorOut, RegTemperatureHouseOut}, nil},
		{"five byte counters", "26000804ba000144008e8500060cb90001",
			[]Register{RegOperatingTime, 0x44, RegOperatingTimeSecond}, nil},
		{"sixteen byte name", "8800c246616e20636f6e74726f6c6c657220", []Register{RegNameFanController}, nil},
		{"unknown register", "1d00018f0000", []Register{RegRunState}, ErrUnknownRegister},
		{"value cut short", "8100dc", nil, ErrTruncatedPayload},
		{"trailing byte", "1d000155", []Register{RegRunState}, ErrTruncatedPayload},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := decodeRegisters(decodeHex(t, tt.payload))

			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			} else {
				assert.NoError(t, err)
			}

			registers := make([]Register, 0, len(got))
			for _, value := range got {
				registers = append(registers, value.Register)
			}
			assert.Equal(t, tt.want, sliceOrNil(registers))
		})
	}
}

func TestRegisterValue_Int(t *testing.T) {
	tests := []struct {
		raw  string
		want int64
	}{
		{"fb", -5},
		{"8c", -116},
		{"7f", 127},
		{"ecff", -20},
		{"dc00", 220},
		{"f200", 242},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			assert.Equal(t, tt.want, RegisterValue{Raw: decodeHex(t, tt.raw)}.Int())
		})
	}
}

func TestRegisterValue_Duration(t *testing.T) {
	// 0x26 as dumped right after the restart on 2026-08-05: 4 years, 22 days, 15:12.
	value := RegisterValue{Register: RegOperatingTime, Raw: decodeHex(t, "0c0f160004")}

	want := time.Duration(4*daysPerFirmwareYear+22)*24*time.Hour + 15*time.Hour + 12*time.Minute
	assert.Equal(t, want, value.Duration())
	assert.Equal(t, float64(128099520), value.Duration().Seconds())
}

func TestRegisterValue_Text(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"article number", "45544130303336453331 45", "ETA0036E31E"},
		{"device name with type byte", "c246616e20636f6e74726f6c6c657220", "Fan controller"},
		{"panel name", "c7546f7563682054465420312020 2020", "Touch TFT 1"},
		{"numeric register", "dc00", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name+"/"+tt.raw, func(t *testing.T) {
			assert.Equal(t, tt.want, RegisterValue{Raw: decodeHex(t, tt.raw)}.Text())
		})
	}
}

func TestRegisterValue_Tenths(t *testing.T) {
	assert.Equal(t, 12.5, RegisterValue{Raw: decodeHex(t, "7d00")}.Tenths())
	assert.Equal(t, -1.5, RegisterValue{Raw: decodeHex(t, "f1ff")}.Tenths())
}

func sliceOrNil(registers []Register) []Register {
	if len(registers) == 0 {
		return nil
	}
	return registers
}
