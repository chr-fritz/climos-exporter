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

package logging

import (
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"log/slog"
	"os"
	"reflect"
	"testing"
)

func Test_loggerConfig_Initialize(t *testing.T) {
	previous := slog.Default()
	lc := &loggerConfig{
		level:         "debug",
		formatterName: "json",
		configLogger:  slog.Default(),
	}
	lc.Initialize()

	now := slog.Default()
	assert.NotSame(t, previous, now)
}

func Test_loggerConfig_createHandler(t *testing.T) {
	tests := []struct {
		name          string
		level         string
		formatterName string
		want          slog.Handler
	}{
		{"json-debug", "debug", "json", slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})},
		{"text-debug", "debug", "text", slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})},
		{"unknown-debug", "debug", "unknown", slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})},
		{"json-error", "error", "json", slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError})},
		{"json-warning", "warning", "json", slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lc := &loggerConfig{
				level:         tt.level,
				formatterName: tt.formatterName,
				configLogger:  slog.Default(),
			}
			if got := lc.createHandler(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("createHandler() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_loggerConfig_parseLevel(t *testing.T) {
	tests := []struct {
		level   string
		want    slog.Level
		wantErr bool
	}{
		{"fatal", slog.LevelInfo, true},
		{"error", slog.LevelError, false},
		{"error+4", slog.LevelError + 4, false},
		{"warn", slog.LevelWarn, false},
		{"warning", slog.LevelInfo, true},
		{"info", slog.LevelInfo, false},
		{"debug", slog.LevelDebug, false},
		{"trace", slog.LevelInfo, true},
	}
	for _, tt := range tests {
		t.Run(tt.level, func(t *testing.T) {
			lc := &loggerConfig{
				level:        tt.level,
				configLogger: slog.Default(),
			}
			got, err := lc.parseLevel()
			if (err != nil) != tt.wantErr {
				t.Errorf("parseLevel() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("parseLevel() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestInitFlags(t *testing.T) {
	pflagCommandline := pflag.CommandLine
	matching := &cobra.Command{}
	tests := []struct {
		name    string
		flagset *pflag.FlagSet
		cmd     *cobra.Command
	}{
		{"both nil", nil, nil},
		{"only flag set", pflag.NewFlagSet("test", 0), nil},
		{"only command", nil, &cobra.Command{}},
		{"both matching", matching.Flags(), matching},
	}
	for _, tt := range tests {
		pflag.CommandLine = pflag.NewFlagSet("cmd", 0)

		t.Run(tt.name, func(t *testing.T) {
			config := InitFlags(tt.flagset, tt.cmd).(*loggerConfig)
			previous := slog.Default()

			config.Initialize()
			now := slog.Default()
			assert.NotSame(t, previous, now)
		})
	}
	pflag.CommandLine = pflagCommandline
}
