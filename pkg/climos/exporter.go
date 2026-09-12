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
	"errors"
	"log/slog"
	"math"
	"slices"
	"sync"
	"time"

	"github.com/chr-fritz/climos-exporter/pkg/logging"
	"github.com/hashicorp/go-multierror"
	"github.com/prometheus/client_golang/prometheus"
)

const metricNamespace = "climos"

const (
	frameResultOk       = "ok"
	frameResultRepaired = "repaired"
)

// restartBurstWindow separates two bus restarts from each other. One restart
// scans the whole address range within about a second, so every scan frame
// closer together than this belongs to the same restart.
const restartBurstWindow = 5 * time.Second

type MetricsExporter interface {
	Run(ctx context.Context)
}

// gaugeSpec describes one register that is exported under a stable metric name.
type gaugeSpec struct {
	Register    Register
	Subsystem   string
	Name        string
	Help        string
	ConstLabels prometheus.Labels
	Read        func(RegisterValue) float64
}

// gaugeSpecs covers the registers whose meaning is established. Everything else
// the bus reports still reaches Prometheus as climos_register, so an
// unidentified value stays visible. docs/protocol.md records the evidence.
var gaugeSpecs = []gaugeSpec{
	{RegTemperatureIndoorIn, "temperatures", "indoor_in", "Supply air temperature entering the house in degree celsius.", nil, RegisterValue.Tenths},
	{RegTemperatureOutside, "temperatures", "outside", "Outdoor air temperature in degree celsius.", nil, RegisterValue.Tenths},
	{RegTemperatureIndoorOut, "temperatures", "indoor_out", "Extract air temperature leaving the rooms in degree celsius.", nil, RegisterValue.Tenths},
	{RegTemperatureHouseOut, "temperatures", "house_out", "Exhaust air temperature leaving the house in degree celsius.", nil, RegisterValue.Tenths},
	{RegFanSetpoint, "", "fan_setpoint_percent", "Fan setpoint in percent, mirroring the 0-10V control input.", nil, RegisterValue.Tenths},
	{RegFilterRemaining, "", "filter_remaining_seconds", "Time left until the filter change is due.", nil, durationSeconds},
	{RegFilterInterval, "", "filter_interval_seconds", "Configured interval between filter changes.", nil, durationSeconds},
	{RegOperatingTime, "", "operating_seconds", "Operating time as the panel counts it.", prometheus.Labels{"counter": "device"}, durationSeconds},
	{RegOperatingTimeFan, "", "operating_seconds", "Operating time as the panel counts it.", prometheus.Labels{"counter": "fan"}, durationSeconds},
	{RegOperatingMode, "", "operating_mode", "Operating mode selected on the panel: 1-3 fan stage, 4 boost, 5 away, 6 automatic.", nil, plainNumber},
	{RegRunState, "", "run_state", "Run state: 1 running, 0 shutting down, 3 shortly after a start.", nil, plainNumber},
	{RegStatusWord, "", "status_word", "Status word, 13 during normal operation.", nil, plainNumber},
	{RegLifecycle, "", "lifecycle_state", "Lifecycle marker: 0 before stopping, 1 then 3 while starting.", nil, plainNumber},
	{RegError, "", "error_code", "Error code reported by the unit, 0 when healthy.", nil, plainNumber},
}

// identityRegisters make up the label set of climos_device_info, in the order
// the labels are declared. The master announces the other nodes but not itself,
// so its own article number never reaches the bus.
var identityRegisters = []Register{
	RegBusVersion, RegArticlePanel, RegArticleFanSlave, RegArticleDefroster,
}

func durationSeconds(value RegisterValue) float64 {
	return value.Duration().Seconds()
}

func plainNumber(value RegisterValue) float64 {
	return float64(value.Uint())
}

type metricsExporter struct {
	registerer prometheus.Registerer
	reader     Reader

	valuesMu sync.RWMutex
	values   map[Register]RegisterValue

	frames      *prometheus.CounterVec
	unknown     *prometheus.CounterVec
	restarts    prometheus.Counter
	registers   *prometheus.GaugeVec
	deviceInfo  *prometheus.GaugeVec
	lastPackage prometheus.Gauge

	lastScan time.Time
}

