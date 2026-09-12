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
	"os"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReplayDump runs a recorded bytestream through the framer and the register
// decoder. Point CLIMOS_DUMP at one of the daily files the reader writes when
// --stream-log-dir is set:
//
//	CLIMOS_DUMP=$PWD/dumps/2026-08-05.bin go test ./pkg/climos/ -run TestReplayDump -v
//
// The counts it prints are the quickest way to check a new firmware or a
// rewired bus against what docs/protocol.md describes.
func TestReplayDump(t *testing.T) {
	path := os.Getenv("CLIMOS_DUMP")
	if path == "" {
		t.Skip("set CLIMOS_DUMP to a recorded bytestream to run this")
	}

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	stats := replay(t, data)

	t.Logf("frames=%d repaired=%d undecodable=%d registers=%d",
		stats.frames, stats.repaired, stats.undecodable, len(stats.registers))
	t.Logf("registers seen: %v", sortedRegisters(stats.registers))

	assert.Greater(t, stats.frames, 100_000, "a full day carries over a million frames")
	assert.Less(t, stats.undecodable, stats.dataFrames/100,
		"every data frame should decode; a failure means an unknown register or a wrong width")
}

type replayStats struct {
	frames      int
	dataFrames  int
	repaired    int
	undecodable int
	registers   map[Register]int
}

func replay(t *testing.T, data []byte) replayStats {
	t.Helper()
	stats := replayStats{registers: map[Register]int{}}

	for {
		extracted, err := extractPackage(data)
		if err != nil {
			return stats
		}
		data = data[extracted.NextStart:]

		stats.frames++
		if extracted.Repaired {
			stats.repaired++
		}
		stats.count(newPackage(extracted.Data, extracted.Repaired))
	}
}

func (s *replayStats) count(p *Package) {
	parsed, err := ParsePackage(context.Background(), p)
	if errors.Is(err, ErrNoRegisterData) {
		return
	}

	s.dataFrames++
	if err != nil {
		s.undecodable++
	}
	data, ok := parsed.(*DataPackage)
	if !ok {
		return
	}
	for _, value := range data.Values {
		s.registers[value.Register]++
	}
}

func sortedRegisters(counts map[Register]int) []string {
	out := make([]string, 0, len(counts))
	for register := range counts {
		out = append(out, register.String())
	}
	sort.Strings(out)
	return out
}
