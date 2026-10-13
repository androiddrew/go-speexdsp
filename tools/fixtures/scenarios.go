package main

import (
	"math"
	"math/rand/v2"

	"github.com/androiddrew/go-speexdsp/internal/room"
)

const rate = 16000

// clips are the source recordings in testdata/clips.
type clips struct {
	heart, michael []float32 // Kokoro "agent" voices
	command, reply []float32 // openWakeWord human speech
}

// scenario is one input recording: what the mic heard (mic), what the
// speaker played (ref) and the near-end speech alone (near, when there is
// any). All are 16 kHz mono.
type scenario struct {
	Name string `json:"name"`
	Desc string `json:"description"`
	mic  []float32
	ref  []float32
	near []float32
}

// place adds clip, scaled by gain, into dst at second t.
func place(dst, clip []float32, t float64, gain float32) {
	off := int(t * rate)
	for i, v := range clip {
		if off+i < len(dst) {
			dst[off+i] += gain * v
		}
	}
}

func seconds(s float64) []float32 { return make([]float32, int(s*rate)) }

func scenarios(c clips) []scenario {
	var out []scenario

	// 1. Echo only: the agent talks and nobody else does.
	{
		ref := c.heart[:8*rate]
		near := seconds(8)
		out = append(out, scenario{
			Name: "echo_only",
			Desc: "agent voice through talkie's room (80 ms, -6 dB, 300 ms RT60, tanh speaker), no near end",
			mic:  room.New(room.Talkie).Mix(near, ref), ref: ref,
		})
	}

	// 1b. The same with a linear speaker, the canceller's best case.
	{
		cfg := room.Talkie
		cfg.Distortion = 0
		ref := c.michael[:6*rate]
		near := seconds(6)
		out = append(out, scenario{
			Name: "echo_linear",
			Desc: "agent voice through talkie's room with a linear speaker, no near end",
			mic:  room.New(cfg).Mix(near, ref), ref: ref,
		})
	}

	// 2. Double-talk: the user starts talking 3 s into the agent's speech.
	{
		ref := c.michael[:8*rate]
		near := seconds(8)
		place(near, c.command, 3, 1)
		place(near, c.reply, 6.2, 1)
		out = append(out, scenario{
			Name: "double_talk",
			Desc: "agent voice with the user talking over it from 3.0 s to 5.7 s and 6.2 s to 7.8 s",
			mic:  room.New(room.Talkie).Mix(near, ref), ref: ref, near: near,
		})
	}

	// 3. Echo path change: the room's impulse response switches halfway, as
	// if the speaker moved.
	{
		ref := c.heart[:10*rate]
		near := seconds(10)
		a := room.New(room.Talkie).Mix(near, ref)
		moved := room.Config{Rate: rate, DelayMS: 50, GainDB: -3, RT60MS: 200, Distortion: 1.5, NoiseDBFS: -65, Seed: 7}
		b := room.New(moved).Mix(near, ref)
		mic := append(append([]float32{}, a[:5*rate]...), b[5*rate:]...)
		out = append(out, scenario{
			Name: "path_change",
			Desc: "agent voice; at 5 s the room switches to 50 ms delay, -3 dB, 200 ms RT60",
			mic:  mic, ref: ref,
		})
	}

	// 4. Noise: the user talks over steady fan-like noise, with no agent.
	{
		near := seconds(6)
		place(near, c.command, 1, 1)
		place(near, c.reply, 4.2, 1)
		mic := make([]float32, len(near))
		fan := fanNoise(len(near), -35)
		for i := range mic {
			mic[i] = float32(max(-1, min(1, float64(near[i])+fan[i])))
		}
		out = append(out, scenario{
			Name: "noise",
			Desc: "user speech over -35 dBFS low-pass fan noise with a 120 Hz hum, no agent",
			mic:  mic, ref: seconds(6), near: near,
		})
	}

	// 5. Level change: a quiet talker, then a loud one.
	{
		near := seconds(8)
		place(near, c.command, 0.5, 0.1) // -20 dB
		place(near, c.reply, 4, 0.1)
		place(near, c.reply, 6, 1)
		mic := make([]float32, len(near))
		r := rand.New(rand.NewPCG(5, 6))
		amp := math.Pow(10, -65/20.0)
		for i := range mic {
			mic[i] = float32(max(-1, min(1, float64(near[i])+r.NormFloat64()*amp)))
		}
		out = append(out, scenario{
			Name: "level",
			Desc: "user speech at -20 dB from 0.5 s to 5.6 s, then at full level from 6.0 s",
			mic:  mic, ref: seconds(8), near: near,
		})
	}

	// 6. Silence and full scale: digital silence, then full-scale square and
	// triangle waves, then full-scale noise on both channels.
	{
		mic, ref := seconds(3), seconds(3)
		for i := rate; i < 2*rate; i++ {
			if (i/40)%2 == 0 { // 200 Hz square at full scale
				mic[i] = 1
			} else {
				mic[i] = -1
			}
			ph := float32(i%80) / 80 // 200 Hz triangle at full scale
			ref[i] = 4*min(ph, 1-ph) - 1
		}
		r := rand.New(rand.NewPCG(8, 9))
		for i := 2 * rate; i < 3*rate; i++ {
			mic[i] = float32(2*r.Float64() - 1)
			ref[i] = float32(2*r.Float64() - 1)
		}
		out = append(out, scenario{
			Name: "edge",
			Desc: "1 s of digital silence, 1 s of full-scale square (mic) and triangle (ref), 1 s of full-scale noise on both",
			mic:  mic, ref: ref,
		})
	}
	return out
}

