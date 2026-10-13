// Ported from SpeexDSP 1.2.1 libspeexdsp/mdf.c, which carries this notice:
//
//	Copyright (C) 2003-2008 Jean-Marc Valin
//
//	Redistribution and use in source and binary forms, with or without
//	modification, are permitted provided that the following conditions are
//	met:
//
//	1. Redistributions of source code must retain the above copyright notice,
//	this list of conditions and the following disclaimer.
//
//	2. Redistributions in binary form must reproduce the above copyright
//	notice, this list of conditions and the following disclaimer in the
//	documentation and/or other materials provided with the distribution.
//
//	3. The name of the author may not be used to endorse or promote products
//	derived from this software without specific prior written permission.
//
//	THIS SOFTWARE IS PROVIDED BY THE AUTHOR ``AS IS'' AND ANY EXPRESS OR
//	IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE IMPLIED WARRANTIES
//	OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
//	DISCLAIMED. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR ANY DIRECT,
//	INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES
//	(INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR
//	SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION)
//	HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT,
//	STRICT LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN
//	ANY WAY OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE
//	POSSIBILITY OF SUCH DAMAGE.

// Package mdf is the echo canceller of SpeexDSP 1.2.1 (mdf.c), float build,
// TWO_PATH, one microphone and one speaker. Package echo wraps it; package
// preprocess reaches it through FromCanceller for residual echo estimation.
//
// Signals are in 16-bit units (a full-scale sample is 32768), as upstream.
// Every expression keeps C's precision: float32 where C uses float, float64
// where a double literal or a double libm call promotes it. Products that
// feed sums are wrapped in float32() or float64() so the Go compiler cannot
// fuse them into multiply-adds.
package mdf

import (
	"math"

	"github.com/androiddrew/go-speexdsp/internal/fft"
)

// Two-path filter constants (mdf.c, float build).
const (
	minLeak      = float32(.005)
	var1Smooth   = float32(.36)
	var2Smooth   = float32(.7225)
	var1Update   = float32(.5)
	var2Update   = float32(.25)
	varBacktrack = float32(4)
)

// State is SpeexEchoState for C = K = 1.
type State struct {
	frameSize, windowSize, m int
	cancelCount              int
	adapted                  bool
	saturated, screwedUp     int
	samplingRate             int
	specAverage              float32
	beta0, betaMax           float32
	sumAdapt                 float32
	leakEstimate             float32

	e, x, xf, input, y, lastY, yf, ef []float32 // e, x, X, input, y, last_y, Y, E
	phi, w, foreground                []float32
	davg1, davg2, dvar1, dvar2        float32
	power, power1, wtmp               []float32
	rf, yfp, xfp, eh, yh              []float32 // Rf, Yf, Xf, Eh, Yh
	pey, pyy                          float32
	window, prop                      []float32
	fft                               *fft.FFT
	memX, memD, memE                  float32
	preemph, notchRadius              float32
	notchMem                          [2]float32

	out16 []float32 // the int16 output upstream feeds back into last_y
}

// New is speex_echo_state_init followed by SPEEX_ECHO_SET_SAMPLING_RATE.
// The caller validates the arguments.
func New(frameSize, filterLength, samplingRate int) *State {
	n := 2 * frameSize
	m := (filterLength + frameSize - 1) / frameSize
	s := &State{
		frameSize:  frameSize,
		windowSize: n,
		m:          m,
		fft:        fft.New(n),
		e:          make([]float32, n),
		x:          make([]float32, n),
		input:      make([]float32, frameSize),
		y:          make([]float32, n),
		lastY:      make([]float32, n),
		yfp:        make([]float32, frameSize+1),
		rf:         make([]float32, frameSize+1),
		xfp:        make([]float32, frameSize+1),
		yh:         make([]float32, frameSize+1),
		eh:         make([]float32, frameSize+1),
		xf:         make([]float32, (m+1)*n),
		yf:         make([]float32, n),
		ef:         make([]float32, n),
		w:          make([]float32, m*n),
		foreground: make([]float32, m*n),
		phi:        make([]float32, n),
		power:      make([]float32, frameSize+1),
		power1:     make([]float32, frameSize+1),
		window:     make([]float32, n),
		prop:       make([]float32, m),
		wtmp:       make([]float32, n),
		out16:      make([]float32, frameSize),
	}
	s.setSamplingRate(samplingRate)

	for i := range n {
		s.window[i] = float32(.5 - float64(.5*math.Cos(2*math.Pi*float64(i)/float64(n))))
	}
	for i := range s.power1 {
		s.power1[i] = 1
	}
	// Ratio of ~10 between adaptation rate of first and last block.
	decay := float32(math.Exp(float64(-(float32(2.4) / float32(m)))))
	s.prop[0] = float32(.7)
	sum := s.prop[0]
	for i := 1; i < m; i++ {
		s.prop[i] = s.prop[i-1] * decay
		sum += s.prop[i]
	}
	for i := m - 1; i >= 0; i-- {
		s.prop[i] = (float32(.8) * s.prop[i]) / sum
	}
	s.preemph = float32(.9)
	s.pey, s.pyy = 1, 1
	return s
}

