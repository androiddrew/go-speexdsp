// Ported from SpeexDSP 1.2.1 libspeexdsp/preprocess.c, which carries this
// notice:
//
//	Copyright (C) 2003 Epic Games (written by Jean-Marc Valin)
//	Copyright (C) 2004-2006 Epic Games
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

package preprocess

import (
	"math"

	"github.com/androiddrew/go-speexdsp/internal/fft"
	"github.com/androiddrew/go-speexdsp/internal/mdf"
)

// Upstream defaults (preprocess.c, float build).
const (
	nbBands               = 24
	noiseSuppressDefault  = -15
	echoSuppressDefault   = -40
	echoSuppressActiveDef = -15
	speechProbStartDef    = float32(.35)
	speechProbContinueDef = float32(.20)

	loudnessExp = float32(5)
	ampScale    = float32(.001)
)

// state is SpeexPreprocessState. Like mdf, every expression keeps C's
// float/double promotion, and products that feed sums are wrapped in
// float32() or float64() so the compiler cannot fuse them.
type state struct {
	frameSize, psSize, samplingRate, nbands int
	bank                                    *filterBank

	denoise            bool
	noiseSuppress      int
	echoSuppress       int
	echoSuppressActive int
	echo               *mdf.State

	speechProb float32

	frame, ft, ps, gain2, gainFloor, window, noise, reverbEstimate, oldPS []float32
	gain, prior, post, s, smin, stmp                                      []float32
	updateProb                                                            []bool
	zeta, echoNoise, residualEcho, inbuf, outbuf                          []float32

	// AGC, float build only upstream.
	agc                              bool
	agcLevel                         float32
	loudnessAccum                    float32
	loudnessWeight                   []float32 // perceptual loudness curve
	loudness                         float32   // loudness estimate
	agcGain                          float32   // current AGC gain
	maxGain                          float32   // maximum gain allowed
	maxIncreaseStep, maxDecreaseStep float32   // per-frame gain change limits
	prevLoudness                     float32   // loudness of previous frame
	initMax                          float32   // current gain limit during initialisation

	vad                                 bool
	speechProbStart, speechProbContinue float32
	wasSpeech                           bool
	reverbDecay                         float32 // dereverb is not ported; always 0

	nbAdapt, minCount int
	fft               *fft.FFT
}

// conjWindow is conj_window: the analysis/synthesis window.
func conjWindow(w []float32) {
	n := len(w)
	for i := range n {
		x := (4 * float32(i)) / float32(n)
		inv := false
		switch {
		case x < 1:
		case x < 2:
			x = 2 - x
			inv = true
		case x < 3:
			x = x - 2
			inv = true
		default:
			x = 2 - x + 2 // 4 - x
		}
		x = float32(1.271903) * x
		// spx_cos_norm(x) is cos((.5f*M_PI)*x) in double, and SQR16_Q15
		// squares the double expression.
		d := .5 - float64(.5*math.Cos((.5*math.Pi)*float64(x)))
		tmp := float32(d * d)
		if inv {
			tmp = 1 - tmp
		}
		w[i] = float32(math.Sqrt(float64(tmp)))
	}
}

