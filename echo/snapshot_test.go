package echo

import (
	"encoding/binary"
	"errors"
	"math"
	"slices"
	"testing"
)

// run passes frames through c and returns the outputs.
func run(t *testing.T, c *Canceller, mic, played [][]float32) [][]float32 {
	t.Helper()
	var outs [][]float32
	for f := range mic {
		out := make([]float32, len(mic[f]))
		if err := c.Cancel(mic[f], played[f], out); err != nil {
			t.Fatal(err)
		}
		outs = append(outs, out)
	}
	return outs
}

func newCanceller(t *testing.T, frame, filter, rate int) *Canceller {
	t.Helper()
	c, err := New(frame, filter, rate)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// TestSnapshotContinuesExactly: a canceller restored from a snapshot
// produces exactly what the original produces from then on.
func TestSnapshotContinuesExactly(t *testing.T) {
	const frame = 256
	mic, played := noiseFrames(300, frame)
	a := newCanceller(t, frame, 2048, 16000)
	run(t, a, mic[:150], played[:150])
	snap := a.Snapshot()
	want := run(t, a, mic[150:], played[150:])

	b := newCanceller(t, frame, 2048, 16000)
	run(t, b, mic[:20], played[:20]) // a different history, overwritten
	if err := b.Restore(snap); err != nil {
		t.Fatal(err)
	}
	got := run(t, b, mic[150:], played[150:])
	for f := range want {
		if !slices.Equal(got[f], want[f]) {
			t.Fatalf("frame %d differs after restoring", 150+f)
		}
	}
	if !slices.Equal(a.ImpulseResponse(), b.ImpulseResponse()) || a.Leak() != b.Leak() {
		t.Error("the learned echo path differs after restoring")
	}
	t.Logf("snapshot of a %d-sample filter at %d-sample frames: %d bytes", a.FilterLength(), frame, len(snap))
}

// TestSnapshotStartsConverged: restored into a new canceller, as a later
// session would, the learned echo path removes the echo from the first
// frames, where a new canceller is still learning.
func TestSnapshotStartsConverged(t *testing.T) {
	const frame = 256
	mic, played := noiseFrames(400, frame)
	trained := newCanceller(t, frame, 2048, 16000)
	run(t, trained, mic[:300], played[:300])
	snap := trained.Snapshot()

	erle := func(c *Canceller) float64 {
		outs := run(t, c, mic[300:330], played[300:330])
		var in, out float64
		for f := range outs {
			in += energy(mic[300+f])
			out += energy(outs[f])
		}
		return 10 * math.Log10(in/out)
	}
	restored := newCanceller(t, frame, 2048, 16000)
	if err := restored.Restore(snap); err != nil {
		t.Fatal(err)
	}
	warm, cold := erle(restored), erle(newCanceller(t, frame, 2048, 16000))
	t.Logf("echo removed over the first 30 frames: %.1f dB restored, %.1f dB new", warm, cold)
	if warm < 20 || warm < cold+10 {
		t.Errorf("restored %.1f dB, new %.1f dB", warm, cold)
	}
}

func TestSnapshotRejects(t *testing.T) {
	src := newCanceller(t, 64, 256, 16000)
	mic, played := noiseFrames(20, 64)
	run(t, src, mic, played)
	snap := src.Snapshot()

	for _, c := range []struct{ frame, filter, rate int }{{128, 256, 16000}, {64, 512, 16000}, {64, 256, 8000}} {
		if err := newCanceller(t, c.frame, c.filter, c.rate).Restore(snap); !errors.Is(err, ErrInvalidSnapshot) {
			t.Errorf("into %d/%d/%d: %v", c.frame, c.filter, c.rate, err)
		}
	}
	corrupt := func(edit func([]byte)) []byte {
		b := slices.Clone(snap)
		edit(b)
		return b
	}
	nan := math.Float32bits(float32(math.NaN()))
	for name, bad := range map[string][]byte{
		"empty":       nil,
		"magic":       corrupt(func(b []byte) { b[0] = 'X' }),
		"version":     corrupt(func(b []byte) { binary.LittleEndian.PutUint32(b[4:], 99) }),
		"extra bytes": append(slices.Clone(snap), 0),
		"length":      corrupt(func(b []byte) { binary.LittleEndian.PutUint32(b[len(b)-4*64-4:], 7) }),
		"NaN":         corrupt(func(b []byte) { binary.LittleEndian.PutUint32(b[len(b)-4:], nan) }),
	} {
		assertRejected(t, name, bad)
	}
	for n := range len(snap) { // every truncation
		assertRejected(t, "truncated", snap[:n])
	}
}

// assertRejected restores bad into a canceller and checks it fails and
// leaves the canceller as it was.
func assertRejected(t *testing.T, name string, bad []byte) {
	t.Helper()
	c := newCanceller(t, 64, 256, 16000)
	mic, played := noiseFrames(10, 64)
	run(t, c, mic, played)
	before := c.Snapshot()
	if err := c.Restore(bad); !errors.Is(err, ErrInvalidSnapshot) {
		t.Fatalf("%s (%d bytes): %v, want ErrInvalidSnapshot", name, len(bad), err)
	}
	if !slices.Equal(c.Snapshot(), before) {
		t.Fatalf("%s: a failed restore changed the canceller", name)
	}
}
