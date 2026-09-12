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
	"context"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMetricsExporterExportsTemperatures(t *testing.T) {
	m, _ := newTestExporter(t)

	m.handlePackage(t.Context(), newPackage(decodeHex(t, "01008513d9aa44008c8100dc0082007d008300eb0084008c00"), false))

	assert.Equal(t, 22.0, testutil.ToFloat64(m.mustCollector(t, RegTemperatureIndoorIn)))
	assert.Equal(t, 12.5, testutil.ToFloat64(m.mustCollector(t, RegTemperatureOutside)))
	assert.Equal(t, 23.5, testutil.ToFloat64(m.mustCollector(t, RegTemperatureIndoorOut)))
	assert.Equal(t, 14.0, testutil.ToFloat64(m.mustCollector(t, RegTemperatureHouseOut)))
}

func TestMetricsExporterReportsNaNBeforeTheFirstValue(t *testing.T) {
	m, _ := newTestExporter(t)

	assert.True(t, math.IsNaN(testutil.ToFloat64(m.mustCollector(t, RegFilterRemaining))),
		"a register that has not been seen must not read as zero")
}

func TestMetricsExporterExportsUnnamedRegisters(t *testing.T) {
	m, registry := newTestExporter(t)

	m.handlePackage(t.Context(), newPackage(decodeHex(t, "01008513d9aa44008c8100dc0082007d008300eb0084008c00"), false))

	expected := `
# HELP climos_register Unsigned wire value of every numeric register; the registers known to be signed are also exposed interpreted under their own metric.
# TYPE climos_register gauge
climos_register{register="0x44"} 140
climos_register{register="0x81"} 220
climos_register{register="0x82"} 125
climos_register{register="0x83"} 235
climos_register{register="0x84"} 140
`
	require.NoError(t, testutil.GatherAndCompare(registry, strings.NewReader(expected), "climos_register"))
}

func TestMetricsExporterCountsFramesByResult(t *testing.T) {
	m, registry := newTestExporter(t)
	frame := decodeHex(t, "01008513d9aa44008c8100dc0082007d008300eb0084008c00")

	m.handlePackage(t.Context(), newPackage(frame, false))
	m.handlePackage(t.Context(), newPackage(frame, true))
	m.handlePackage(t.Context(), newPackage(frame, true))

	expected := `
# HELP climos_frames_total Frames read from the bus, by framing result.
# TYPE climos_frames_total counter
climos_frames_total{result="ok"} 1
climos_frames_total{result="repaired"} 2
`
	require.NoError(t, testutil.GatherAndCompare(registry, strings.NewReader(expected), "climos_frames_total"))
}

func TestMetricsExporterCountsOneRestartPerAddressScan(t *testing.T) {
	m, _ := newTestExporter(t)
	now := time.Now()

	// One scan walks the whole address range within about a second.
	m.countRestart(now)
	m.countRestart(now.Add(300 * time.Millisecond))
	m.countRestart(now.Add(900 * time.Millisecond))
	assert.Equal(t, 1.0, testutil.ToFloat64(m.restarts))

	m.countRestart(now.Add(restartBurstWindow + time.Second))
	assert.Equal(t, 2.0, testutil.ToFloat64(m.restarts))
}

func TestMetricsExporterBuildsDeviceInfoFromSeveralFrames(t *testing.T) {
	m, registry := newTestExporter(t)

	m.handlePackage(t.Context(), newPackage(decodeHex(t, "010085127ff50000010701 0d00 4554413030333645333145"), false))
	m.handlePackage(t.Context(), newPackage(decodeHex(t, "01008515d82b0000010701 1c00 5354303030363445323645 1d0003"), false))

	expected := `
# HELP climos_device_info Bus version and the article numbers of the attached nodes, reported after a bus restart.
# TYPE climos_device_info gauge
climos_device_info{bus_version="1.7.1",defroster="ST00064E26E",fan_slave="",panel="ETA0036E31E"} 1
`
	require.NoError(t, testutil.GatherAndCompare(registry, strings.NewReader(expected), "climos_device_info"))
}