// newState is speex_preprocess_state_init.
func newState(frameSize, samplingRate int) *state {
	n := frameSize // ps_size; upstream's rounding to a power of two is disabled
	n3 := 2*n - frameSize
	m := nbBands
	st := &state{
		frameSize:          frameSize,
		psSize:             n,
		samplingRate:       samplingRate,
		nbands:             m,
		denoise:            true,
		noiseSuppress:      noiseSuppressDefault,
		echoSuppress:       echoSuppressDefault,
		echoSuppressActive: echoSuppressActiveDef,
		bank:               newFilterBank(m, float32(samplingRate), n),
		frame:              make([]float32, 2*n),
		window:             make([]float32, 2*n),
		ft:                 make([]float32, 2*n),
		ps:                 make([]float32, n+m),
		noise:              make([]float32, n+m),
		echoNoise:          make([]float32, n+m),
		residualEcho:       make([]float32, n+m+1),
		reverbEstimate:     make([]float32, n+m),
		oldPS:              make([]float32, n+m),
		prior:              make([]float32, n+m),
		post:               make([]float32, n+m),
		gain:               make([]float32, n+m),
		gain2:              make([]float32, n+m),
		gainFloor:          make([]float32, n+m),
		zeta:               make([]float32, n+m),
		s:                  make([]float32, n),
		smin:               make([]float32, n),
		stmp:               make([]float32, n),
		updateProb:         make([]bool, n),
		inbuf:              make([]float32, n3),
		outbuf:             make([]float32, n3),
		fft:                fft.New(2 * n),
	}
	conjWindow(st.window[:2*n3])
	for i := 2 * n3; i < 2*n; i++ {
		st.window[i] = 1
	}
	// With ps_size equal to the frame size, N4 is 0 and the window needs no
	// rearranging.
	for i := range n + m {
		st.noise[i] = 1
		st.oldPS[i] = 1
		st.gain[i] = 1
		st.post[i] = 1
		st.prior[i] = 1
	}
	for i := range n {
		st.updateProb[i] = true
	}
	st.speechProbStart = speechProbStartDef
	st.speechProbContinue = speechProbContinueDef

	st.agcLevel = 8000
	st.loudnessWeight = make([]float32, n)
	for i := range n {
		ff := float32(float64(float32(i)) * .5 * float64(samplingRate) / float64(float32(n)))
		w := float32(float64(float32(.35)-(float32(.35)*ff)/16000) +
			float64(float64(float32(.73))*math.Exp(float64((float32(-.5)*(ff-3800))*(ff-3800)/float32(9e5)))))
		if w < float32(.01) {
			w = float32(.01)
		}
		st.loudnessWeight[i] = w * w
	}
	st.loudness = float32(1e-15)
	st.agcGain = 1
	st.maxGain = 30
	st.maxIncreaseStep = float32(math.Exp(float64(float32(0.11513)) * 12. * float64(frameSize) / float64(samplingRate)))
	st.maxDecreaseStep = float32(math.Exp(float64(-float32(0.11513)) * 40. * float64(frameSize) / float64(samplingRate)))
	st.prevLoudness = 1
	st.initMax = 1
	return st
}

// setAGCIncrement is SPEEX_PREPROCESS_SET_AGC_INCREMENT (dB/s).
func (st *state) setAGCIncrement(db int) {
	st.maxIncreaseStep = float32(math.Exp(float64(float32(0.11513) * float32(db) * float32(st.frameSize) / float32(st.samplingRate))))
}

// setAGCDecrement is SPEEX_PREPROCESS_SET_AGC_DECREMENT (dB/s).
func (st *state) setAGCDecrement(db int) {
	st.maxDecreaseStep = float32(math.Exp(float64(float32(0.11513) * float32(db) * float32(st.frameSize) / float32(st.samplingRate))))
}

// setAGCMaxGain is SPEEX_PREPROCESS_SET_AGC_MAX_GAIN (dB).
func (st *state) setAGCMaxGain(db int) {
	st.maxGain = float32(math.Exp(float64(float32(0.11513) * float32(db))))
}

// computeAGC is speex_compute_agc: track the loudness of speech frames and
// scale the spectrum ft toward agcLevel.
func (st *state) computeAGC(pframe float32, ft []float32) {
	n := st.psSize
	loudness := float32(1)
	for i := 2; i < n; i++ {
		loudness += float32(2 * float32(n) * st.ps[i] * st.loudnessWeight[i])
	}
	loudness = float32(math.Sqrt(float64(loudness)))
	if pframe > float32(.3) {
		rate := float32(.03 * float64(pframe) * float64(pframe))
		st.loudness = float32(float64((1-rate)*st.loudness) +
			float64(float64(rate)*math.Pow(float64(ampScale*loudness), float64(loudnessExp))))
		st.loudnessAccum = float32((1-rate)*st.loudnessAccum) + rate
		if st.initMax < st.maxGain && st.nbAdapt > 20 {
			st.initMax *= 1 + float32(float32(.1)*pframe*pframe)
		}
	}

	targetGain := float32(float64(ampScale*st.agcLevel) *
		math.Pow(float64(st.loudness)/(1e-4+float64(st.loudnessAccum)), float64(-1/loudnessExp)))

	if (float64(pframe) > .5 && st.nbAdapt > 20) || targetGain < st.agcGain {
		if targetGain > st.maxIncreaseStep*st.agcGain {
			targetGain = st.maxIncreaseStep * st.agcGain
		}
		if targetGain < st.maxDecreaseStep*st.agcGain && loudness < 10*st.prevLoudness {
			targetGain = st.maxDecreaseStep * st.agcGain
		}
		if targetGain > st.maxGain {
			targetGain = st.maxGain
		}
		if targetGain > st.initMax {
			targetGain = st.initMax
		}
		st.agcGain = targetGain
	}

	for i := range 2 * n {
		ft[i] *= st.agcGain
	}
	st.prevLoudness = loudness
}