// setSamplingRate is SPEEX_ECHO_SET_SAMPLING_RATE.
func (s *State) setSamplingRate(rate int) {
	s.samplingRate = rate
	s.specAverage = float32(s.frameSize) / float32(rate)
	s.beta0 = (2 * float32(s.frameSize)) / float32(rate)
	s.betaMax = (.5 * float32(s.frameSize)) / float32(rate)
	switch {
	case rate < 12000:
		s.notchRadius = float32(.9)
	case rate < 24000:
		s.notchRadius = float32(.982)
	default:
		s.notchRadius = float32(.992)
	}
}

// FrameSize is SPEEX_ECHO_GET_FRAME_SIZE.
func (s *State) FrameSize() int { return s.frameSize }

// SamplingRate is SPEEX_ECHO_GET_SAMPLING_RATE.
func (s *State) SamplingRate() int { return s.samplingRate }

// LeakEstimate is the echo path's estimated leak (Pey/Pyy), 0 to 1.
func (s *State) LeakEstimate() float32 { return s.leakEstimate }

// Adapted reports whether the filter has passed its initial adaptation.
func (s *State) Adapted() bool { return s.adapted }

// Reset is speex_echo_state_reset. Like upstream it leaves leak_estimate,
// the second half of last_y and the scratch buffers alone.
func (s *State) Reset() {
	s.cancelCount = 0
	s.screwedUp = 0
	clear(s.w)
	clear(s.foreground)
	clear(s.xf)
	clear(s.power)
	for i := range s.power1 {
		s.power1[i] = 1
	}
	clear(s.eh)
	clear(s.yh)
	clear(s.lastY[:s.frameSize])
	clear(s.ef)
	clear(s.x)
	s.notchMem = [2]float32{}
	s.memD, s.memE, s.memX = 0, 0, 0
	s.saturated = 0
	s.adapted = false
	s.sumAdapt = 0
	s.pey, s.pyy = 1, 1
	s.davg1, s.davg2 = 0, 0
	s.dvar1, s.dvar2 = 0, 0
}

// filterDCNotch16 is filter_dc_notch16 with stride 1.
func filterDCNotch16(in []float32, radius float32, out []float32, mem *[2]float32) {
	den2 := float32(float64(radius*radius) + float64(.7*float64(1-radius)*float64(1-radius)))
	for i := range out {
		vin := in[i]
		vout := mem[0] + vin
		mem[0] = mem[1] + 2*(-vin+float32(radius*vout))
		mem[1] = vin - float32(den2*vout)
		out[i] = radius * vout
	}
}

// innerProd is mdf_inner_prod.
func innerProd(x, y []float32, n int) float32 {
	var sum float32
	for i := 0; i+1 < n; i += 2 {
		part := float32(x[i] * y[i])
		part += float32(x[i+1] * y[i+1])
		sum += part
	}
	return sum
}

// powerSpectrum is power_spectrum.
func powerSpectrum(x, ps []float32, n int) {
	ps[0] = x[0] * x[0]
	i, j := 1, 1
	for ; i < n-1; i, j = i+2, j+1 {
		ps[j] = float32(x[i]*x[i]) + float32(x[i+1]*x[i+1])
	}
	ps[j] = x[i] * x[i]
}

