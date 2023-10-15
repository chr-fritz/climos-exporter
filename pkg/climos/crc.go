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

import "bytes"

// ValidateCrc checks if given package can be valid due to the containing crc on byte 5 and 6.
func ValidateCrc(data []byte) bool {
	if len(data) < 6 {
		return false
	}

	refCrc := []byte{data[4], data[5]}
	calcForData := append([]byte{}, data[0:4]...)
	calcForData = append(calcForData, data[6:]...)
	crc := calculateCrc(calcForData)
	return bytes.Equal(refCrc, crc)
}

func calculateCrc(data []byte) []byte {
	var crc uint16
	crc = crc16CCITT(crc, data)
	return []byte{byte(crc & 0xFF), byte(crc >> 8)}
}
func crc16CCITT(crc uint16, data []byte) uint16 {
	msb := crc >> 8
	lsb := crc & 255
	// Implement crc16CCITT function here
	for _, c := range data {
		x := uint16(c) ^ msb
		x ^= x >> 4
		msb = (lsb ^ (x >> 3) ^ (x << 4)) & 255
		lsb = (x ^ (x << 5)) & 255
		crc = (msb << 8) + lsb
	}
	return crc
}
