package echo

import (
	"math"
	"testing"

	"github.com/androiddrew/go-speexdsp/internal/fixture"
)

// TestParity runs every recorded scenario through the Canceller and
// compares it with speex_echo_cancellation from the pinned C sources. The
// float build has no rounding of its own, so the Go output must equal C's
// before C rounds it to int16 (within half an LSB), and the leak estimate
// must match bit for bit.
func TestParity(t *testing.T) {
	ref, err := fixture.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range ref.Runs {
		t.Run(r.Name, func(t *testing.T) {
			rec, err := fixture.Open(r)
			if err != nil {
				t.Fatal(err)
			}
			p := r.Pipeline
			c, err := New(p.Frame, p.Filter, ref.Rate)
			if err != nil {
				t.Fatal(err)
			}
			out := make([]float32, p.Frame)
			var worst float64
			var mismatched, leakMismatch int
			firstBad := -1
			for f := range rec.Frames {
				lo, hi := f*p.Frame, (f+1)*p.Frame
				if p.ResetFrame > 0 && f == p.ResetFrame {
					c.Reset()
				}
				if err := c.Cancel(rec.Mic[lo:hi], rec.Ref[lo:hi], out); err != nil {
					t.Fatal(err)
				}
				for i, v := range out {
					want := rec.AEC[lo+i]
					d := math.Abs(float64(v)*32768 - float64(want))
					worst = max(worst, d)
					if fixture.Round(v*32768) != want {
						mismatched++
						if firstBad < 0 {
							firstBad = f
						}
					}
				}
				if math.Float32bits(c.Leak()) != math.Float32bits(rec.Frames[f][fixture.Leak]) {
					leakMismatch++
					if firstBad < 0 {
						firstBad = f
					}
				}
			}
			t.Logf("max |go - C| = %.3f LSB over %d frames; %d samples round differently, %d leak estimates differ",
				worst, len(rec.Frames), mismatched, leakMismatch)
			if mismatched > 0 || leakMismatch > 0 {
				t.Errorf("first divergence at frame %d (%.2f s)", firstBad, float64(firstBad*p.Frame)/float64(ref.Rate))
			}
		})
	}
}
