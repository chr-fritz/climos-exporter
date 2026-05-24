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
	"log/slog"

	"github.com/chr-fritz/climos-exporter/pkg/logging"
	"github.com/hashicorp/go-multierror"
	"github.com/prometheus/client_golang/prometheus"
)

type MetricsExporter interface {
	Run(ctx context.Context)
}

type metricsExporter struct {
	lastTemperatures *TemperaturePackage
	registerer       prometheus.Registerer
	reader           Reader
}

func NewMetricsExporter(registerer prometheus.Registerer, reader Reader) (MetricsExporter, error) {
	m := &metricsExporter{
		registerer: registerer,
		reader:     reader,
	}
	if err := m.registerTemperatureMetrics(); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *metricsExporter) Run(ctx context.Context) {
	for {
		select {
		case p := <-m.reader.PackagesChan():
			slog.With(
				"command", p.Command,
				"address", p.TargetAddress,
				"data", asHex(p.Data),
			).Log(ctx, logging.LevelTrace, "Got valid package")

			parsePackage, err := ParsePackage(ctx, p)
			if err != nil {
				// do nothing
				continue
			}

			t, ok := parsePackage.(*TemperaturePackage)
			if ok {
				m.lastTemperatures = t
			}

		case <-ctx.Done():
			return
		}
	}
}

func (m *metricsExporter) registerTemperatureMetrics() error {
	var result *multierror.Error
	err := m.registerer.Register(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Namespace: "climos",
		Subsystem: "temperatures",
		Name:      "outside",
	}, func() float64 { return m.getLastTemperatures().OutsideTemperature }))

	if err != nil {
		result = multierror.Append(result, err)
	}
	err = m.registerer.Register(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Namespace: "climos",
		Subsystem: "temperatures",
		Name:      "indoor_in",
	}, func() float64 { return m.getLastTemperatures().IndoorInTemperature }))

	if err != nil {
		result = multierror.Append(result, err)
	}
	err = m.registerer.Register(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Namespace: "climos",
		Subsystem: "temperatures",
		Name:      "indoor_out",
	}, func() float64 { return m.getLastTemperatures().IndoorOutTemperature }))
	if err != nil {
		result = multierror.Append(result, err)
	}

	err = m.registerer.Register(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Namespace: "climos",
		Subsystem: "temperatures",
		Name:      "house_out",
	}, func() float64 { return m.getLastTemperatures().HouseOutTemperature }))
	if err != nil {
		result = multierror.Append(result, err)
	}
	return result.ErrorOrNil()
}

func (m *metricsExporter) getLastTemperatures() *TemperaturePackage {
	if m.lastTemperatures == nil {
		return noTemperatures
	}
	return m.lastTemperatures
}
