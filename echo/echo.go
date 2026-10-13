// Package echo is a pure-Go port of the acoustic echo canceller in SpeexDSP
// 1.2.1 (libspeexdsp/mdf.c, float build): a multidelay block frequency-domain
// adaptive filter (AUMDF) with two-path double-talk protection.
//
// A Canceller learns the path from what the speaker played to what the
// microphone heard, and subtracts that echo from the microphone, one frame at
// a time. It handles one microphone and one speaker whose frames are already
// aligned, as a full-duplex audio device delivers them. It does not resample,
// delay-compensate or suppress residual echo; link it to a
// preprocess.Preprocessor for residual echo suppression.
//
// Samples are mono float32 in [-1, 1]. Internally they are scaled by 32768
// so upstream's constants, which are in 16-bit units, apply unchanged.
package echo

import (
	"errors"
	"fmt"

	"github.com/androiddrew/go-speexdsp/internal/mdf"
)

var (
	// ErrInvalidConfig reports a frame size, filter length or sample rate
	// that is not positive.
	ErrInvalidConfig = errors.New("invalid echo canceller configuration")
	// ErrInvalidAudio reports a frame of the wrong length or a sample that is
	// nonfinite or outside [-1, 1].
	ErrInvalidAudio = errors.New("invalid audio")
)

func init() {
	mdf.FromCanceller = func(c any) *mdf.State { return c.(*Canceller).s }
}

// Canceller removes the echo of what the speaker played from the microphone.
// It is not safe for concurrent use. A Preprocessor linked to it reads its
// state, so use the two from the same goroutine.
type Canceller struct {
	s         *mdf.State
	filterLen int
	mic, far  []float32 // the current frame in 16-bit units
	out       []float32
}

// New returns a Canceller for frames of frameSize samples that models
// filterLength samples of echo path, at sampleRate Hz. It is
// speex_echo_state_init plus SPEEX_ECHO_SET_SAMPLING_RATE.
//
// For 16 kHz voice, 256-sample frames (16 ms) and a 4000-sample filter
// (250 ms) are a good start. The filter is rounded up to whole frames, and
// its cost grows with filterLength/frameSize, so a longer filter costs more
// than a longer frame. Upstream's FFT handles any size; sizes whose doubled
// frame factors into 2, 3, 4 and 5 are fastest.
func New(frameSize, filterLength, sampleRate int) (*Canceller, error) {
	if frameSize <= 0 || filterLength <= 0 || sampleRate <= 0 {
		return nil, fmt.Errorf("%w: frame size %d, filter length %d and sample rate %d must be positive",
			ErrInvalidConfig, frameSize, filterLength, sampleRate)
	}
	s := mdf.New(frameSize, filterLength, sampleRate)
	m := (filterLength + frameSize - 1) / frameSize
	return &Canceller{
		s:         s,
		filterLen: m * frameSize,
		mic:       make([]float32, frameSize),
		far:       make([]float32, frameSize),
		out:       make([]float32, frameSize),
	}, nil
}

// Cancel writes mic with the echo of played removed into out. All three hold
// one frame. played is what the speaker played during mic's frame. out may be
// mic itself. This is speex_echo_cancellation.
//
// The output is saturated to the int16 range like upstream's, but it is not
// rounded to 16 bits. If upstream's sanity checks find the filter has
// diverged, the frame comes back silent and the canceller resets itself, as
// upstream does.
func (c *Canceller) Cancel(mic, played, out []float32) error {
	n := len(c.mic)
	if len(mic) != n || len(played) != n || len(out) != n {
		return fmt.Errorf("%w: frames are %d, %d and %d samples, want %d", ErrInvalidAudio, len(mic), len(played), len(out), n)
	}
	if err := scale(c.mic, mic); err != nil {
		return fmt.Errorf("%w: mic: %w", ErrInvalidAudio, err)
	}
	if err := scale(c.far, played); err != nil {
		return fmt.Errorf("%w: played: %w", ErrInvalidAudio, err)
	}
	c.s.Cancel(c.mic, c.far, c.out)
	for i, v := range c.out {
		out[i] = v / 32768
	}
	return nil
}

// scale converts samples in [-1, 1] to 16-bit units.
func scale(dst, src []float32) error {
	for i, v := range src {
		if !(v >= -1 && v <= 1) {
			return fmt.Errorf("sample %d is %v; samples must be finite and within [-1, 1]", i, v)
		}
		dst[i] = v * 32768
	}
	return nil
}

// Reset forgets the learned echo path and the signal history
// (speex_echo_state_reset). Like upstream, it keeps the leak estimate and the
// per-block adaptation proportions until the filter adapts again, so a reset
// canceller can differ slightly from a new one.
func (c *Canceller) Reset() { c.s.Reset() }

// FrameSize reports the samples per frame.
func (c *Canceller) FrameSize() int { return c.s.FrameSize() }

// SampleRate reports the sample rate in Hz.
func (c *Canceller) SampleRate() int { return c.s.SamplingRate() }

// FilterLength reports the modeled echo path in samples: the requested
// length rounded up to whole frames.
func (c *Canceller) FilterLength() int { return c.filterLen }

// Leak reports the canceller's estimate of how much of the echo leaks
// through, from 0 to 1 (upstream's leak_estimate). It drives residual echo
// suppression in a linked Preprocessor.
func (c *Canceller) Leak() float32 { return c.s.LeakEstimate() }

// ImpulseResponse returns the learned echo path, FilterLength taps from
// played to mic: a tap of 0.5 at index d means half of what was played comes
// back d samples later. It is for diagnostics. Upstream's
// SPEEX_ECHO_GET_IMPULSE_RESPONSE returns the same taps multiplied by
// 32767*2*FrameSize and truncated to integers.
func (c *Canceller) ImpulseResponse() []float32 { return c.s.ImpulseResponse() }
