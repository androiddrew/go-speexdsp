package preprocess

import (
	"math"
	"testing"

	"github.com/androiddrew/go-speexdsp/echo"
	"github.com/androiddrew/go-speexdsp/internal/fixture"
)

// newRun builds the canceller and preprocessor a fixture run used.
func newRun(t *testing.T, rate int, p fixture.Pipeline) (*echo.Canceller, *Preprocessor) {
	t.Helper()
	c, err := echo.New(p.Frame, p.Filter, rate)
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{
		FrameSize:            p.Frame,
		SampleRate:           rate,
		DisableDenoise:       !p.Denoise,
		NoiseSuppressDB:      p.NoiseSuppress,
		EchoSuppressDB:       p.EchoSuppress,
		EchoSuppressActiveDB: p.EchoActive,
	}
	if p.LinkEcho {
		cfg.Echo = c
	}
	pp, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return c, pp
}

// TestParity runs every recorded scenario through the canceller and the
// preprocessor and compares the preprocessor with speex_preprocess_run from
// the pinned C sources. Like C, the preprocessor gets the canceller's
// output rounded to int16. The output must equal C's before C rounds it
// (within half an LSB), and the speech probability must match bit for bit.
func TestParity(t *testing.T) {
	ref, err := fixture.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range ref.Runs {
		t.Run(r.Name, func(t *testing.T) {
			p := r.Pipeline
			if p.AGC {
				t.Skip("AGC is not ported yet")
			}
			rec, err := fixture.Open(r)
			if err != nil {
				t.Fatal(err)
			}
			c, pp := newRun(t, ref.Rate, p)
			frame := make([]float32, p.Frame)
			var worst float64
			var mismatched, probMismatch int
			firstBad := -1
			for f := range rec.Frames {
				lo, hi := f*p.Frame, (f+1)*p.Frame
				if p.ResetFrame > 0 && f == p.ResetFrame {
					c.Reset()
				}
				if err := c.Cancel(rec.Mic[lo:hi], rec.Ref[lo:hi], frame); err != nil {
					t.Fatal(err)
				}
				for i, v := range frame {
					frame[i] = float32(fixture.Round(v*32768)) / 32768
				}
				if err := pp.Run(frame); err != nil {
					t.Fatal(err)
				}
				for i, v := range frame {
					want := rec.PP[lo+i]
					worst = max(worst, math.Abs(float64(v)*32768-float64(want)))
					if fixture.Round(v*32768) != want {
						mismatched++
						if firstBad < 0 {
							firstBad = f
						}
					}
				}
				if math.Float32bits(pp.SpeechProbability()) != math.Float32bits(rec.Frames[f][fixture.Prob]) {
					probMismatch++
					if firstBad < 0 {
						firstBad = f
					}
				}
			}
			t.Logf("max |go - C| = %.3f LSB over %d frames; %d samples round differently, %d speech probabilities differ",
				worst, len(rec.Frames), mismatched, probMismatch)
			if mismatched > 0 || probMismatch > 0 {
				t.Errorf("first divergence at frame %d (%.2f s)", firstBad, float64(firstBad*p.Frame)/float64(ref.Rate))
			}
		})
	}
}
