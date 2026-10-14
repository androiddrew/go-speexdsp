package mdf

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// Snapshot format: a header, then every scalar and buffer of the state in
// the order fields lists them, little-endian. Everything is included, scratch
// buffers too, so a restored state continues exactly where the snapshot was
// taken. Derived values (the window, the FFT, the rate's constants) are
// rebuilt by New and checked through the header instead.
const (
	snapshotMagic   = "SPXE"
	snapshotVersion = 1
)

// ErrSnapshot reports a snapshot that can't be restored.
var ErrSnapshot = errors.New("invalid snapshot")

// fields lists the state's mutable values in snapshot order. The same list
// is used to save and to restore, so the two can't disagree.
func (s *State) fields() (ints []*int, floats []*float32, buffers [][]float32) {
	ints = []*int{&s.cancelCount, &s.saturated, &s.screwedUp}
	floats = []*float32{
		&s.sumAdapt, &s.leakEstimate, &s.davg1, &s.davg2, &s.dvar1, &s.dvar2,
		&s.pey, &s.pyy, &s.memX, &s.memD, &s.memE, &s.notchMem[0], &s.notchMem[1],
	}
	buffers = [][]float32{
		s.e, s.x, s.xf, s.input, s.y, s.lastY, s.yf, s.ef, s.phi, s.w, s.foreground,
		s.power, s.power1, s.wtmp, s.rf, s.yfp, s.xfp, s.eh, s.yh, s.prop, s.out16,
	}
	return ints, floats, buffers
}

// MarshalBinary returns the state's snapshot.
func (s *State) MarshalBinary() ([]byte, error) {
	var b bytes.Buffer
	b.WriteString(snapshotMagic)
	write := func(v any) { _ = binary.Write(&b, binary.LittleEndian, v) }
	write(uint32(snapshotVersion))
	write([3]uint32{uint32(s.frameSize), uint32(s.m), uint32(s.samplingRate)})
	ints, floats, buffers := s.fields()
	write(s.adapted)
	for _, v := range ints {
		write(int64(*v))
	}
	for _, v := range floats {
		write(*v)
	}
	for _, buf := range buffers {
		write(uint32(len(buf)))
		write(buf)
	}
	return b.Bytes(), nil
}

// UnmarshalBinary restores a snapshot taken from a state with the same frame
// size, filter blocks and sampling rate. On error the state is unchanged.
func (s *State) UnmarshalBinary(data []byte) error {
	r := bytes.NewReader(data)
	read := func(v any) error { return binary.Read(r, binary.LittleEndian, v) }
	magic := make([]byte, len(snapshotMagic))
	if _, err := r.Read(magic); err != nil || string(magic) != snapshotMagic {
		return fmt.Errorf("%w: not an echo canceller snapshot", ErrSnapshot)
	}
	var version uint32
	if err := read(&version); err != nil || version != snapshotVersion {
		return fmt.Errorf("%w: version %d, want %d", ErrSnapshot, version, snapshotVersion)
	}
	var shape [3]uint32
	if err := read(&shape); err != nil {
		return fmt.Errorf("%w: truncated", ErrSnapshot)
	}
	if want := [3]uint32{uint32(s.frameSize), uint32(s.m), uint32(s.samplingRate)}; shape != want {
		return fmt.Errorf("%w: taken with frame size %d, %d filter blocks at %d Hz; this canceller has %d, %d at %d Hz",
			ErrSnapshot, shape[0], shape[1], shape[2], want[0], want[1], want[2])
	}
	// Read into a copy, so a failure part way leaves s untouched.
	next := *s
	next.e, next.x, next.xf, next.input = clone(s.e), clone(s.x), clone(s.xf), clone(s.input)
	next.y, next.lastY, next.yf, next.ef = clone(s.y), clone(s.lastY), clone(s.yf), clone(s.ef)
	next.phi, next.w, next.foreground = clone(s.phi), clone(s.w), clone(s.foreground)
	next.power, next.power1, next.wtmp = clone(s.power), clone(s.power1), clone(s.wtmp)
	next.rf, next.yfp, next.xfp, next.eh, next.yh = clone(s.rf), clone(s.yfp), clone(s.xfp), clone(s.eh), clone(s.yh)
	next.prop, next.out16 = clone(s.prop), clone(s.out16)
	ints, floats, buffers := next.fields()
	if err := read(&next.adapted); err != nil {
		return fmt.Errorf("%w: truncated", ErrSnapshot)
	}
	for _, v := range ints {
		var x int64
		if err := read(&x); err != nil {
			return fmt.Errorf("%w: truncated", ErrSnapshot)
		}
		*v = int(x)
	}
	for _, v := range floats {
		if err := read(v); err != nil {
			return fmt.Errorf("%w: truncated", ErrSnapshot)
		}
	}
	for i, buf := range buffers {
		var n uint32
		if err := read(&n); err != nil || int(n) != len(buf) {
			return fmt.Errorf("%w: buffer %d has %d values, want %d", ErrSnapshot, i, n, len(buf))
		}
		if err := read(buf); err != nil {
			return fmt.Errorf("%w: truncated", ErrSnapshot)
		}
		for _, v := range buf {
			if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
				return fmt.Errorf("%w: buffer %d holds %v", ErrSnapshot, i, v)
			}
		}
	}
	if r.Len() != 0 {
		return fmt.Errorf("%w: %d bytes left over", ErrSnapshot, r.Len())
	}
	*s = next
	return nil
}

func clone(x []float32) []float32 { return append([]float32(nil), x...) }