// fanNoise is low-pass filtered Gaussian noise plus a 120 Hz hum 12 dB
// below it, scaled to dbfs RMS.
func fanNoise(n int, dbfs float64) []float64 {
	r := rand.New(rand.NewPCG(10, 11))
	out := make([]float64, n)
	var lp, sum float64
	for i := range out {
		lp += 0.08 * (r.NormFloat64() - lp)
		out[i] = lp
		sum += lp * lp
	}
	rms := math.Sqrt(sum / float64(n))
	target := math.Pow(10, dbfs/20)
	hum := target * math.Pow(10, -12/20.0) * math.Sqrt2
	for i := range out {
		out[i] = out[i]/rms*target + hum*math.Sin(2*math.Pi*120*float64(i)/rate)
	}
	return out
}

// pipeline is the canceller and preprocessor settings one run uses.
type pipeline struct {
	Frame         int     `json:"frame"`
	Filter        int     `json:"filter"`
	Denoise       bool    `json:"denoise"`
	NoiseSuppress int     `json:"noise_suppress_db"`
	EchoSuppress  int     `json:"echo_suppress_db"`
	EchoActive    int     `json:"echo_suppress_active_db"`
	AGC           bool    `json:"agc"`
	AGCLevel      float32 `json:"agc_level"`
	LinkEcho      bool    `json:"link_echo"`
	AGCIncrement  int     `json:"agc_increment,omitempty"`
	AGCDecrement  int     `json:"agc_decrement,omitempty"`
	AGCMaxGain    int     `json:"agc_max_gain,omitempty"`
	VAD           bool    `json:"vad"`
	ResetFrame    int     `json:"reset_frame,omitempty"` // > 0: reset the canceller before this frame
	ProbStart     int     `json:"prob_start,omitempty"`
	ProbContinue  int     `json:"prob_continue,omitempty"`
}

var (
	// wonky is wonky-talkie's EchoStage: residual echo suppression with mild
	// denoising.
	wonky = pipeline{Frame: 256, Filter: 4000, Denoise: true, NoiseSuppress: -10, EchoSuppress: -40, EchoActive: -15, AGCLevel: 8000, LinkEcho: true}
	// denoise is the preprocessor's defaults without a canceller link.
	denoise = pipeline{Frame: 256, Filter: 4000, Denoise: true, NoiseSuppress: -15, EchoSuppress: -40, EchoActive: -15, AGCLevel: 8000}
	// agc adds automatic gain control to the defaults.
	agc = pipeline{Frame: 256, Filter: 4000, Denoise: true, NoiseSuppress: -15, EchoSuppress: -40, EchoActive: -15, AGC: true, AGCLevel: 8000, VAD: true}
	// everything links the canceller and turns on AGC and the VAD with
	// non-default settings.
	everything = pipeline{Frame: 256, Filter: 4000, Denoise: true, NoiseSuppress: -15, EchoSuppress: -40, EchoActive: -15, AGC: true, AGCLevel: 12000, LinkEcho: true,
		AGCIncrement: 20, AGCDecrement: -30, AGCMaxGain: 20, VAD: true, ProbStart: 50, ProbContinue: 30}
)

// runSpec is one scenario through one pipeline.
type runSpec struct {
	Name     string   `json:"name"`
	Scenario string   `json:"scenario"`
	Pipeline pipeline `json:"pipeline"`
}

func runs() []runSpec {
	f160 := wonky
	f160.Frame = 160
	nodenoise := wonky
	nodenoise.Denoise = false
	reset := wonky
	reset.ResetFrame = 5 * rate / reset.Frame // when the room changes
	return []runSpec{
		{"echo_only", "echo_only", wonky},
		{"echo_only_f160", "echo_only", f160},
		{"echo_linear", "echo_linear", wonky},
		{"double_talk", "double_talk", wonky},
		{"double_talk_nodenoise", "double_talk", nodenoise},
		{"path_change", "path_change", wonky},
		{"path_change_reset", "path_change", reset},
		{"noise", "noise", denoise},
		{"level", "level", agc},
		{"edge", "edge", everything},
	}
}