// powerSpectrumAccum is power_spectrum_accum.
func powerSpectrumAccum(x, ps []float32, n int) {
	ps[0] += float32(x[0] * x[0])
	i, j := 1, 1
	for ; i < n-1; i, j = i+2, j+1 {
		ps[j] += float32(x[i]*x[i]) + float32(x[i+1]*x[i+1])
	}
	ps[j] += float32(x[i] * x[i])
}

// spectralMulAccum is spectral_mul_accum (and spectral_mul_accum16, which is
// the same function in the float build).
func spectralMulAccum(x, y, acc []float32, n, m int) {
	clear(acc[:n])
	for j := range m {
		xj, yj := x[j*n:(j+1)*n], y[j*n:(j+1)*n]
		acc[0] += float32(xj[0] * yj[0])
		i := 1
		for ; i < n-1; i += 2 {
			acc[i] += float32(xj[i]*yj[i]) - float32(xj[i+1]*yj[i+1])
			acc[i+1] += float32(xj[i+1]*yj[i]) + float32(xj[i]*yj[i+1])
		}
		acc[i] += float32(xj[i] * yj[i])
	}
}

// weightedSpectralMulConj is weighted_spectral_mul_conj.
func weightedSpectralMulConj(w []float32, p float32, x, y, prod []float32, n int) {
	ww := p * w[0]
	prod[0] = ww * (x[0] * y[0])
	i, j := 1, 1
	for ; i < n-1; i, j = i+2, j+1 {
		ww = p * w[j]
		prod[i] = ww * (float32(x[i]*y[i]) + float32(x[i+1]*y[i+1]))
		prod[i+1] = ww * (float32(-x[i+1]*y[i]) + float32(x[i]*y[i+1]))
	}
	ww = p * w[j]
	prod[i] = ww * (x[i] * y[i])
}

// adjustProp is mdf_adjust_prop for one channel pair.
func adjustProp(w []float32, n, m int, prop []float32) {
	maxSum := float32(1)
	propSum := float32(1)
	for i := range m {
		tmp := float32(1)
		for _, v := range w[i*n : (i+1)*n] {
			tmp += float32(v * v)
		}
		prop[i] = float32(math.Sqrt(float64(tmp)))
		if prop[i] > maxSum {
			maxSum = prop[i]
		}
	}
	for i := range m {
		prop[i] += float32(float32(.1) * maxSum)
		propSum += prop[i]
	}
	for i := range m {
		prop[i] = (float32(.99) * prop[i]) / propSum
	}
}

// word2int is WORD2INT: round half up and saturate to int16.
func word2int(x float32) float32 {
	switch {
	case x < -32767.5:
		return -32768
	case x > 32766.5:
		return 32767
	}
	return float32(math.Floor(.5 + float64(x)))
}

