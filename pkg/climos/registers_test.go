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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
			[]Register{RegOperatingTime, 0x44, RegOperatingTimeFan}, nil},
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

// TestRegisterValue_UintKeepsTheWireValue guards the reading of register 0x44,
// whose soft start ramps evenly from 10 to 138 and would jump from 127 to -128
// if the byte were sign extended.
func TestRegisterValue_UintKeepsTheWireValue(t *testing.T) {
	tests := []struct {
		raw  string
		want uint64
	}{
		{"8a", 138},
		{"fb", 251},
		{"be", 190},
		{"7f", 127},
		{"f1ff", 65521},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			assert.Equal(t, tt.want, RegisterValue{Raw: decodeHex(t, tt.raw)}.Uint())
		})
	}
	assert.Zero(t, RegisterValue{}.Uint())
	assert.Zero(t, RegisterValue{Raw: make([]byte, 11)}.Uint(), "text is not a number")
}

func TestRegisterValue_IntRejectsWidthsItCannotRepresent(t *testing.T) {
	assert.Equal(t, int64(0), RegisterValue{}.Int(), "an absent register has no number")
	assert.Equal(t, int64(0), RegisterValue{Raw: make([]byte, 11)}.Int(), "text is not a number")
	assert.Equal(t, int64(-1), RegisterValue{Raw: decodeHex(t, "ffffffffffffffff")}.Int())
}

func TestRegisterValue_DurationOnlyReadsCounters(t *testing.T) {
	assert.Zero(t, RegisterValue{Raw: decodeHex(t, "dc00")}.Duration(),
		"a two byte register is not a counter")
}

func TestRegisterValue_IsNumeric(t *testing.T) {
	for _, width := range []int{1, 2} {
		assert.True(t, RegisterValue{Raw: make([]byte, width)}.IsNumeric(), "width %d", width)
	}
	for _, width := range []int{0, versionWidth, 4, counterWidth, articleWidth, nameWidth} {
		assert.False(t, RegisterValue{Raw: make([]byte, width)}.IsNumeric(), "width %d", width)
	}
}

// TestNumericRegistersAreOneOrTwoBytes is what lets IsNumeric decide on width
// alone: every wider register in the table is a version, a stamp, a counter,
// text or a bit field, none of which reads as a number.
func TestNumericRegistersAreOneOrTwoBytes(t *testing.T) {
	structured := map[Register]bool{
		RegBusVersion: true, 0x89: true, 0x8a: true, 0x8c: true, 0x8d: true,
	}
	for register, width := range registerWidths {
		if width == 3 || width == 4 {
			assert.True(t, structured[register],
				"register %s is %d bytes wide but not a known version or stamp", register, width)
		}
	}
}

func TestUnknownRegisterError(t *testing.T) {
	err := UnknownRegisterError{Register: 0x8f, Offset: 15}

	assert.ErrorIs(t, err, ErrUnknownRegister)
	assert.Equal(t, "unknown register 0x8f at offset 15", err.Error())
}

func TestRegisterWidthsCoverEveryNamedRegister(t *testing.T) {
	named := []Register{
		RegBusVersion, RegLifecycle, RegArticlePanel, RegError, RegArticleFanSlave, RegStatusWord,
		RegArticleDefroster, RegRunState, RegOperatingTime, RegOperatingMode,
		RegFilterInterval, RegFilterRemaining, RegFanSetpoint,
		RegTemperatureIndoorIn, RegTemperatureOutside, RegTemperatureIndoorOut,
		RegTemperatureHouseOut, RegOperatingTimeFan, RegNameFanController, RegNamePanel,
	}
	for _, register := range named {
		_, known := registerWidths[register]
		assert.True(t, known, "register %s is named but has no width", register)
	}
}

// TestRegisterValue_Version covers the bus version the panel shows under
// Information / Software-Versionen, which on this unit reads 1.7.1.
func TestRegisterValue_Version(t *testing.T) {
	assert.Equal(t, "1.7.1", RegisterValue{Raw: decodeHex(t, "010701")}.Version())
	assert.Empty(t, RegisterValue{Raw: decodeHex(t, "dc00")}.Version())
}

// TestDecodeRegistersReadsTheLateArrival covers 0x62, which appears in no
// restart dump and so has no width from one. The frame below was recorded on
// 2026-09-12 while a setting was changed on the panel, and it pins the width
// down: 0x2e is one byte, which leaves exactly one for 0x62.
func TestDecodeRegistersReadsTheLateArrival(t *testing.T) {
	values, err := decodeRegisters([]byte{0x2e, 0x00, 0x12, 0x62, 0x00, 0x0b})

	require.NoError(t, err)
	require.Len(t, values, 2)
	assert.Equal(t, Register(0x2e), values[0].Register)
	assert.Equal(t, uint64(18), values[0].Uint())
	assert.Equal(t, Register(0x62), values[1].Register)
	assert.Equal(t, uint64(11), values[1].Uint())
}
