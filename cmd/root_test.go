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
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
)

// TestInitConfigReadsTheEnvironment guards the key replacer. Without it viper
// derives an environment name containing a dot from a key like exporter.device,
// which no shell can set, and every value below is silently ignored.
func TestInitConfigReadsTheEnvironment(t *testing.T) {
	tests := []struct {
		variable string
		key      string
		value    string
	}{
		{"EXPORTER_DEVICE", RunDeviceParm, "/dev/ttyUSB7"},
		{"EXPORTER_PORT", RunPortParm, "9123"},
		{"EXPORTER_STREAM_DIR", RunStreamDirParm, "/var/lib/climos"},
	}
	for _, tt := range tests {
		t.Run(tt.variable, func(t *testing.T) {
			t.Setenv(tt.variable, tt.value)

			NewRootOptions().initConfig()

			assert.Equal(t, tt.value, viper.GetString(tt.key))
		})
	}
}
