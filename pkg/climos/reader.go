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
	"errors"
	"go.bug.st/serial"
	"log/slog"
	"time"
)

type Reader interface {
	Run() error
	PackagesChan() chan *Package
	Close()
}

type reader struct {
	dev          string
	client       serial.Port
	packagesChan chan *Package
}

func NewReader(dev string) Reader {
	return &reader{
		dev:          dev,
		packagesChan: make(chan *Package),
	}
}

func (r reader) PackagesChan() chan *Package {
	return r.packagesChan
}

func (r reader) Run() error {
	mode := &serial.Mode{
		BaudRate:          9600,
		DataBits:          8,
		Parity:            serial.SpaceParity,
		StopBits:          serial.OneStopBit,
		InitialStatusBits: nil,
	}
	var err error
	r.client, err = serial.Open(r.dev, mode)

	if err != nil {
		return err
	}

	go r.read()

	return nil
}

func (r reader) read() {
	var data []byte
	for {
		buf := make([]byte, 10*1024)
		n, err := r.client.Read(buf)
		if err != nil && !isPortError(err, serial.PortClosed) {
			slog.Warn("Error reading from serial port", "error", err)
			continue
		} else if isPortError(err, serial.PortClosed) {
			break
		}
		data = append(data, buf[:n]...)

		for {
			bytes, newStart, err := extractPackage(data)
			if err != nil && errors.Is(err, ErrorToShort) {
				break
			}

			data = append([]byte{}, data[newStart:]...)
			r.packagesChan <- newPackage(bytes)
		}
		time.Sleep(1 * time.Second)
	}
}

func (r reader) Close() {
	if err := r.client.Close(); err != nil {
		slog.With("error", err).
			Warn("Can not close serial port")
	}
}

func isPortError(err error, code serial.PortErrorCode) bool {
	return errors.Is(err, serial.PortError{}) && err.(serial.PortError).Code() == code
}