// hypergeomGain is hypergeom_gain: approximately
// gamma(1.25)^2 * M(-.25;1;-x) / sqrt(x), which multiplied by xi/(1+xi) is
// the optimal gain in the loudness domain.
func hypergeomGain(xx float32) float32 {
	table := [21]float32{
		0.82157, 1.02017, 1.20461, 1.37534, 1.53363, 1.68092, 1.81865,
		1.94811, 2.07038, 2.18638, 2.29688, 2.40255, 2.50391, 2.60144,
		2.69551, 2.78647, 2.87458, 2.96015, 3.04333, 3.12431, 3.20326}
	x := xx
	integer := float32(math.Floor(float64(2 * x)))
	ind := int(integer)
	if ind < 0 {
		return 1
	}
	if ind > 19 {
		return float32(1 + .1296/float64(x))
	}
	frac := float32(2*x) - integer
	return float32(float64(float32((1-frac)*table[ind])+float32(frac*table[ind+1])) / math.Sqrt(float64(x+float32(.0001))))
}

// qcurve is qcurve (float build).
func qcurve(x float32) float32 {
	return 1 / (1 + float32(.15)/x)
}

// computeGainFloor is compute_gain_floor (float build). Its int
// effectiveEchoSuppress is upstream's: the caller's float truncates.
func computeGainFloor(noiseSuppress, effectiveEchoSuppress int, noise, echo, gainFloor []float32, n int) {
	noiseFloor := float32(math.Exp(float64(float32(.2302585) * float32(noiseSuppress))))
	echoFloor := float32(math.Exp(float64(float32(.2302585) * float32(effectiveEchoSuppress))))
	// Compute the gain floor based on different floors for the background
	// noise and residual echo.
	for i := range n {
		num := float32(noiseFloor*noise[i]) + float32(echoFloor*echo[i])
		gainFloor[i] = float32(math.Sqrt(float64(num)) / math.Sqrt(float64(1+noise[i]+echo[i])))
	}
}

// analysis is preprocess_analysis: window the frame with the previous one
// and take its power spectrum, linear and Bark.
func (st *state) analysis(x []float32) {
	n := st.psSize
	n3 := 2*n - st.frameSize
	n4 := st.frameSize - n3
	ps := st.ps

	// 'Build' input frame.
	copy(st.frame[:n3], st.inbuf)
	copy(st.frame[n3:n3+st.frameSize], x)
	// Update inbuf.
	copy(st.inbuf, x[n4:n4+n3])
	// Windowing.
	for i := range 2 * n {
		st.frame[i] = st.frame[i] * st.window[i]
	}
	// Perform FFT.
	st.fft.Forward(st.frame, st.ft)
	// Power spectrum.
	ps[0] = st.ft[0] * st.ft[0]
	for i := 1; i < n; i++ {
		ps[i] = float32(st.ft[2*i-1]*st.ft[2*i-1]) + float32(st.ft[2*i]*st.ft[2*i])
	}
	st.bank.computeBank32(ps, ps[n:])
}

// updateNoiseProb is update_noise_prob: minimum statistics over the smoothed
// spectrum decide where the noise estimate may update.
func (st *state) updateNoiseProb() {
	n := st.psSize
	for i := 1; i < n-1; i++ {
		st.s[i] = float32(float32(float32(float32(.8)*st.s[i])+float32(float32(.05)*st.ps[i-1]))+
			float32(float32(.1)*st.ps[i])) + float32(float32(.05)*st.ps[i+1])
	}
	st.s[0] = float32(float32(.8)*st.s[0]) + float32(float32(.2)*st.ps[0])
	st.s[n-1] = float32(float32(.8)*st.s[n-1]) + float32(float32(.2)*st.ps[n-1])

	if st.nbAdapt == 1 {
		clear(st.smin)
		clear(st.stmp)
	}

	var minRange int
	switch {
	case st.nbAdapt < 100:
		minRange = 15
	case st.nbAdapt < 1000:
		minRange = 50
	case st.nbAdapt < 10000:
		minRange = 150
	default:
		minRange = 300
	}
	if st.minCount > minRange {
		st.minCount = 0
		for i := range n {
			st.smin[i] = min32(st.stmp[i], st.s[i])
			st.stmp[i] = st.s[i]
		}
	} else {
		for i := range n {
			st.smin[i] = min32(st.smin[i], st.s[i])
			st.stmp[i] = min32(st.stmp[i], st.s[i])
		}
	}
	for i := range n {
		st.updateProb[i] = float32(.4)*st.s[i] > st.smin[i]
	}
}

