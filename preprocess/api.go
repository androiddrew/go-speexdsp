// Package preprocess is a pure-Go port of the preprocessor in SpeexDSP 1.2.1
// (libspeexdsp/preprocess.c and filterbank.c, float build): noise
// suppression, residual echo suppression, automatic gain control and a
// speech probability.
//
// The preprocessor estimates the noise spectrum by minimum statistics and
// applies an Ephraim-Malah gain per frequency bin, smoothed over 24 Bark
// bands. Linked to an echo.Canceller, it also suppresses the echo the
// canceller left behind, using the canceller's residual echo estimate. The
// AGC scales speech frames toward a target loudness. It overlaps each frame
// with the previous one, so its output lags its input by one frame, as
// upstream's does.
//
// The speech probability steers the residual echo floor and the AGC. As a
// voice activity detector it is crude; upstream calls it a kludge, and
// Silero VAD (github.com/androiddrew/go-silero-vad) is far better.
//
// It does not resample, and upstream's dereverberation, which is unfinished,
// is not ported. Samples are mono float32 in [-1, 1]; internally they are
// scaled by 32768 so upstream's constants, which are in 16-bit units, apply
// unchanged.
package preprocess

import (
	"errors"
	"fmt"
	"math"

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

	// AGC turns automatic gain control on (SET_AGC).
	AGC bool
	// AGCLevel is the AGC's target loudness in 16-bit units, up to 32768
	// (SET_AGC_LEVEL). Zero selects 8000; values below 1 become 1, as
	// upstream.
	AGCLevel float32
	// AGCIncrement is the fastest the AGC may raise its gain, in dB/s
	// (SET_AGC_INCREMENT). Zero selects 12.
	AGCIncrement int
	// AGCDecrement is the fastest the AGC may lower its gain, in dB/s, zero
	// or negative (SET_AGC_DECREMENT). Zero selects -40.
	AGCDecrement int
	// AGCMaxGain is the largest gain the AGC may apply, in dB
	// (SET_AGC_MAX_GAIN). Zero selects upstream's initial limit, a linear
	// gain of 30 (29.5 dB), which SET_AGC_MAX_GAIN cannot express exactly.
	AGCMaxGain int

	// VAD makes Run report speech from the speech probability (SET_VAD).
	// Without it Run always reports speech, as upstream.
	VAD bool
	// ProbStart is the speech probability, in percent, above which a frame
	// starts speech (SET_PROB_START). Zero selects 35.
	ProbStart int
	// ProbContinue is the speech probability, in percent, above which speech
	// continues (SET_PROB_CONTINUE). Zero selects 20.
	ProbContinue int

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
	if !(c.AGCLevel >= 0 && c.AGCLevel <= 32768) {
		return nil, fmt.Errorf("%w: AGC level %v must be within [0, 32768]", ErrInvalidConfig, c.AGCLevel)
	}
	if c.AGCIncrement < 0 || c.AGCDecrement > 0 || c.AGCMaxGain < 0 {
		return nil, fmt.Errorf("%w: AGC increment %d and max gain %d must not be negative, decrement %d not positive",
			ErrInvalidConfig, c.AGCIncrement, c.AGCMaxGain, c.AGCDecrement)
	}
	if c.ProbStart < 0 || c.ProbStart > 100 || c.ProbContinue < 0 || c.ProbContinue > 100 {
		return nil, fmt.Errorf("%w: speech probabilities %d and %d must be percentages", ErrInvalidConfig, c.ProbStart, c.ProbContinue)
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
	st.agc = c.AGC
	if c.AGCLevel != 0 {
		st.agcLevel = max(c.AGCLevel, 1)
	}
	if c.AGCIncrement != 0 {
		st.setAGCIncrement(c.AGCIncrement)
	}
	if c.AGCDecrement != 0 {
		st.setAGCDecrement(c.AGCDecrement)
	}
	if c.AGCMaxGain != 0 {
		st.setAGCMaxGain(c.AGCMaxGain)
	}
	st.vad = c.VAD
	if c.ProbStart != 0 {
		st.speechProbStart = float32(c.ProbStart) / 100
	}
	if c.ProbContinue != 0 {
		st.speechProbContinue = float32(c.ProbContinue) / 100
	}
	if c.Echo != nil {
		st.echo = mdf.FromCanceller(c.Echo)
	}
	return &Preprocessor{st: st, buf: make([]float32, c.FrameSize)}, nil
}

// Run processes one frame in place (speex_preprocess_run) and reports
// whether it is speech; with Config.VAD off it always reports speech, as
// upstream does. The output is saturated to the int16 range like upstream's,
// but it is not rounded to 16 bits.
func (p *Preprocessor) Run(frame []float32) (speech bool, err error) {
	if err := p.load(frame); err != nil {
		return false, err
	}
	speech = p.st.run(p.buf)
	for i, v := range p.buf {
		frame[i] = v / 32768
	}
	return speech, nil
}

// Update feeds a frame into the noise estimate without processing it, for
// frames that will not be heard, such as while the near end is muted
// (speex_preprocess_estimate_update). frame is not modified.
func (p *Preprocessor) Update(frame []float32) error {
	if err := p.load(frame); err != nil {
		return err
	}
	p.st.estimateUpdate(p.buf)
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

// AGCGain reports the AGC's current gain in dB (GET_AGC_GAIN, which upstream
// rounds to an integer). It is 0 until the AGC acts.
func (p *Preprocessor) AGCGain() float32 {
	return float32(8.6858 * math.Log(float64(p.st.agcGain)))
}

// Loudness reports the AGC's speech loudness estimate in upstream's units,
// comparable to AGCLevel/1000 (GET_AGC_LOUDNESS, which upstream truncates to
// an integer).
func (p *Preprocessor) Loudness() float32 {
	return float32(math.Pow(float64(p.st.loudness), 1.0/float64(loudnessExp)))
}

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
