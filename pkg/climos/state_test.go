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
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSaveAndLoadRegistersRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registers.json")

	require.NoError(t, SaveRegisters(path, map[Register]RegisterValue{
		0x43: {Register: 0x43, Raw: []byte{0x14}},
		0x41: {Register: 0x41, Raw: []byte{0xf0, 0x00}},
		0x3e: {Register: 0x3e, Raw: []byte{0x11, 0x17, 0x3f, 0x00, 0x00}},
	}))

	values, savedAt, err := LoadRegisters(path)
	require.NoError(t, err)
	require.Len(t, values, 3)

	assert.WithinDuration(t, time.Now(), savedAt, time.Minute)
	assert.Equal(t, Register(0x3e), values[0].Register, "values come back in register order")
	assert.Equal(t, uint64(20), byRegisterValue(values)[0x43].Uint())
	assert.Equal(t, uint64(240), byRegisterValue(values)[0x41].Uint())
	assert.Equal(t, 63*24*time.Hour+23*time.Hour+17*time.Minute, byRegisterValue(values)[0x3e].Duration())
}

// TestLoadRegistersAcceptsAMissingFile covers the first start, where there is
// nothing to restore and that is not a failure.
func TestLoadRegistersAcceptsAMissingFile(t *testing.T) {
	values, _, err := LoadRegisters(filepath.Join(t.TempDir(), "absent.json"))

	require.NoError(t, err)
	assert.Empty(t, values)
}

// TestLoadRegistersRejectsAWrongWidth covers a file written before a register's
// width was known: restoring it would publish a number this build would never
// have read off the bus.
func TestLoadRegistersRejectsAWrongWidth(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registers.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"registers":{"0x43":"1234"}}`), 0o644))

	_, _, err := LoadRegisters(path)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected 1")
}

func TestLoadRegistersRejectsAnUnknownRegister(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registers.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"registers":{"0xfe":"01"}}`), 0o644))

	_, _, err := LoadRegisters(path)

	require.ErrorIs(t, err, ErrUnknownRegister)
}

// TestMetricsExporterRestoresTheRareRegisters is the point of the whole file:
// a setting reaches the bus only when it changes or in the dump after a bus
// restart, so without the state file it is missing from the metrics for as long
// as that takes.
func TestMetricsExporterRestoresTheRareRegisters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registers.json")
	require.NoError(t, SaveRegisters(path, map[Register]RegisterValue{
		0x43:              {Register: 0x43, Raw: []byte{0x14}},
		RegFilterInterval: {Register: RegFilterInterval, Raw: []byte{0x00, 0x00, 0xa0, 0x00, 0x00}},
	}))

	registry := prometheus.NewRegistry()
	exporter, err := NewMetricsExporter(registry, NewReader("", "", ParitySpace), path)
	require.NoError(t, err)
	m := exporter.(*metricsExporter)

	assert.Equal(t, float64(160*24*3600), testutil.ToFloat64(m.mustCollector(t, RegFilterInterval)),
		"the configured filter interval survives a restart of the exporter")
	assert.Equal(t, 20.0, testutil.ToFloat64(m.registers.WithLabelValues("0x43")),
		"and so does a register that has no gauge of its own")
}

func TestMetricsExporterStartsCleanWithoutAStateFile(t *testing.T) {
	m, _ := newTestExporter(t)

	assert.True(t, math.IsNaN(testutil.ToFloat64(m.mustCollector(t, RegFilterInterval))))
}

func byRegisterValue(values []RegisterValue) map[Register]RegisterValue {
	out := map[Register]RegisterValue{}
	for _, value := range values {
		out[value.Register] = value
	}
	return out
}
