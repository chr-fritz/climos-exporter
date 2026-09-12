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

func TestAnalyzerReportsWhatMoved(t *testing.T) {
	stream := concat(
		dataFrame(t, filterRemaining(60, 0, 0), register44(0x8a)),
		dataFrame(t, filterRemaining(59, 0, 0), register44(0x8a)),
		dataFrame(t, filterRemaining(58, 0, 0), register44(0x8b)),
	)

	analyzer := NewAnalyzer()
	stats := Replay(stream, analyzer.Add)

	require.Equal(t, 3, stats.Frames)
	require.Equal(t, 3, stats.DataFrames)
	assert.Equal(t, 0, stats.Undecodable)

	profiles := byRegister(analyzer.Profiles())
	assert.Equal(t, 3, profiles[0x44].Frames)
	assert.Equal(t, 1, profiles[0x44].Changes, "0x44 took a second value once")
	assert.Equal(t, 2, profiles[0x44].Distinct)
	assert.Equal(t, []byte{0x8a}, profiles[0x44].First)
	assert.Equal(t, []byte{0x8b}, profiles[0x44].Last)
}

// TestAnalyzerTimesChangesByTheFilterCountdown covers the only clock the bus
// offers: a change is placed by how far the countdown has run down since the
// start of the recording, not by when the file was read.
func TestAnalyzerTimesChangesByTheFilterCountdown(t *testing.T) {
	stream := concat(
		dataFrame(t, filterRemaining(60, 0, 0), register44(0x8a)),
		dataFrame(t, filterRemaining(59, 23, 30), register44(0x8b)),
	)

	analyzer := NewAnalyzer()
	Replay(stream, analyzer.Add)

	changes := changesOf(analyzer, 0x44)
	require.Len(t, changes, 1)
	assert.Equal(t, 30*time.Minute, changes[0].Elapsed,
		"half an hour of the countdown had run down when 0x44 moved")
	assert.Equal(t, []byte{0x8a}, changes[0].From)
	assert.Equal(t, []byte{0x8b}, changes[0].To)
}

func TestAnalyzerCountsChangesPerRegister(t *testing.T) {
	stream := concat(
		dataFrame(t, register44(0x01)),
		dataFrame(t, register44(0x02)),
		dataFrame(t, register44(0x03)),
	)

	analyzer := NewAnalyzer()
	Replay(stream, analyzer.Add)

	assert.Equal(t, map[Register]int{0x44: 2}, analyzer.ChangeCounts())
}

func register44(value byte) []byte {
	return []byte{0x44, 0x00, value}
}

// filterRemaining builds the five byte countdown register, which is what moves
// the clock a replay places its changes on.
func filterRemaining(days, hours, minutes uint8) []byte {
	return []byte{0x3e, 0x00, minutes, hours, days, 0, 0}
}

func changesOf(analyzer *Analyzer, register Register) []RegisterChange {
	out := []RegisterChange{}
	for _, change := range analyzer.Changes() {
		if change.Register == register {
			out = append(out, change)
		}
	}
	return out
}

// dataFrame wraps register records into a frame addressed at the register
// space, with the CRC the framer will check.
func dataFrame(t *testing.T, records ...[]byte) []byte {
	t.Helper()

	payload := concat(records...)
	require.Less(t, len(payload), 0x80, "a test payload has to fit the length byte")

	header := []byte{0x01, 0x00, byte(GetSet), byte(len(payload))}
	crc := calculateCrc(concat(header, payload))
	return concat(header, crc, payload)
}

func concat(parts ...[]byte) []byte {
	out := []byte{}
	for _, part := range parts {
		out = append(out, part...)
	}
	return out
}

func byRegister(profiles []RegisterProfile) map[Register]RegisterProfile {
	out := map[Register]RegisterProfile{}
	for _, profile := range profiles {
		out[profile.Register] = profile
	}
	return out
}
