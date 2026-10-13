// Package preprocess is a pure-Go port of the preprocessor in SpeexDSP 1.2.1
// (libspeexdsp/preprocess.c and filterbank.c, float build): noise
// suppression and residual echo suppression.
//
// The preprocessor estimates the noise spectrum by minimum statistics and
// applies an Ephraim-Malah gain per frequency bin, smoothed over 24 Bark
// bands. Linked to an echo.Canceller, it also suppresses the echo the
// canceller left behind, using the canceller's residual echo estimate. It
// works on overlapping frames, so its output lags its input by one frame's
// worth of window overlap, as upstream's does.
//
// It does not resample, and upstream's dereverberation, which is unfinished,
// is not ported. Samples are mono float32 in [-1, 1]; internally they are
// scaled by 32768 so upstream's constants, which are in 16-bit units, apply
// unchanged.
package preprocess

import (
	"errors"
	"fmt"

	"github.com/androiddrew/go-speexdsp/echo"
	"github.com/androiddrew/go-speexdsp/internal/mdf"
)

var (
	// ErrInvalidConfig reports a Config the preprocessor cannot run with.
	ErrInvalidConfig = errors.New("invalid preprocessor configuration")
	// ErrInvalidAudio reports a frame of the wrong length or a sample that is
	// nonfinite or outside [-1, 1].
	ErrInvalidAudio = errors.New("invalid audio")
)

// Config configures a Preprocessor. Zero values select upstream's defaults.
type Config struct {
	// FrameSize is the samples per frame; required. 10 to 20 ms works best.
	FrameSize int
	// SampleRate is in Hz; required.
	SampleRate int

	// DisableDenoise turns noise suppression off (SET_DENOISE 0). Upstream
	// applies residual echo suppression through the same gains, so this
	// turns that off too.
	DisableDenoise bool
	// NoiseSuppressDB is the maximum noise attenuation in dB
	// (SET_NOISE_SUPPRESS). Zero selects -15. As upstream, the sign is
	// ignored: 10 means -10 dB.
	NoiseSuppressDB int
	// EchoSuppressDB is the maximum residual echo attenuation in dB when
	// the near end is silent (SET_ECHO_SUPPRESS). Zero selects -40.
	EchoSuppressDB int
	// EchoSuppressActiveDB is the maximum residual echo attenuation in dB
	// while the near end talks (SET_ECHO_SUPPRESS_ACTIVE). Zero selects -15.
	EchoSuppressActiveDB int

	// Echo links a canceller for residual echo suppression
	// (SET_ECHO_STATE). Run the canceller on each frame before the
	// preprocessor, with the same frame size and sample rate.
	Echo *echo.Canceller
}

// Preprocessor suppresses noise and residual echo in a stream of frames. It
// is not safe for concurrent use, and a linked Canceller must be used from
// the same goroutine.
type Preprocessor struct {
	st  *state
	buf []float32
}

// New returns a Preprocessor (speex_preprocess_state_init and its ctls).
func New(c Config) (*Preprocessor, error) {
	if c.FrameSize <= 0 || c.SampleRate <= 0 {
		return nil, fmt.Errorf("%w: frame size %d and sample rate %d must be positive", ErrInvalidConfig, c.FrameSize, c.SampleRate)
	}
	if c.Echo != nil && (c.Echo.FrameSize() != c.FrameSize || c.Echo.SampleRate() != c.SampleRate) {
		return nil, fmt.Errorf("%w: the canceller runs %d-sample frames at %d Hz, the preprocessor %d at %d Hz",
			ErrInvalidConfig, c.Echo.FrameSize(), c.Echo.SampleRate(), c.FrameSize, c.SampleRate)
	}
	st := newState(c.FrameSize, c.SampleRate)
	st.denoise = !c.DisableDenoise
	if c.NoiseSuppressDB != 0 {
		st.noiseSuppress = -abs(c.NoiseSuppressDB)
	}
	if c.EchoSuppressDB != 0 {
		st.echoSuppress = -abs(c.EchoSuppressDB)
	}
	if c.EchoSuppressActiveDB != 0 {
		st.echoSuppressActive = -abs(c.EchoSuppressActiveDB)
	}
	if c.Echo != nil {
		st.echo = mdf.FromCanceller(c.Echo)
	}
	return &Preprocessor{st: st, buf: make([]float32, c.FrameSize)}, nil
}

// Run processes one frame in place (speex_preprocess_run). The output is
// saturated to the int16 range like upstream's, but it is not rounded to 16
// bits.
func (p *Preprocessor) Run(frame []float32) error {
	if err := p.load(frame); err != nil {
		return err
	}
	p.st.run(p.buf)
	for i, v := range p.buf {
		frame[i] = v / 32768
	}
	return nil
}

// load validates frame and scales it into p.buf.
func (p *Preprocessor) load(frame []float32) error {
	if len(frame) != len(p.buf) {
		return fmt.Errorf("%w: frame is %d samples, want %d", ErrInvalidAudio, len(frame), len(p.buf))
	}
	for i, v := range frame {
		if !(v >= -1 && v <= 1) {
			return fmt.Errorf("%w: sample %d is %v; samples must be finite and within [-1, 1]", ErrInvalidAudio, i, v)
		}
		p.buf[i] = v * 32768
	}
	return nil
}

// FrameSize reports the samples per frame.
func (p *Preprocessor) FrameSize() int { return p.st.frameSize }

// SpeechProbability reports the last frame's probability of speech, 0 to 1
// (GET_PROB, which upstream rounds to a percentage). Silero VAD is a much
// better speech detector; this one mostly matters because it steers the
// residual echo floor and the AGC.
func (p *Preprocessor) SpeechProbability() float32 { return p.st.speechProb }

// PSD returns the last frame's power spectrum, FrameSize bins, in upstream's
// units: 16-bit samples, FFT scaled by 1/(2*FrameSize) (GET_PSD without its
// integer conversion).
func (p *Preprocessor) PSD() []float32 {
	return append([]float32(nil), p.st.ps[:p.st.psSize]...)
}

// NoisePSD returns the current noise estimate in PSD's units (GET_NOISE_PSD
// without its integer conversion).
func (p *Preprocessor) NoisePSD() []float32 {
	return append([]float32(nil), p.st.noise[:p.st.psSize]...)
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
