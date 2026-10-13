// Package fft is a port of SpeexDSP 1.2.1's float FFT: smallft.c (FFTPACK
// from Ogg Vorbis) wrapped with fftwrap.c's USE_SMALLFT scaling.
//
// The spectrum uses FFTPACK's packed half-complex layout, which the echo
// canceller and preprocessor index directly: out[0] is the DC term, then
// out[2k-1], out[2k] are the real and imaginary parts of bin k, and for even
// n, out[n-1] is the real Nyquist term.
//
// The forward transform scales by 1/n; the inverse does not, so
// Inverse(Forward(x)) returns x.
package fft

// FFT holds the twiddle tables and scratch space for one size. It is not
// safe for concurrent use.
type FFT struct {
	l     *drft
	scale float32
}

// New returns an FFT of n points (spx_fft_init). n must be positive.
func New(n int) *FFT {
	if n < 1 {
		panic("fft: size must be positive")
	}
	// fftwrap.c computes 1./n in double and stores it in a float.
	return &FFT{l: newDrft(n), scale: float32(1.0 / float64(n))}
}

// N reports the transform size.
func (f *FFT) N() int { return f.l.n }

// Forward writes the scaled spectrum of in to out (spx_fft). in and out must
// both hold N values and must not be the same slice.
func (f *FFT) Forward(in, out []float32) {
	n := f.l.n
	in, out = in[:n], out[:n]
	for i := range out {
		out[i] = f.scale * in[i]
	}
	f.l.forward(out)
}

// Inverse writes the signal for the packed spectrum in to out (spx_ifft). in
// and out must both hold N values and must not be the same slice.
func (f *FFT) Inverse(in, out []float32) {
	n := f.l.n
	copy(out[:n], in[:n])
	f.l.backward(out[:n])
}
