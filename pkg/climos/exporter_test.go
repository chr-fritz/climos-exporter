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
# HELP climos_register Raw value of every numeric register the bus reports.
# TYPE climos_register gauge
climos_register{register="0x44"} -116
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
# HELP climos_device_info Article numbers and device names, reported after a bus restart.
# TYPE climos_device_info gauge
climos_device_info{article_fan="",article_master="ETA0036E31E",article_panel="ST00064E26E",fan_controller="",panel=""} 1
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

func TestMetricsExporterConvertsCountersToSeconds(t *testing.T) {
	m, _ := newTestExporter(t)
	// 0x3e counts down to the filter change, 0x3a holds the interval in days.
	frame := decodeHex(t, "0100850a00003e00 0a0c640000 3a0064")
	frame[4], frame[5] = crcOf(frame)
	m.handlePackage(t.Context(), newPackage(frame, false))

	assert.Equal(t, float64((100*24+12)*3600+10*60),
		testutil.ToFloat64(m.mustCollector(t, RegFilterRemaining)))
	assert.Equal(t, float64(100*24*3600),
		testutil.ToFloat64(m.mustCollector(t, RegFilterInterval)))
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