func TestMetricsExporterRunStopsWithTheContext(t *testing.T) {
	m, _ := newTestExporter(t)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})

	go func() {
		m.Run(ctx)
		close(done)
	}()
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return after the context was cancelled")
	}
}

func newTestExporter(t *testing.T) (*metricsExporter, *prometheus.Registry) {
	t.Helper()
	registry := prometheus.NewRegistry()
	exporter, err := NewMetricsExporter(registry, NewReader("", ""))
	require.NoError(t, err)
	return exporter.(*metricsExporter), registry
}

// mustCollector rebuilds the gauge for one register so a test can read it
// without gathering the whole registry.
func (m *metricsExporter) mustCollector(t *testing.T, register Register) prometheus.Collector {
	t.Helper()
	for _, spec := range gaugeSpecs {
		if spec.Register == register {
			return m.newRegisterGauge(spec)
		}
	}
	t.Fatalf("no gauge declared for register %s", register)
	return nil
}

func TestMetricsExporterReportsAnUnknownRegister(t *testing.T) {
	m, registry := newTestExporter(t)
	// A temperature frame with 0x84 replaced by the unassigned register 0x8f.
	frame := decodeHex(t, "01008513a9464400 8c 8100dc00 82007d00 8300eb00 8f008c00")
	frame[4], frame[5] = crcOf(frame)

	m.handlePackage(t.Context(), newPackage(frame, false))

	expected := `
# HELP climos_unknown_registers_total Payload records dropped because the register id has no known width.
# TYPE climos_unknown_registers_total counter
climos_unknown_registers_total{register="0x8f"} 1
`
	require.NoError(t, testutil.GatherAndCompare(registry, strings.NewReader(expected), "climos_unknown_registers_total"))
	assert.Equal(t, 23.5, testutil.ToFloat64(m.mustCollector(t, RegTemperatureIndoorOut)),
		"the values in front of the unknown register still have to be stored")
}

func TestMetricsExporterExportsEveryDeclaredGauge(t *testing.T) {
	_, registry := newTestExporter(t)

	families, err := registry.Gather()
	require.NoError(t, err)

	names := map[string]bool{}
	for _, family := range families {
		names[family.GetName()] = true
	}
	for _, spec := range gaugeSpecs {
		name := "climos_" + spec.Name
		if spec.Subsystem != "" {
			name = "climos_" + spec.Subsystem + "_" + spec.Name
		}
		assert.True(t, names[name], "%s is declared but not registered", name)
	}
}

// TestMetricsExporterMatchesThePanel replays the registers behind the readings
// the unit's own panel showed on 2026-09-12 at 12:15. The counters in
// particular were guesswork until that screen put both of them side by side:
// 0x85 counts the fan, not merely something trailing 0x26, and the configured
// filter interval is 0x3d rather than 0x3a.
func TestMetricsExporterMatchesThePanel(t *testing.T) {
	m, _ := newTestExporter(t)
	frame := decodeHex(t, "01008521 0000"+
		"0000 010701"+ // Software-Versionen: BUS-Version 1.7.1
		"2600 11113b0004"+ // Betriebsstunden insgesamt: 4 y 59 d 17:17
		"3d00 0000a00000"+ // Filterlaufzeit voreingestellt: 160 days
		"3e00 00003e0000"+ // Filterlaufzeit Restlaufzeit: 62 days
		"8500 280327 0004") // Betriebsstunden Lüfter: 4 y 39 d 03:40
	frame[4], frame[5] = crcOf(frame)

	m.handlePackage(t.Context(), newPackage(frame, false))

	assert.Equal(t, float64((4*365+59)*86400+17*3600+17*60),
		testutil.ToFloat64(m.mustCollector(t, RegOperatingTime)), "insgesamt")
	assert.Equal(t, float64((4*365+39)*86400+3*3600+40*60),
		testutil.ToFloat64(m.mustCollector(t, RegOperatingTimeFan)), "Lüfter")
	assert.Equal(t, float64(160*86400), testutil.ToFloat64(m.mustCollector(t, RegFilterInterval)))
	assert.Equal(t, float64(62*86400), testutil.ToFloat64(m.mustCollector(t, RegFilterRemaining)))
}