func NewMetricsExporter(registerer prometheus.Registerer, reader Reader) (MetricsExporter, error) {
	m := &metricsExporter{
		registerer: registerer,
		reader:     reader,
		values:     map[Register]RegisterValue{},
		frames: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricNamespace,
			Name:      "frames_total",
			Help:      "Frames read from the bus, by framing result.",
		}, []string{"result"}),
		unknown: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricNamespace,
			Name:      "unknown_registers_total",
			Help:      "Payload records dropped because the register id has no known width.",
		}, []string{"register"}),
		restarts: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: metricNamespace,
			Name:      "bus_restarts_total",
			Help:      "Bus restarts, counted once per address scan.",
		}),
		registers: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: metricNamespace,
			Name:      "register",
			Help:      "Unsigned wire value of every numeric register; the registers known to be signed are also exposed interpreted under their own metric.",
		}, []string{"register"}),
		deviceInfo: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: metricNamespace,
			Name:      "device_info",
			Help:      "Bus version and the article numbers of the attached nodes, reported after a bus restart.",
		}, []string{"bus_version", "panel", "fan_slave", "defroster"}),
		lastPackage: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: metricNamespace,
			Name:      "last_package_timestamp_seconds",
			Help:      "Unix time of the last package read from the bus.",
		}),
	}

	if err := m.registerMetrics(); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *metricsExporter) registerMetrics() error {
	var result *multierror.Error

	collectors := []prometheus.Collector{m.frames, m.unknown, m.restarts, m.registers, m.deviceInfo, m.lastPackage}
	for _, spec := range gaugeSpecs {
		collectors = append(collectors, m.newRegisterGauge(spec))
	}
	for _, collector := range collectors {
		if err := m.registerer.Register(collector); err != nil {
			result = multierror.Append(result, err)
		}
	}

	// Declaring both results up front keeps the repaired share of
	// climos_frames_total usable before the first repair happens.
	m.frames.WithLabelValues(frameResultOk)
	m.frames.WithLabelValues(frameResultRepaired)

	return result.ErrorOrNil()
}

func (m *metricsExporter) newRegisterGauge(spec gaugeSpec) prometheus.Collector {
	return prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Namespace:   metricNamespace,
		Subsystem:   spec.Subsystem,
		Name:        spec.Name,
		Help:        spec.Help,
		ConstLabels: spec.ConstLabels,
	}, func() float64 { return m.read(spec) })
}

// read reports NaN while a register has not been seen yet. The once-a-minute
// and restart-only registers are legitimately absent for a while after start.
func (m *metricsExporter) read(spec gaugeSpec) float64 {
	m.valuesMu.RLock()
	defer m.valuesMu.RUnlock()

	value, ok := m.values[spec.Register]
	if !ok {
		return math.NaN()
	}
	return spec.Read(value)
}

func (m *metricsExporter) Run(ctx context.Context) {
	for {
		select {
		case p := <-m.reader.PackagesChan():
			m.handlePackage(ctx, p)
		case <-ctx.Done():
			return
		}
	}
}

func (m *metricsExporter) handlePackage(ctx context.Context, p *Package) {
	m.countFrame(p)
	logPackage(ctx, p)

	parsed, err := ParsePackage(ctx, p)
	m.countUnknownRegister(err)

	if data, ok := parsed.(*DataPackage); ok {
		m.store(data.Values)
	}
}

// countUnknownRegister makes a register that the decoder cannot place visible.
// Everything behind it in that payload is lost, so a rising counter is the
// signal to work out the new register's width from the next restart dump.
func (m *metricsExporter) countUnknownRegister(err error) {
	var unknown UnknownRegisterError
	if !errors.As(err, &unknown) {
		return
	}
	m.unknown.WithLabelValues(unknown.Register.String()).Inc()
}

func (m *metricsExporter) countFrame(p *Package) {
	result := frameResultOk
	if p.Repaired {
		result = frameResultRepaired
	}
	m.frames.WithLabelValues(result).Inc()
	m.lastPackage.SetToCurrentTime()

	if p.Command == BroadcastRequest {
		m.countRestart(time.Now())
	}
}

func (m *metricsExporter) countRestart(now time.Time) {
	if now.Sub(m.lastScan) > restartBurstWindow {
		m.restarts.Inc()
	}
	m.lastScan = now
}

func (m *metricsExporter) store(values []RegisterValue) {
	m.valuesMu.Lock()
	for _, value := range values {
		m.values[value.Register] = value
		if value.IsNumeric() {
			m.registers.WithLabelValues(value.Register.String()).Set(float64(value.Uint()))
		}
	}
	m.valuesMu.Unlock()

	if slices.ContainsFunc(values, isIdentity) {
		m.updateDeviceInfo()
	}
}

func isIdentity(value RegisterValue) bool {
	return slices.Contains(identityRegisters, value.Register)
}

// updateDeviceInfo republishes the whole series whenever one of its labels
// arrives, because the parts come from different frames of the restart dump.
func (m *metricsExporter) updateDeviceInfo() {
	m.valuesMu.RLock()
	labels := []string{m.values[RegBusVersion].Version()}
	for _, register := range identityRegisters[1:] {
		labels = append(labels, m.values[register].Text())
	}
	_, known := m.values[RegArticlePanel]
	m.valuesMu.RUnlock()

	if !known {
		return
	}
	m.deviceInfo.Reset()
	m.deviceInfo.WithLabelValues(labels...).Set(1)
}

// logPackage builds its attributes only when they will be written: at roughly
// thirty frames per second the formatting would otherwise dominate the loop.
func logPackage(ctx context.Context, p *Package) {
	if !slog.Default().Enabled(ctx, logging.LevelTrace) {
		return
	}
	slog.With(
		"command", p.Command,
		"address", p.TargetAddress,
		"repaired", p.Repaired,
		"data", asHex(p.Data),
	).Log(ctx, logging.LevelTrace, "Got valid package")
}