// run is speex_preprocess_run. x is one frame in 16-bit units, processed in
// place, and saturated to the int16 range but not rounded. It returns
// upstream's speech decision, which is always true with the VAD off.
func (st *state) run(x []float32) bool {
	n := st.psSize
	n3 := 2*n - st.frameSize
	ps := st.ps

	st.nbAdapt++
	if st.nbAdapt > 20000 {
		st.nbAdapt = 20000
	}
	st.minCount++

	beta := 1 / float32(st.nbAdapt)
	if .03 > float64(beta) {
		beta = float32(.03)
	}
	beta1 := 1 - beta
	m := st.nbands
	// Deal with residual echo if provided.
	if st.echo != nil {
		st.echo.Residual(st.residualEcho)
		// If there are NaNs or ridiculous values, it'll show up in the DC
		// and we just reset everything to zero.
		if !(st.residualEcho[0] >= 0 && st.residualEcho[0] < float32(n)*1e9) {
			clear(st.residualEcho[:n])
		}
		for i := range n {
			st.echoNoise[i] = max32(float32(.6)*st.echoNoise[i], st.residualEcho[i])
		}
		st.bank.computeBank32(st.echoNoise, st.echoNoise[n:])
	} else {
		clear(st.echoNoise)
	}
	st.analysis(x)
	st.updateNoiseProb()

	// Update the noise estimate for the frequencies where it can be.
	for i := range n {
		if !st.updateProb[i] || ps[i] < st.noise[i] {
			st.noise[i] = max32(0, float32(beta1*st.noise[i])+float32(beta*ps[i]))
		}
	}
	st.bank.computeBank32(st.noise, st.noise[n:])

	// Special case for first frame.
	if st.nbAdapt == 1 {
		copy(st.oldPS, ps[:n+m])
	}

	// Compute a posteriori SNR.
	for i := range n + m {
		// Total noise estimate including residual echo and reverberation.
		totNoise := 1 + st.noise[i] + st.echoNoise[i] + st.reverbEstimate[i]
		// A posteriori SNR = ps/noise - 1.
		st.post[i] = ps[i]/totNoise - 1
		st.post[i] = min32(st.post[i], 100)
		// Computing update gamma = .1 + .9*(old/(old+noise))^2.
		r := st.oldPS[i] / (st.oldPS[i] + totNoise)
		gamma := float32(.1) + float32(float32(.89)*(r*r))
		// A priori SNR update = gamma*max(0,post) + (1-gamma)*old/noise.
		st.prior[i] = float32(gamma*max32(0, st.post[i])) + float32((1-gamma)*(st.oldPS[i]/totNoise))
		st.prior[i] = min32(st.prior[i], 100)
	}

	// Recursive average of the a priori SNR. A bit smoothed for the psd
	// components.
	st.zeta[0] = float32(float32(.7)*st.zeta[0]) + float32(float32(.3)*st.prior[0])
	for i := 1; i < n-1; i++ {
		st.zeta[i] = float32(float32(float32(float32(.7)*st.zeta[i])+float32(float32(.15)*st.prior[i]))+
			float32(float32(.075)*st.prior[i-1])) + float32(float32(.075)*st.prior[i+1])
	}
	for i := n - 1; i < n+m; i++ {
		st.zeta[i] = float32(float32(.7)*st.zeta[i]) + float32(float32(.3)*st.prior[i])
	}

	// Speech probability of presence for the entire frame is based on the
	// average filterbank a priori SNR.
	var zframe float32
	for i := n; i < n+m; i++ {
		zframe += st.zeta[i]
	}
	pframe := float32(.1) + float32(float32(.899)*qcurve(zframe/float32(st.nbands)))

	effectiveEchoSuppress := float32((1-pframe)*float32(st.echoSuppress)) + float32(pframe*float32(st.echoSuppressActive))

	computeGainFloor(st.noiseSuppress, int(effectiveEchoSuppress), st.noise[n:], st.echoNoise[n:], st.gainFloor[n:], m)

	// Compute Ephraim & Malah gain speech probability of presence for each
	// critical band (Bark scale). Technically this is actually wrong because
	// the EM gain assumes a slightly different probability distribution.
	for i := n; i < n+m; i++ {
		priorRatio := st.prior[i] / (st.prior[i] + 1)
		theta := priorRatio * (1 + st.post[i])
		mm := hypergeomGain(theta)
		// Gain with bound.
		st.gain[i] = min32(1, priorRatio*mm)
		// Save old Bark power spectrum.
		st.oldPS[i] = float32(float32(.2)*st.oldPS[i]) + float32((float32(.8)*(st.gain[i]*st.gain[i]))*ps[i])

		p1 := float32(.199) + float32(float32(.8)*qcurve(st.zeta[i]))
		q := 1 - float32(pframe*p1)
		st.gain2[i] = float32(1 / (1 + float64(float64((q/(1-q))*(1+st.prior[i]))*math.Exp(float64(-theta)))))
	}
	// Convert the EM gains and speech prob to linear frequency.
	st.bank.computePSD16(st.gain2[n:], st.gain2)
	st.bank.computePSD16(st.gain[n:], st.gain)

	// Linear frequency resolution (upstream's "if (1)" branch).
	st.bank.computePSD16(st.gainFloor[n:], st.gainFloor)

	// Compute gain according to the Ephraim-Malah algorithm.
	for i := range n {
		// Wiener filter gain.
		priorRatio := st.prior[i] / (st.prior[i] + 1)
		theta := priorRatio * (1 + st.post[i])
		// Optimal estimator for loudness domain.
		mm := hypergeomGain(theta)
		// EM gain with bound.
		g := min32(1, priorRatio*mm)
		// Interpolated speech probability of presence.
		p := st.gain2[i]

		// Constrain the gain to be close to the Bark scale gain.
		if float32(.333)*g > st.gain[i] {
			g = 3 * st.gain[i]
		}
		st.gain[i] = g

		// Save old power spectrum.
		st.oldPS[i] = float32(float32(.2)*st.oldPS[i]) + float32((float32(.8)*(st.gain[i]*st.gain[i]))*ps[i])

		// Apply gain floor.
		if st.gain[i] < st.gainFloor[i] {
			st.gain[i] = st.gainFloor[i]
		}

		// Take into account speech probability of presence (loudness domain
		// MMSE estimator): gain2 = [p*sqrt(gain)+(1-p)*sqrt(gain_floor)]^2.
		tmp := float32(float64(float64(p)*math.Sqrt(float64(st.gain[i]))) +
			float64(float64(1-p)*math.Sqrt(float64(st.gainFloor[i]))))
		st.gain2[i] = tmp * tmp
	}

	// If noise suppression is off, don't apply the gain.
	if !st.denoise {
		for i := range n + m {
			st.gain2[i] = 1
		}
	}

	// Apply computed gain.
	for i := 1; i < n; i++ {
		st.ft[2*i-1] = st.gain2[i] * st.ft[2*i-1]
		st.ft[2*i] = st.gain2[i] * st.ft[2*i]
	}
	st.ft[0] = st.gain2[0] * st.ft[0]
	st.ft[2*n-1] = st.gain2[n-1] * st.ft[2*n-1]

	if st.agc {
		st.computeAGC(pframe, st.ft)
	}

	// Inverse FFT with 1/N scaling.
	st.fft.Inverse(st.ft, st.frame)

	if st.agc {
		var maxSample float32
		for _, v := range st.frame {
			if abs32(v) > maxSample {
				maxSample = abs32(v)
			}
		}
		if maxSample > 28000 {
			damp := 28000 / maxSample
			for i := range st.frame {
				st.frame[i] *= damp
			}
		}
	}

	// Synthesis window (for WOLA).
	for i := range 2 * n {
		st.frame[i] = st.frame[i] * st.window[i]
	}

	// Perform overlap and add.
	for i := range n3 {
		x[i] = min(max(st.outbuf[i]+st.frame[i], -32768), 32767)
	}

	// Update outbuf.
	copy(st.outbuf, st.frame[st.frameSize:st.frameSize+n3])

	// "FIXME: This VAD is a kludge"
	st.speechProb = pframe
	if !st.vad {
		return true
	}
	if st.speechProb > st.speechProbStart || (st.wasSpeech && st.speechProb > st.speechProbContinue) {
		st.wasSpeech = true
		return true
	}
	st.wasSpeech = false
	return false
}

// estimateUpdate is speex_preprocess_estimate_update: update the noise
// estimate and the overlap without producing output.
func (st *state) estimateUpdate(x []float32) {
	n := st.psSize
	n3 := 2*n - st.frameSize
	m := st.nbands
	ps := st.ps

	st.minCount++
	st.analysis(x)
	st.updateNoiseProb()

	for i := 1; i < n-1; i++ {
		if !st.updateProb[i] || ps[i] < st.noise[i] {
			st.noise[i] = float32(float32(.95)*st.noise[i]) + float32(float32(.05)*ps[i])
		}
	}
	for i := range n3 {
		st.outbuf[i] = x[st.frameSize-n3+i] * st.window[st.frameSize+i]
	}
	// Save old power spectrum.
	copy(st.oldPS, ps[:n+m])
	for i := range n {
		st.reverbEstimate[i] = st.reverbDecay * st.reverbEstimate[i]
	}
}

func abs32(x float32) float32 {
	if x < 0 {
		return -x
	}
	return x
}

func min32(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

func max32(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}