// Cancel is speex_echo_cancellation. in and far are one frame each in 16-bit
// units; out receives the echo-cancelled frame, saturated to the int16 range
// like upstream's WORD2INT but not rounded. out must not alias in.
func (s *State) Cancel(in, far, out []float32) {
	n := s.windowSize
	m := s.m
	fs := s.frameSize

	s.cancelCount++
	ss := float32(.35 / float64(m))
	ss1 := 1 - ss

	// Apply a notch filter to make sure DC doesn't end up causing problems.
	filterDCNotch16(in, s.notchRadius, s.input, &s.notchMem)
	// Copy input data to buffer and apply pre-emphasis.
	for i := range fs {
		tmp32 := s.input[i] - float32(s.preemph*s.memD)
		s.memD = s.input[i]
		s.input[i] = tmp32
	}

	for i := range fs {
		s.x[i] = s.x[i+fs]
		tmp32 := far[i] - float32(s.preemph*s.memX)
		s.x[i+fs] = tmp32
		s.memX = far[i]
	}

	// Shift memory and convert x (echo input) to the frequency domain.
	copy(s.xf[n:], s.xf[:m*n])
	s.fft.Forward(s.x, s.xf[:n])

	sxx := innerProd(s.x[fs:], s.x[fs:], fs)
	powerSpectrumAccum(s.xf[:n], s.xfp, n)

	// Compute foreground filter.
	spectralMulAccum(s.xf, s.foreground, s.yf, n, m)
	s.fft.Inverse(s.yf, s.e)
	for i := range fs {
		s.e[i] = s.input[i] - s.e[i+fs]
	}
	sff := innerProd(s.e, s.e, fs)

	// Adjust proportional adaption rate.
	if s.adapted {
		adjustProp(s.w, n, m, s.prop)
	}
	// Compute weight gradient.
	if s.saturated == 0 {
		for j := m - 1; j >= 0; j-- {
			weightedSpectralMulConj(s.power1, s.prop[j], s.xf[(j+1)*n:], s.ef, s.phi, n)
			wj := s.w[j*n : (j+1)*n]
			for i := range n {
				wj[i] += s.phi[i]
			}
		}
	} else {
		s.saturated--
	}

	// Update weight to prevent circular convolution (AUMDF).
	for j := range m {
		if j == 0 || s.cancelCount%(m-1) == j-1 {
			wj := s.w[j*n : (j+1)*n]
			s.fft.Inverse(wj, s.wtmp)
			clear(s.wtmp[fs:n])
			s.fft.Forward(s.wtmp, wj)
		}
	}

	// So we can use power_spectrum_accum.
	clear(s.rf)
	clear(s.yfp)
	clear(s.xfp)

	// Difference in response, used to estimate the variance of our residual
	// power estimate.
	var dbf, see float32
	spectralMulAccum(s.xf, s.w, s.yf, n, m)
	s.fft.Inverse(s.yf, s.y)
	for i := range fs {
		s.e[i] = s.e[i+fs] - s.y[i+fs]
	}
	dbf += 10 + innerProd(s.e, s.e, fs)
	for i := range fs {
		s.e[i] = s.input[i] - s.y[i+fs]
	}
	see += innerProd(s.e, s.e, fs)

	// Logic for updating the foreground filter: for two time windows,
	// compute the mean of the energy difference and its variance.
	s.davg1 = float32(float32(.6)*s.davg1) + float32(float32(.4)*(sff-see))
	s.davg2 = float32(float32(.85)*s.davg2) + float32(float32(.15)*(sff-see))
	s.dvar1 = float32(var1Smooth*s.dvar1) + float32((float32(.4)*sff)*(float32(.4)*dbf))
	s.dvar2 = float32(var2Smooth*s.dvar2) + float32((float32(.15)*sff)*(float32(.15)*dbf))

	// Check if we have a statistically significant reduction in the residual
	// echo. This is *not* Gaussian, so be careful about the longer tail.
	updateForeground := false
	switch {
	case (sff-see)*abs32(sff-see) > sff*dbf:
		updateForeground = true
	case s.davg1*abs32(s.davg1) > var1Update*s.dvar1:
		updateForeground = true
	case s.davg2*abs32(s.davg2) > var2Update*s.dvar2:
		updateForeground = true
	}

	if updateForeground {
		s.davg1, s.davg2 = 0, 0
		s.dvar1, s.dvar2 = 0, 0
		// Copy background filter to foreground filter.
		copy(s.foreground, s.w)
		// Apply a smooth transition so as to not introduce blocking artifacts.
		for i := range fs {
			s.e[i+fs] = float32(s.window[i+fs]*s.e[i+fs]) + float32(s.window[i]*s.y[i+fs])
		}
	} else {
		// Otherwise, check if the background filter is significantly worse.
		resetBackground := false
		if -(sff-see)*abs32(sff-see) > varBacktrack*(sff*dbf) {
			resetBackground = true
		}
		if -s.davg1*abs32(s.davg1) > varBacktrack*s.dvar1 {
			resetBackground = true
		}
		if -s.davg2*abs32(s.davg2) > varBacktrack*s.dvar2 {
			resetBackground = true
		}
		if resetBackground {
			// Copy foreground filter to background filter, and the output so
			// as to get correct adaptation.
			copy(s.w, s.foreground)
			for i := range fs {
				s.y[i+fs] = s.e[i+fs]
			}
			for i := range fs {
				s.e[i] = s.input[i] - s.y[i+fs]
			}
			see = sff
			s.davg1, s.davg2 = 0, 0
			s.dvar1, s.dvar2 = 0, 0
		}
	}

	var sey, syy, sdd float32
	// Compute error signal (for the output with de-emphasis).
	for i := range fs {
		tmpOut := s.input[i] - s.e[i+fs]
		tmpOut += float32(s.preemph * s.memE)
		// This is an arbitrary test for saturation in the microphone signal.
		if in[i] <= -32000 || in[i] >= 32000 {
			if s.saturated == 0 {
				s.saturated = 1
			}
		}
		out[i] = min(max(tmpOut, -32768), 32767)
		s.out16[i] = word2int(tmpOut)
		s.memE = tmpOut
	}

	// Compute error signal (filter update version).
	for i := range fs {
		s.e[i+fs] = s.e[i]
		s.e[i] = 0
	}

	// Compute a bunch of correlations.
	sey += innerProd(s.e[fs:], s.y[fs:], fs)
	syy += innerProd(s.y[fs:], s.y[fs:], fs)
	sdd += innerProd(s.input, s.input, fs)

	// Convert error to frequency domain.
	s.fft.Forward(s.e, s.ef)
	clear(s.y[:fs])
	s.fft.Forward(s.y, s.yf)

	// Compute power spectrum of echo (X), error (E) and filter response (Y).
	powerSpectrumAccum(s.ef, s.rf, n)
	powerSpectrumAccum(s.yf, s.yfp, n)

	// Do some sanity check.
	nf := float64(n)
	if !(syy >= 0 && sxx >= 0 && see >= 0) ||
		!(float64(sff) < nf*1e9 && float64(syy) < nf*1e9 && float64(sxx) < nf*1e9) {
		// Things have gone really bad.
		s.screwedUp += 50
		clear(out[:fs])
		clear(s.out16)
	} else if sff > sdd+float32(float32(n)*10000) {
		// AEC seems to add lots of echo instead of removing it, let's see if
		// it will improve.
		s.screwedUp++
	} else {
		// Everything's fine.
		s.screwedUp = 0
	}
	if s.screwedUp >= 50 {
		// "The echo canceller started acting funny and got slapped (reset).
		// It swears it will behave now."
		s.Reset()
		return
	}

	// Add a small noise floor to make sure not to have problems when dividing.
	see = max(see, float32(n)*100)

	sxx += innerProd(s.x[fs:], s.x[fs:], fs)
	powerSpectrumAccum(s.xf[:n], s.xfp, n)

	// Smooth far end energy estimate over time.
	for j := 0; j <= fs; j++ {
		s.power[j] = float32(float32(ss1*s.power[j])+1) + float32(ss*s.xfp[j])
	}

	// Compute filtered spectra and (cross-)correlations.
	pey, pyy := float32(1), float32(1)
	for j := fs; j >= 0; j-- {
		eh := s.rf[j] - s.eh[j]
		yh := s.yfp[j] - s.yh[j]
		pey += float32(eh * yh)
		pyy += float32(yh * yh)
		s.eh[j] = float32((1-s.specAverage)*s.eh[j]) + float32(s.specAverage*s.rf[j])
		s.yh[j] = float32((1-s.specAverage)*s.yh[j]) + float32(s.specAverage*s.yfp[j])
	}

	pyy = float32(math.Sqrt(float64(pyy)))
	pey = pey / pyy

	// Compute correlation update rate.
	tmp32 := s.beta0 * syy
	if tmp32 > s.betaMax*see {
		tmp32 = s.betaMax * see
	}
	alpha := tmp32 / see
	alpha1 := 1 - alpha
	// Update correlations (recursive average).
	s.pey = float32(alpha1*s.pey) + float32(alpha*pey)
	s.pyy = float32(alpha1*s.pyy) + float32(alpha*pyy)
	if s.pyy < 1 {
		s.pyy = 1
	}
	// We don't really hope to get better than 33 dB (MIN_LEAK-3dB)
	// attenuation anyway.
	if s.pey < minLeak*s.pyy {
		s.pey = minLeak * s.pyy
	}
	if s.pey > s.pyy {
		s.pey = s.pyy
	}
	// leak_estimate is the linear regression result.
	s.leakEstimate = s.pey / s.pyy
	if s.leakEstimate > 16383 {
		s.leakEstimate = 32767
	}

	// Compute Residual to Error Ratio.
	rer := float32((float64(.0001*float64(sxx)) + float64(3.*float64(s.leakEstimate*syy))) / float64(see))
	// Check for y in e (lower bound on RER).
	if lb := (sey * sey) / (1 + float32(see*syy)); rer < lb {
		rer = lb
	}
	if rer > .5 {
		rer = .5
	}

	// We consider that the filter has had minimal adaptation if the
	// following is true.
	if !s.adapted && s.sumAdapt > float32(m) && s.leakEstimate*syy > float32(.03)*syy {
		s.adapted = true
	}

	if s.adapted {
		// Normal learning rate calculation once we're past the minimal
		// adaptation phase.
		for i := 0; i <= fs; i++ {
			// Compute frequency-domain adaptation mask.
			r := s.leakEstimate * s.yfp[i]
			e := s.rf[i] + 1
			if float64(r) > .5*float64(e) {
				r = float32(.5 * float64(e))
			}
			r = float32(float64(.7*float64(r)) + float64(.3*float64(rer*e)))
			s.power1[i] = r / (e * (s.power[i] + 10))
		}
	} else {
		// Temporary adaption rate if filter is not yet adapted enough.
		var adaptRate float32
		if sxx > float32(n)*1000 {
			tmp32 = float32(.25) * sxx
			if float64(tmp32) > .25*float64(see) {
				tmp32 = float32(.25 * float64(see))
			}
			adaptRate = tmp32 / see
		}
		for i := 0; i <= fs; i++ {
			s.power1[i] = adaptRate / (s.power[i] + 10)
		}
		// How much have we adapted so far?
		s.sumAdapt += adaptRate
	}

	copy(s.lastY[:fs], s.lastY[fs:])
	if s.adapted {
		// If the filter is adapted, take the filtered echo. Upstream
		// subtracts the int16 output from the int16 input.
		for i := range fs {
			s.lastY[fs+i] = in[i] - s.out16[i]
		}
	}
}

