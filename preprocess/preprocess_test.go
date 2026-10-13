package preprocess

import (
	"errors"
	"math"
	"math/rand/v2"
	"testing"

	"github.com/androiddrew/go-speexdsp/echo"
)

func TestNewRejectsBadConfig(t *testing.T) {
	c160, err := echo.New(160, 1600, 16000)
	if err != nil {
		t.Fatal(err)
	}
	c8k, err := echo.New(256, 2048, 8000)
	if err != nil {
		t.Fatal(err)
	}
	for name, cfg := range map[string]Config{
		"zero frame":            {SampleRate: 16000},
		"negative rate":         {FrameSize: 256, SampleRate: -1},
		"echo frame mismatch":   {FrameSize: 256, SampleRate: 16000, Echo: c160},
		"echo rate mismatch":    {FrameSize: 256, SampleRate: 16000, Echo: c8k},
		"nothing at all":        {},
		"zero rate, echo given": {FrameSize: 160, Echo: c160},
	} {
		if _, err := New(cfg); !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("%s: New = %v, want ErrInvalidConfig", name, err)
		}
	}
}

func TestRunRejectsBadAudio(t *testing.T) {
	p, err := New(Config{FrameSize: 4, SampleRate: 16000})
	if err != nil {
		t.Fatal(err)
	}
	for name, frame := range map[string][]float32{
		"short": {0, 0, 0},
		"long":  {0, 0, 0, 0, 0},
		"NaN":   {0, float32(math.NaN()), 0, 0},
		"+Inf":  {float32(math.Inf(1)), 0, 0, 0},
		"loud":  {0, 0, -1.01, 0},
	} {
		if err := p.Run(frame); !errors.Is(err, ErrInvalidAudio) {
			t.Errorf("%s: Run = %v, want ErrInvalidAudio", name, err)
		}
	}
	if err := p.Run([]float32{1, -1, 0.5, 0}); err != nil {
		t.Errorf("full-scale frame: %v", err)
	}
}

// TestSuppressesNoise checks that steady white noise is attenuated by about
// the configured noise floor once the estimate settles.
func TestSuppressesNoise(t *testing.T) {
	for _, db := range []int{-10, -15, -25} {
		p, err := New(Config{FrameSize: 256, SampleRate: 16000, NoiseSuppressDB: db})
		if err != nil {
			t.Fatal(err)
		}
		r := rand.New(rand.NewPCG(3, 4))
		frame := make([]float32, 256)
		var in, out float64
		for f := range 300 {
			var e float64
			for i := range frame {
				frame[i] = float32(r.NormFloat64() * 0.01)
				e += float64(frame[i]) * float64(frame[i])
			}
			if err := p.Run(frame); err != nil {
				t.Fatal(err)
			}
			if f >= 200 {
				in += e
				for _, v := range frame {
					out += float64(v) * float64(v)
				}
			}
		}
		got := 10 * math.Log10(out/in)
		t.Logf("NoiseSuppressDB %d: white noise output/input %.1f dB", db, got)
		if got > float64(db)+6 || got < float64(db)-6 {
			t.Errorf("NoiseSuppressDB %d: output/input %.1f dB, want within 6 dB of the floor", db, got)
		}
	}
}
