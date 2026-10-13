// Ported from SpeexDSP 1.2.1 libspeexdsp/filterbank.c, which carries this
// notice:
//
//	Copyright (C) 2006 Jean-Marc Valin
//
//	Redistribution and use in source and binary forms, with or without
//	modification, are permitted provided that the following conditions
//	are met:
//
//	- Redistributions of source code must retain the above copyright
//	notice, this list of conditions and the following disclaimer.
//
//	- Redistributions in binary form must reproduce the above copyright
//	notice, this list of conditions and the following disclaimer in the
//	documentation and/or other materials provided with the distribution.
//
//	- Neither the name of the Xiph.org Foundation nor the names of its
//	contributors may be used to endorse or promote products derived from
//	this software without specific prior written permission.
//
//	THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
//	``AS IS'' AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
//	LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
//	A PARTICULAR PURPOSE ARE DISCLAIMED.  IN NO EVENT SHALL THE FOUNDATION OR
//	CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL,
//	EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT LIMITED TO,
//	PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE, DATA, OR
//	PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY THEORY OF
//	LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT (INCLUDING
//	NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE OF THIS
//	SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.

package preprocess

import "math"

// filterBank is FilterBank: triangular Bark-scale bands over the linear
// spectrum. The float build's normalization (scaling) is unused by the
// preprocessor and not ported.
type filterBank struct {
	bankLeft, bankRight     []int
	filterLeft, filterRight []float32
	nbBanks, len            int
}

// toBark is filterbank.c's toBARK in the float build: float terms, double
// atan, summed in double.
func toBark(n float32) float32 {
	return float32(float64(float64(float32(13.1))*math.Atan(float64(float32(.00074)*n))) +
		float64(float64(float32(2.24))*math.Atan(float64(n*n*float32(1.85e-8)))) +
		float64(float32(1e-4)*n))
}

// newFilterBank is filterbank_new.
func newFilterBank(banks int, sampling float32, n int) *filterBank {
	df := sampling / (2 * float32(n))
	maxMel := toBark(sampling / 2)
	melInterval := maxMel / float32(banks-1)

	b := &filterBank{
		nbBanks:     banks,
		len:         n,
		bankLeft:    make([]int, n),
		bankRight:   make([]int, n),
		filterLeft:  make([]float32, n),
		filterRight: make([]float32, n),
	}
	for i := range n {
		currFreq := float32(i) * df
		mel := toBark(currFreq)
		if mel > maxMel {
			break
		}
		id1 := int(math.Floor(float64(mel / melInterval)))
		var val float32
		if id1 > banks-2 {
			id1 = banks - 2
			val = 1
		} else {
			val = (mel - float32(float32(id1)*melInterval)) / melInterval
		}
		b.bankLeft[i] = id1
		b.filterLeft[i] = 1 - val
		b.bankRight[i] = id1 + 1
		b.filterRight[i] = val
	}
	return b
}

// computeBank32 is filterbank_compute_bank32: band energies of ps.
func (b *filterBank) computeBank32(ps, mel []float32) {
	clear(mel[:b.nbBanks])
	for i := range b.len {
		mel[b.bankLeft[i]] += float32(b.filterLeft[i] * ps[i])
		mel[b.bankRight[i]] += float32(b.filterRight[i] * ps[i])
	}
}

// computePSD16 is filterbank_compute_psd16: band values interpolated back to
// the linear spectrum.
func (b *filterBank) computePSD16(mel, ps []float32) {
	for i := range b.len {
		tmp := float32(mel[b.bankLeft[i]] * b.filterLeft[i])
		tmp += float32(mel[b.bankRight[i]] * b.filterRight[i])
		ps[i] = tmp
	}
}
