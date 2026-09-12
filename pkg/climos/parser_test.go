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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParsePackage(t *testing.T) {
	tests := []struct {
		name         string
		data         string
		wantErr      error
		wantRegister Register
		wantTenths   float64
	}{
		{"invalid package", "0000000e7b0101000517061202003725130400", ErrNoRegisterData, 0, 0},
		{"poll without payload", "010484002 87d", ErrNoRegisterData, 0, 0},
		{"temperatures", "01008513d9aa4400 8c 8100dc00 82007d00 8300eb00 84008c00", nil, RegTemperatureOutside, 12.5},
		{"temperatures with counters", "01008521c79126000804ba000144008e8100d20082004b008300eb00840064008500060cb90001", nil, RegTemperatureOutside, 7.5},
		{"fan setpoint", "0100850429e5550 0f200", nil, RegFanSetpoint, 24.2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParsePackage(t.Context(), newPackage(decodeHex(t, tt.data), false))
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				assert.Nil(t, got)
				return
			}

			require.NoError(t, err)
			data, ok := got.(*DataPackage)
			require.True(t, ok, "expected a DataPackage, got %T", got)
			assert.Equal(t, tt.wantTenths, findRegister(t, data, tt.wantRegister).Tenths())
		})
	}
}

func TestParsePackageKeepsValuesBeforeAnUnknownRegister(t *testing.T) {
	// A temperature frame with 0x84 replaced by the unassigned register 0x8f.
	data := decodeHex(t, "01008513a9464400 8c 8100dc00 82007d00 8300eb00 8f008c00")
	data[4], data[5] = crcOf(data)

	got, err := ParsePackage(t.Context(), newPackage(data, false))

	var unknown UnknownRegisterError
	require.ErrorAs(t, err, &unknown, "the caller has to learn which register stopped the walk")
	assert.Equal(t, Register(0x8f), unknown.Register)

	values := got.(*DataPackage).Values
	assert.Len(t, values, 4, "the records in front of the unknown one must survive")
	assert.Equal(t, 23.5, findRegister(t, got.(*DataPackage), RegTemperatureIndoorOut).Tenths())
}

func TestDataPackageString(t *testing.T) {
	got, err := ParsePackage(t.Context(), newPackage(decodeHex(t, "0100850429e555 00f200"), false))

	require.NoError(t, err)
	assert.Equal(t, "Got registers: 0x55=0xf200", got.String())
	assert.Equal(t, "data", got.GetPackageType())
}

func findRegister(t *testing.T, data *DataPackage, register Register) RegisterValue {
	t.Helper()
	for _, value := range data.Values {
		if value.Register == register {
			return value
		}
	}
	t.Fatalf("register %s not found in %s", register, data)
	return RegisterValue{}
}