// Residual is speex_echo_get_residual: the power spectrum of the estimated
// residual echo, frameSize+1 bins in 16-bit units squared.
func (s *State) Residual(residual []float32) {
	n := s.windowSize
	// Apply hanning window.
	for i := range n {
		s.y[i] = s.window[i] * s.lastY[i]
	}
	// Compute power spectrum of the echo.
	s.fft.Forward(s.y, s.yf)
	powerSpectrum(s.yf, residual, n)

	var leak2 float32
	if s.leakEstimate > .5 {
		leak2 = 1
	} else {
		leak2 = 2 * s.leakEstimate
	}
	// Estimate residual echo.
	for i := 0; i <= s.frameSize; i++ {
		residual[i] = truncInt32(leak2 * residual[i])
	}
}

// ImpulseResponse is SPEEX_ECHO_GET_IMPULSE_RESPONSE in natural units:
// m*frameSize time-domain taps of the background filter, so that the echo
// estimate is the played signal convolved with them. Upstream returns the
// inverse FFT of the weights, which is the windowSize times larger, times
// 32767 and truncated to integers.
func (s *State) ImpulseResponse() []float32 {
	n, fs := s.windowSize, s.frameSize
	filt := make([]float32, s.m*fs)
	for j := range s.m {
		s.fft.Inverse(s.w[j*n:(j+1)*n], s.wtmp)
		for i, v := range s.wtmp[:fs] {
			filt[j*fs+i] = v / float32(n)
		}
	}
	return filt
}

func abs32(x float32) float32 {
	if x < 0 {
		return -x
	}
	return x
}

// truncInt32 is C's (spx_int32_t) cast of a float: truncation toward zero.
// Out-of-range values are undefined in C; they saturate here.
func truncInt32(x float32) float32 {
	switch {
	case x >= 2147483648:
		return 2147483647
	case x <= -2147483648:
		return -2147483648
	case x != x:
		return 0
	}
	return float32(int32(x))
}

// FromCanceller returns the State inside an *echo.Canceller. Package echo
// sets it at init, so package preprocess can link a canceller without echo
// exporting its internals.
var FromCanceller func(c any) *State
