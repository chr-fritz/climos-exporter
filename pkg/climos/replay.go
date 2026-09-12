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
	"bytes"
	"context"
	"errors"
	"slices"
	"time"
)

// ReplayEvent is one decoded data frame together with where it sat in the file.
type ReplayEvent struct {
	Offset int
	Values []RegisterValue
}

// ReplayStats counts what a walk over a recording found, in the same terms as
// the climos_frames_total and climos_unknown_registers_total metrics so a
// recording can be held against what the running exporter reports.
type ReplayStats struct {
	Frames      int
	Repaired    int
	DataFrames  int
	Undecodable int
}

// Replay walks a recorded bytestream and hands every data frame to visit. It is
// the offline twin of the reader: same framer, same decoder, no serial port.
func Replay(data []byte, visit func(ReplayEvent)) ReplayStats {
	ctx := context.Background()
	stats := ReplayStats{}
	offset := 0

	for {
		extracted, err := extractPackage(data)
		if err != nil {
			return stats
		}
		frameStart := offset + int(extracted.NextStart) - len(extracted.Data)
		data = data[extracted.NextStart:]
		offset += int(extracted.NextStart)

		stats.Frames++
		if extracted.Repaired {
			stats.Repaired++
		}

		values, ok := decodeFrame(ctx, extracted, &stats)
		if !ok {
			continue
		}
		visit(ReplayEvent{Offset: frameStart, Values: values})
	}
}

// decodeFrame reports the register records of one frame, counting the frames
// that carry register data at all separately from those that fail to decode.
func decodeFrame(ctx context.Context, extracted extractedPackage, stats *ReplayStats) ([]RegisterValue, bool) {
	parsed, err := ParsePackage(ctx, newPackage(extracted.Data, extracted.Repaired))
	if errors.Is(err, ErrNoRegisterData) {
		return nil, false
	}

	stats.DataFrames++
	if err != nil {
		stats.Undecodable++
	}

	data, ok := parsed.(*DataPackage)
	if !ok {
		return nil, false
	}
	return data.Values, true
}

// RegisterProfile summarises one register over a whole recording.
type RegisterProfile struct {
	Register Register
	Frames   int
	Changes  int
	Distinct int
	First    []byte
	Last     []byte
}

// RegisterChange is one register taking a new value.
type RegisterChange struct {
	Offset   int
	Elapsed  time.Duration
	Register Register
	From     []byte
	To       []byte
}

// Analyzer collects both views a recording is read for: a per register summary
// that says which registers move at all, and the individual changes that say
// when. Settings changed at the panel move a register once or twice while the
// temperatures move thousands of times, so the two together are what turns a
// recording into an identification.
type Analyzer struct {
	profiles map[Register]*registerState
	changes  []RegisterChange
	clock    replayClock
}

type registerState struct {
	profile  RegisterProfile
	distinct map[string]struct{}
}

func NewAnalyzer() *Analyzer {
	return &Analyzer{profiles: map[Register]*registerState{}}
}

// Add folds one frame into the analysis.
func (a *Analyzer) Add(event ReplayEvent) {
	for _, value := range event.Values {
		a.clock.observe(value)
		a.addValue(event.Offset, value)
	}
}

func (a *Analyzer) addValue(offset int, value RegisterValue) {
	state, known := a.profiles[value.Register]
	if !known {
		state = &registerState{
			profile:  RegisterProfile{Register: value.Register, First: cloneRaw(value.Raw)},
			distinct: map[string]struct{}{},
		}
		a.profiles[value.Register] = state
	}

	state.profile.Frames++
	state.distinct[string(value.Raw)] = struct{}{}
	state.profile.Distinct = len(state.distinct)

	if state.profile.Last != nil && !bytes.Equal(state.profile.Last, value.Raw) {
		state.profile.Changes++
		a.changes = append(a.changes, RegisterChange{
			Offset:   offset,
			Elapsed:  a.clock.elapsed(),
			Register: value.Register,
			From:     state.profile.Last,
			To:       cloneRaw(value.Raw),
		})
	}
	state.profile.Last = cloneRaw(value.Raw)
}

// Profiles reports every register the recording carried, most active first.
func (a *Analyzer) Profiles() []RegisterProfile {
	out := make([]RegisterProfile, 0, len(a.profiles))
	for _, state := range a.profiles {
		out = append(out, state.profile)
	}
	slices.SortFunc(out, func(x, y RegisterProfile) int {
		if x.Changes != y.Changes {
			return y.Changes - x.Changes
		}
		return int(x.Register) - int(y.Register)
	})
	return out
}

// Changes reports the value changes in the order they were recorded.
func (a *Analyzer) Changes() []RegisterChange {
	return a.changes
}

// ChangeCounts reports how often each register changed, so a caller can drop
// the registers that move constantly before printing the rest.
func (a *Analyzer) ChangeCounts() map[Register]int {
	out := make(map[Register]int, len(a.profiles))
	for register, state := range a.profiles {
		out[register] = state.profile.Changes
	}
	return out
}

// replayClock recovers a timeline from the recording itself. The stream carries
// no timestamp, but the filter countdown ticks down once per running minute,
// which is the only monotone clock on the bus.
type replayClock struct {
	first   time.Duration
	current time.Duration
	started bool
}

func (c *replayClock) observe(value RegisterValue) {
	if value.Register != RegFilterRemaining {
		return
	}
	remaining := value.Duration()
	if remaining == 0 {
		return
	}
	if !c.started {
		c.first = remaining
		c.started = true
	}
	c.current = remaining
}

// elapsed is how long the unit has been running since the start of the
// recording. It stands still while the unit is off, which is the honest answer:
// the countdown is the only time base there is.
func (c *replayClock) elapsed() time.Duration {
	if !c.started {
		return 0
	}
	return c.first - c.current
}

func cloneRaw(raw []byte) []byte {
	return append([]byte(nil), raw...)
}
