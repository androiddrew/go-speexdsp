package echo

import (
	"errors"
	"math"
	"math/rand/v2"
	"slices"
	"testing"
)

func TestNewRejectsBadConfig(t *testing.T) {
	for _, c := range []struct{ frame, filter, rate int }{
		{0, 4000, 16000}, {-1, 4000, 16000}, {256, 0, 16000}, {256, 4000, 0}, {256, -5, -1},
	} {
		if _, err := New(c.frame, c.filter, c.rate); !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("New(%d, %d, %d) = %v, want ErrInvalidConfig", c.frame, c.filter, c.rate, err)
		}
	}
}

func TestAccessors(t *testing.T) {
	c, err := New(256, 4000, 16000)
	if err != nil {
		t.Fatal(err)
	}
	if c.FrameSize() != 256 || c.SampleRate() != 16000 || c.FilterLength() != 4096 {
		t.Errorf("FrameSize, SampleRate, FilterLength = %d, %d, %d; want 256, 16000, 4096",
			c.FrameSize(), c.SampleRate(), c.FilterLength())
	}
	if n := len(c.ImpulseResponse()); n != 4096 {
		t.Errorf("ImpulseResponse has %d taps, want 4096", n)
	}
}

func TestCancelRejectsBadAudio(t *testing.T) {
	c, err := New(4, 8, 16000)
	if err != nil {
		t.Fatal(err)
	}
	good := []float32{0, 0.5, -0.5, 1}
	nan := float32(math.NaN())
	for name, tc := range map[string]struct{ mic, played, out []float32 }{
		"short mic":    {good[:3], good, make([]float32, 4)},
		"long played":  {good, append(slices.Clone(good), 0), make([]float32, 4)},
		"nil out":      {good, good, nil},
		"NaN mic":      {[]float32{0, nan, 0, 0}, good, make([]float32, 4)},
		"loud played":  {good, []float32{0, 1.0001, 0, 0}, make([]float32, 4)},
		"-Inf played":  {good, []float32{float32(math.Inf(-1)), 0, 0, 0}, make([]float32, 4)},
		"below -1 mic": {[]float32{-1.5, 0, 0, 0}, good, make([]float32, 4)},
	} {
		if err := c.Cancel(tc.mic, tc.played, tc.out); !errors.Is(err, ErrInvalidAudio) {
			t.Errorf("%s: Cancel = %v, want ErrInvalidAudio", name, err)
		}
	}
	if err := c.Cancel(good, good, make([]float32, 4)); err != nil {
		t.Errorf("full-scale frame: %v", err)
	}
}

// noiseFrames returns n frames of white noise and its echo: half the noise,
// 100 samples later.
func noiseFrames(n, frame int) (mic, played [][]float32) {
	r := rand.New(rand.NewPCG(1, 2))
	var hist []float32
	for range n {
		m, p := make([]float32, frame), make([]float32, frame)
		for i := range p {
			p[i] = float32(r.NormFloat64() * 0.1)
			hist = append(hist, p[i])
			if d := len(hist) - 1 - 100; d >= 0 {
				m[i] = 0.5 * hist[d]
			}
		}
		mic, played = append(mic, m), append(played, p)
	}
	return mic, played
}

func energy(x []float32) float64 {
	var e float64
	for _, v := range x {
		e += float64(v) * float64(v)
	}
	return e
}

// TestLearnsEchoPath checks convergence on a linear echo path: a pure
// 100-sample delay at half amplitude.
func TestLearnsEchoPath(t *testing.T) {
	const frame = 256
	mic, played := noiseFrames(400, frame)
	c, err := New(frame, 2048, 16000)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]float32, frame)
	var in, rest float64
	for f := range mic {
		if err := c.Cancel(mic[f], played[f], out); err != nil {
			t.Fatal(err)
		}
		if f >= 300 {
			in += energy(mic[f])
			rest += energy(out)
		}
	}
	erle := 10 * math.Log10(in/rest)
	ir := c.ImpulseResponse()
	peak := 0
	for i := range ir {
		if math.Abs(float64(ir[i])) > math.Abs(float64(ir[peak])) {
			peak = i
		}
	}
	t.Logf("ERLE over the last 100 frames %.1f dB; impulse response peak %.3f at %d", erle, ir[peak], peak)
	if erle < 20 {
		t.Errorf("ERLE = %.1f dB, want at least 20 dB on a linear echo path", erle)
	}
	if peak != 100 || math.Abs(float64(ir[peak])-0.5) > 0.05 {
		t.Errorf("impulse response peak %.3f at %d, want about 0.5 at 100", ir[peak], peak)
	}
}

// TestInPlace checks that out may be mic itself.
func TestInPlace(t *testing.T) {
	const frame = 160
	mic, played := noiseFrames(50, frame)
	a, _ := New(frame, 1600, 16000)
	b, _ := New(frame, 1600, 16000)
	out := make([]float32, frame)
	for f := range mic {
		if err := a.Cancel(mic[f], played[f], out); err != nil {
			t.Fatal(err)
		}
		buf := slices.Clone(mic[f])
		if err := b.Cancel(buf, played[f], buf); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(out, buf) {
			t.Fatalf("frame %d: in-place output differs", f)
		}
	}
}

// TestReset checks that a canceller reset before it has adapted behaves like
// a new one. After adaptation upstream's reset keeps the per-block
// adaptation proportions; TestParity's path_change_reset run covers that.
func TestReset(t *testing.T) {
	const frame = 256
	mic, played := noiseFrames(80, frame)
	used, _ := New(frame, 2048, 16000)
	out := make([]float32, frame)
	for f := range 3 {
		if err := used.Cancel(mic[f], played[f], out); err != nil {
			t.Fatal(err)
		}
	}
	if used.s.Adapted() {
		t.Fatal("adapted after 3 frames; the test needs a reset before adaptation")
	}
	used.Reset()
	fresh, _ := New(frame, 2048, 16000)
	want := make([]float32, frame)
	for f := 3; f < 80; f++ {
		if err := used.Cancel(mic[f], played[f], out); err != nil {
			t.Fatal(err)
		}
		if err := fresh.Cancel(mic[f], played[f], want); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(out, want) {
			t.Fatalf("frame %d after Reset differs from a new canceller", f)
		}
	}
}

func BenchmarkCancel(b *testing.B) {
	for _, bc := range []struct {
		name          string
		frame, filter int
	}{{"256x4000", 256, 4000}, {"160x4000", 160, 4000}, {"256x2048", 256, 2048}} {
		b.Run(bc.name, func(b *testing.B) {
			mic, played := noiseFrames(16, bc.frame)
			c, _ := New(bc.frame, bc.filter, 16000)
			out := make([]float32, bc.frame)
			f := 0
			for b.Loop() {
				if err := c.Cancel(mic[f%16], played[f%16], out); err != nil {
					b.Fatal(err)
				}
				f++
			}
		})
	}
}
