// Package wav reads and writes the mono 16-bit PCM files in testdata.
package wav

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
)

// Read decodes a mono 16-bit PCM WAV file to samples in [-1, 1) and reports
// its sample rate.
func Read(path string) ([]float32, int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, err
	}
	s, rate, err := Decode(data)
	if err != nil {
		return nil, 0, fmt.Errorf("%s: %w", path, err)
	}
	return s, rate, nil
}

// Decode decodes a mono 16-bit PCM WAV file.
func Decode(data []byte) ([]float32, int, error) {
	if len(data) < 12 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return nil, 0, errors.New("not a WAV file")
	}
	var format struct {
		Tag, Channels uint16
		Rate          uint32
		_             uint32
		_             uint16
		Bits          uint16
	}
	for chunk := data[12:]; len(chunk) >= 8; {
		id, size := string(chunk[:4]), int(binary.LittleEndian.Uint32(chunk[4:8]))
		if size > len(chunk)-8 {
			return nil, 0, errors.New("truncated chunk")
		}
		body := chunk[8 : 8+size]
		switch id {
		case "fmt ":
			if err := binary.Read(bytes.NewReader(body), binary.LittleEndian, &format); err != nil {
				return nil, 0, err
			}
		case "data":
			if format.Tag != 1 || format.Channels != 1 || format.Bits != 16 {
				return nil, 0, errors.New("expected mono 16-bit PCM")
			}
			out := make([]float32, size/2)
			for i := range out {
				out[i] = float32(int16(binary.LittleEndian.Uint16(body[2*i:]))) / 32768
			}
			return out, int(format.Rate), nil
		}
		chunk = chunk[8+size+size%2:]
	}
	return nil, 0, errors.New("no data chunk")
}

// Quantize rounds samples to 16 bits, clipping at full scale.
func Quantize(s []float32) []int16 {
	out := make([]int16, len(s))
	for i, v := range s {
		out[i] = int16(max(-32768, min(32767, math.Round(float64(v)*32768))))
	}
	return out
}

// Write encodes samples as a mono 16-bit PCM WAV file, rounding and clipping
// like Quantize.
func Write(path string, s []float32, rate int) error {
	q := Quantize(s)
	var b bytes.Buffer
	le := binary.LittleEndian
	b.WriteString("RIFF")
	binary.Write(&b, le, uint32(36+2*len(q)))
	b.WriteString("WAVEfmt ")
	binary.Write(&b, le, struct {
		Size                uint32
		Tag, Channels       uint16
		Rate, ByteRate      uint32
		BlockAlign, BitsPer uint16
	}{16, 1, 1, uint32(rate), uint32(2 * rate), 2, 16})
	b.WriteString("data")
	binary.Write(&b, le, uint32(2*len(q)))
	binary.Write(&b, le, q)
	return os.WriteFile(path, b.Bytes(), 0o644)
}
