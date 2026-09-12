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

package cmd

import (
	"context"

	"github.com/chr-fritz/climos-exporter/pkg/climos"
	"github.com/chr-fritz/climos-exporter/pkg/metrics"
	"github.com/heptiolabs/healthcheck"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

const RunPortParm = "exporter.port"
const RunDeviceParm = "exporter.device"
const RunStreamDirParm = "exporter.stream.dir"
const RunParityParm = "exporter.parity"

const (
	portFlag      = "port"
	deviceFlag    = "device"
	streamDirFlag = "stream-dir"
	parityFlag    = "parity"
)

type RunOptions struct {
}

func NewRunOptions() *RunOptions {
	return &RunOptions{}
}

func NewRunCommand() *cobra.Command {
	runOptions := NewRunOptions()

	cmd := cobra.Command{
		Use:   "run",
		Short: "Run the exporter",
		Long:  ``,
		Args:  cobra.NoArgs,
		RunE:  runOptions.run,
	}

	cmd.Flags().Uint16P(portFlag, "p", 8080, "The port where all metrics should be exported.")
	_ = viper.BindPFlag(RunPortParm, cmd.Flags().Lookup(portFlag))
	_ = cmd.RegisterFlagCompletionFunc(portFlag, func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return nil, cobra.ShellCompDirectiveNoFileComp
	})

	cmd.Flags().StringP(deviceFlag, "d", "/dev/ttyUSB0", "The tty device where the climos ventilation system is connected")
	_ = viper.BindPFlag(RunDeviceParm, cmd.Flags().Lookup(deviceFlag))
	_ = cmd.RegisterFlagCompletionFunc(deviceFlag, func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return nil, cobra.ShellCompDirectiveFilterFileExt
	})

	cmd.Flags().String(streamDirFlag, "", "Directory to write raw bytestream logs (optional; empty disables logging)")
	_ = viper.BindPFlag(RunStreamDirParm, cmd.Flags().Lookup(streamDirFlag))
	_ = cmd.RegisterFlagCompletionFunc(streamDirFlag, func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return nil, cobra.ShellCompDirectiveFilterFileExt
	})

	cmd.Flags().String(parityFlag, string(climos.ParitySpace), "Which half of the bus traffic to accept. Space reads the frames; mark reads only the characters space rejects and is an experiment, not an operating mode.")
	_ = viper.BindPFlag(RunParityParm, cmd.Flags().Lookup(parityFlag))
	_ = cmd.RegisterFlagCompletionFunc(parityFlag, func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return climos.ParityNames(), cobra.ShellCompDirectiveNoFileComp
	})

	return &cmd
}

func (i *RunOptions) run(cmd *cobra.Command, _ []string) error {
	ctx, cancelFunc := context.WithCancel(cmd.Context())
	defer cancelFunc()

	prometheusExporter := metrics.NewExporter(uint16(viper.GetUint(RunPortParm)))

	prometheusExporter.AddLivenessCheck("goroutine-threshold", healthcheck.GoroutineCountCheck(100))
	reader, err := i.initAndRunReader(ctx, viper.GetString(RunDeviceParm))
	if err != nil {
		return err
	}

	_, err = i.initAndRunMetricsExporter(ctx, prometheusExporter, reader)
	if err != nil {
		return err
	}

	return prometheusExporter.Run()
}

func (i *RunOptions) initAndRunMetricsExporter(ctx context.Context, exporter metrics.Exporter, reader climos.Reader) (climos.MetricsExporter, error) {
	metricsExporter, err := climos.NewMetricsExporter(exporter, reader)
	if err != nil {
		return nil, err
	}

	go metricsExporter.Run(ctx)

	return metricsExporter, nil
}
func (i *RunOptions) initAndRunReader(ctx context.Context, device string) (climos.Reader, error) {
	parity, err := climos.ParseParity(viper.GetString(RunParityParm))
	if err != nil {
		return nil, err
	}

	reader := climos.NewReader(device, viper.GetString(RunStreamDirParm), parity)

	if e := reader.Run(ctx); e != nil {
		return nil, e
	}

	return reader, nil
}

func init() {
	rootCmd.AddCommand(NewRunCommand())
}
