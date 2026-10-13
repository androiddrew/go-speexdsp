// Package room simulates the speaker -> room -> microphone path for the
// scenario fixtures. It follows talkie-core's internal/sim Room (from
// wonky-talkie's EchoRoom): the speaker's signal comes back delayed,
// reverberant, slightly distorted and attenuated, plus room noise, and the
// converter clips at full scale.
package room

import (
	"math"
	"math/rand/v2"
)

// Config describes a room. The zero Distortion means a linear speaker.
type Config struct {
	Rate       int     // Hz
	DelayMS    int     // direct path delay
	GainDB     float64 // direct path gain
	RT60MS     int     // reverberation time
	Distortion float64 // tanh drive of the speaker; 0 is linear
	NoiseDBFS  float64 // room noise level
	Seed       uint64
}

// Talkie is talkie-core's room: 80 ms delay, -6 dB, 300 ms RT60, tanh
// speaker distortion and -65 dBFS noise at 16 kHz.
var Talkie = Config{Rate: 16000, DelayMS: 80, GainDB: -6, RT60MS: 300, Distortion: 1.5, NoiseDBFS: -65, Seed: 1}

// Room is a simulated echo path.
type Room struct {
	rir        []float64 // impulse response from the first nonzero tap
	delay      int
	distortion float64
	noise      float64
	rng        *rand.Rand
	played     []float64 // distorted speaker history, oldest first
}

// New returns a room. Its impulse response is a direct tap followed by
// exponentially decaying Gaussian reverberation, from a fixed seed.
func New(c Config) *Room {
	rng := rand.New(rand.NewPCG(c.Seed, c.Seed+1))
	g := math.Pow(10, c.GainDB/20)
	d := c.Rate * c.DelayMS / 1000
	n := c.Rate * (c.DelayMS + c.RT60MS) / 1000
	rir := make([]float64, n-d)
	rir[0] = g
	for i := 1; i < len(rir); i++ {
		rir[i] = rng.NormFloat64() * math.Exp(-float64(i-1)/(float64(c.Rate*c.RT60MS)/6900)) * 0.16 * g
	}
	return &Room{rir: rir, delay: d, distortion: c.Distortion, noise: math.Pow(10, c.NoiseDBFS/20), rng: rng}
}

// Mix returns what the mic hears while the near end says near and the
// speaker plays ref. Consecutive calls continue the same stream.
func (r *Room) Mix(near, ref []float32) []float32 {
	mic := make([]float32, len(near))
	for i, u := range near {
		x := float64(ref[i])
		if r.distortion != 0 { // speaker nonlinearity, which linear AEC can't remove
			x = math.Tanh(x*r.distortion) / r.distortion
		}
		r.played = append(r.played, x)
		var echo float64
		last := len(r.played) - 1 - r.delay
		for k := 0; k < len(r.rir) && last-k >= 0; k++ {
			if s := r.played[last-k]; s != 0 {
				echo += r.rir[k] * s
			}
		}
		mic[i] = float32(max(-1, min(1, float64(u)+echo+r.rng.NormFloat64()*r.noise)))
	}
	if keep := r.delay + len(r.rir); len(r.played) > 2*keep {
		r.played = append(r.played[:0], r.played[len(r.played)-keep:]...)
	}
	return mic
}
