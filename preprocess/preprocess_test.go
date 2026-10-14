package preprocess

import (
	"errors"
	"math"
	"math/rand/v2"
	"slices"
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
		"negative AGC level":    {FrameSize: 256, SampleRate: 16000, AGCLevel: -1},
		"AGC level too high":    {FrameSize: 256, SampleRate: 16000, AGCLevel: 40000},
		"NaN AGC level":         {FrameSize: 256, SampleRate: 16000, AGCLevel: float32(math.NaN())},
		"negative increment":    {FrameSize: 256, SampleRate: 16000, AGCIncrement: -3},
		"positive decrement":    {FrameSize: 256, SampleRate: 16000, AGCDecrement: 3},
		"negative max gain":     {FrameSize: 256, SampleRate: 16000, AGCMaxGain: -1},
		"prob start over 100":   {FrameSize: 256, SampleRate: 16000, ProbStart: 101},
		"negative continue":     {FrameSize: 256, SampleRate: 16000, ProbContinue: -1},
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
		if _, err := p.Run(frame); !errors.Is(err, ErrInvalidAudio) {
			t.Errorf("%s: Run = %v, want ErrInvalidAudio", name, err)
		}
	}
	if _, err := p.Run([]float32{1, -1, 0.5, 0}); err != nil {
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
			if _, err := p.Run(frame); err != nil {
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

// TestAGCRaisesQuietSpeech checks that the AGC lifts a quiet, speech-like
// signal toward its target and that Update leaves the frame alone.
func TestAGCRaisesQuietSpeech(t *testing.T) {
	p, err := New(Config{FrameSize: 256, SampleRate: 16000, AGC: true, VAD: true})
	if err != nil {
		t.Fatal(err)
	}
	r := rand.New(rand.NewPCG(5, 6))
	frame := make([]float32, 256)
	silence := make([]float32, 256)
	for range 20 {
		for i := range silence {
			silence[i] = float32(r.NormFloat64() * 1e-4)
		}
		before := append([]float32(nil), silence...)
		if err := p.Update(silence); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(before, silence) {
			t.Fatal("Update modified the frame")
		}
	}
	var speechFrames int
	for f := range 600 {
		// A 300 Hz tone at -30 dBFS, gated like syllables.
		on := (f/20)%2 == 0
		for i := range frame {
			frame[i] = float32(r.NormFloat64() * 1e-4)
			if on {
				frame[i] += float32(0.0447 * math.Sin(2*math.Pi*300*float64(f*256+i)/16000))
			}
		}
		speech, err := p.Run(frame)
		if err != nil {
			t.Fatal(err)
		}
		if speech {
			speechFrames++
		}
	}
	t.Logf("AGC gain %.1f dB, loudness %.2f, %d of 600 frames speech", p.AGCGain(), p.Loudness(), speechFrames)
	if p.AGCGain() < 6 {
		t.Errorf("AGC gain %.1f dB, want it to raise -30 dBFS speech by at least 6 dB", p.AGCGain())
	}
	if speechFrames == 0 || speechFrames == 600 {
		t.Errorf("%d of 600 frames speech; want the gated tone to switch the VAD", speechFrames)
	}
}
