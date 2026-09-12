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
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// capture is 90 bytes taken verbatim from a recorded bytestream and holds eight
// frames, six of which lost their leading address byte to the idle line.
const capture = "000000000000000085033b390e000000088400490801008500000000000000" +
	"098400793f0100850329331d0001000000000000000085033b390e00000008" +
	"8400490801008500000000000000098400793f0100850329331d0001"

func Test_reader_emitPackages(t *testing.T) {
	data := decodeHex(t, capture)

	got, leftover := collectPackages(t, func(r *reader) []byte { return r.emitPackages(data) })

	assert.Equal(t, 8, len(got))
	assert.Empty(t, leftover, "the capture ends on a frame boundary")
}

// Test_reader_emitPackages_acrossReads feeds the same bytes in two chunks that
// split a frame down the middle. A serial read ends wherever the buffer happens
// to fill, so a frame surviving that boundary is the property that matters.
func Test_reader_emitPackages_acrossReads(t *testing.T) {
	data := decodeHex(t, capture)
	const split = 40

	first, leftover := collectPackages(t, func(r *reader) []byte { return r.emitPackages(data[:split]) })
	require.NotEmpty(t, leftover, "an incomplete frame has to be carried over")

	second, rest := collectPackages(t, func(r *reader) []byte {
		return r.emitPackages(append(leftover, data[split:]...))
	})

	assert.Equal(t, 8, len(first)+len(second), "no frame may be lost at the read boundary")
	assert.Empty(t, rest)
}

func Test_reader_emitPackages_keepsIncompleteTail(t *testing.T) {
	data := decodeHex(t, capture)

	_, leftover := collectPackages(t, func(r *reader) []byte { return r.emitPackages(data[:len(data)-4]) })

	assert.Equal(t, data[len(data)-9:len(data)-4], leftover,
		"the bytes of the truncated frame have to be kept for the next read")
}

// collectPackages runs one emitPackages call against a reader whose channel is
// drained concurrently, because emitPackages blocks on an unbuffered channel.
func collectPackages(t *testing.T, emit func(*reader) []byte) ([]*Package, []byte) {
	t.Helper()
	r := &reader{packagesChan: make(chan *Package)}

	done := make(chan []byte, 1)
	go func() { done <- emit(r) }()

	var got []*Package
	for {
		select {
		case p := <-r.packagesChan:
			got = append(got, p)
		case leftover := <-done:
			return got, leftover
		}
	}
}

// Test_reader_writeRaw covers the recording that produced the dumps the
// protocol was reconstructed from: a lost or truncated byte there makes a whole
// day unreadable, because the framing has no resynchronisation point.
func Test_reader_writeRaw(t *testing.T) {
	dir := t.TempDir()
	r := &reader{streamLogDir: dir}
	defer func() { require.NoError(t, r.streamLog.Close()) }()

	r.writeRaw(decodeHex(t, "0100850329331d0001"))
	r.writeRaw(decodeHex(t, "010484002 87d"))

	written, err := os.ReadFile(filepath.Join(dir, time.Now().Format("2006-01-02")+".bin"))
	require.NoError(t, err)
	assert.Equal(t, decodeHex(t, "0100850329331d0001010484002 87d"), written,
		"consecutive writes have to append, not overwrite")
}

func Test_reader_writeRawIsOffWithoutADirectory(t *testing.T) {
	r := &reader{}

	r.writeRaw([]byte{0x01})

	assert.Nil(t, r.streamLog, "no directory configured means no file is opened")
}
