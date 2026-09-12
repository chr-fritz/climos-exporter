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
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.bug.st/serial"
)

// Parity selects which half of the bus traffic the receiver accepts. The line
// sends eleven bit characters — one start bit, nine data bits, one stop bit —
// and a PC UART reads that by putting its parity bit where the ninth data bit
// sits. On this controller family the ninth bit marks a character as an
// address, so space parity passes the data characters and rejects the marked
// ones while mark parity does the opposite. docs/protocol.md records why space
// is the setting that yields whole frames.
type Parity string

const (
	// ParitySpace is what the exporter runs on.
	ParitySpace Parity = "space"
	// ParityMark passes exactly the characters space parity rejects, which is
	// how a recording answers whether the ninth bit still marks frame starts.
	ParityMark Parity = "mark"
	// ParityNone stops the receiver from checking the ninth bit at all. A
	// character is then ten bits where the line sends eleven, so framing breaks
	// — it serves to tell an adapter that reports parity errors from one that
	// drops the bit silently.
	ParityNone Parity = "none"
)

var parityModes = map[Parity]serial.Parity{
	ParitySpace: serial.SpaceParity,
	ParityMark:  serial.MarkParity,
	ParityNone:  serial.NoParity,
}

// ParseParity maps a flag value onto a parity mode.
func ParseParity(value string) (Parity, error) {
	parity := Parity(strings.ToLower(strings.TrimSpace(value)))
	if _, known := parityModes[parity]; !known {
		return "", fmt.Errorf("unknown parity %q, expected space, mark or none", value)
	}
	return parity, nil
}

// ParityNames lists the accepted values for shell completion.
func ParityNames() []string {
	return []string{string(ParitySpace), string(ParityMark), string(ParityNone)}
}

type Reader interface {
	Run(ctx context.Context) error
	PackagesChan() chan *Package
}

type reader struct {
	dev          string
	parity       Parity
	client       serial.Port
	packagesChan chan *Package
	// Optional raw bytestream logging
	streamLogDir string
	streamLog    *os.File
	streamLogDay string
}

func NewReader(dev, streamLogDir string, parity Parity) Reader {
	return &reader{
		dev:          dev,
		parity:       parity,
		packagesChan: make(chan *Package),
		streamLogDir: streamLogDir,
	}
}

func (r *reader) PackagesChan() chan *Package {
	return r.packagesChan
}

func (r *reader) Run(ctx context.Context) error {
	parity, known := parityModes[r.parity]
	if !known {
		return fmt.Errorf("unknown parity %q, expected space, mark or none", r.parity)
	}

	mode := &serial.Mode{
		BaudRate:          9600,
		DataBits:          8,
		Parity:            parity,
		StopBits:          serial.OneStopBit,
		InitialStatusBits: nil,
	}
	var err error
	r.client, err = serial.Open(r.dev, mode)

	if err != nil {
		return err
	}

	go r.read(ctx)

	return nil
}

func (r *reader) read(ctx context.Context) {
	var data []byte
loop:
	for {
		select {
		case <-ctx.Done():
			r.close()
			return
		default:
			buf := make([]byte, 10*1024)
			n, err := r.client.Read(buf)
			if err != nil && !isPortError(err, serial.PortClosed) {
				slog.Warn("Error reading from serial port", "error", err)
				continue
			} else if isPortError(err, serial.PortClosed) {
				break loop
			}

			// Write raw bytestream to daily log file when enabled
			if n > 0 {
				r.writeRaw(buf[:n])
			}
			data = r.emitPackages(append(data, buf[:n]...))
			time.Sleep(1 * time.Second)
		}
	}
}

// emitPackages sends every package the buffer holds and returns what is left
// over. A package can span two reads, so the remainder has to survive into the
// next round instead of being discarded.
func (r *reader) emitPackages(buffer []byte) []byte {
	for {
		extracted, err := extractPackage(buffer)
		if err != nil {
			break
		}

		buffer = buffer[extracted.NextStart:]
		r.packagesChan <- newPackage(extracted.Data, extracted.Repaired)
	}
	return append([]byte{}, buffer...)
}

func (r *reader) close() {
	if err := r.client.Close(); err != nil {
		slog.With("error", err).
			Warn("Can not close serial port")
	}
	if r.streamLog != nil {
		_ = r.streamLog.Close()
	}
}

func isPortError(err error, code serial.PortErrorCode) bool {
	return errors.Is(err, serial.PortError{}) && err.(serial.PortError).Code() == code
}

// streamLogName names the daily file. A recording taken at anything other than
// space parity carries the complementary half of the characters and must not be
// appended to the archive of ordinary recordings, so it gets a name of its own.
// The zero value is an ordinary recording.
func (r *reader) streamLogName() string {
	day := time.Now().Format("2006-01-02")
	if r.parity == "" || r.parity == ParitySpace {
		return day
	}
	return day + "-" + string(r.parity)
}

// writeRaw writes bytes to a file named by the current date in `streamLogDir`.
// If `streamLogDir` is empty, this is a no-op. The file rotates daily and only
// contains data for the current day.
func (r *reader) writeRaw(bs []byte) {
	if r.streamLogDir == "" || len(bs) == 0 {
		return
	}
	// Determine today's file
	today := r.streamLogName()
	if r.streamLogDay != today || r.streamLog == nil {
		// Rotate file
		if r.streamLog != nil {
			_ = r.streamLog.Close()
		}
		// Ensure directory exists
		if err := os.MkdirAll(r.streamLogDir, 0o755); err != nil {
			slog.Warn("Cannot create stream log directory", "dir", r.streamLogDir, "error", err)
			return
		}
		path := filepath.Join(r.streamLogDir, today+".bin")
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			slog.Warn("Cannot open stream log file", "file", path, "error", err)
			return
		}
		r.streamLog = f
		r.streamLogDay = today
	}
	if _, err := r.streamLog.Write(bs); err != nil {
		slog.Warn("Cannot write stream log", "error", err)
	}
}
