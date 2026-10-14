package preprocess

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/androiddrew/go-speexdsp/echo"
	"github.com/androiddrew/go-speexdsp/internal/fixture"
)

// chain runs a fixture run through the Go canceller and preprocessor, as
// TestParity does, and returns both outputs in 16-bit units.
func chain(t *testing.T, rate int, r fixture.Run, rec *fixture.Recording) (aec, pp []float64) {
	t.Helper()
	p := r.Pipeline
	c, pre := newRun(t, rate, p)
	frame := make([]float32, p.Frame)
	for f := range rec.Frames {
		lo, hi := f*p.Frame, (f+1)*p.Frame
		if p.ResetFrame > 0 && f == p.ResetFrame {
			c.Reset()
		}
		if err := c.Cancel(rec.Mic[lo:hi], rec.Ref[lo:hi], frame); err != nil {
			t.Fatal(err)
		}
		for i, v := range frame {
			aec = append(aec, float64(v)*32768)
			frame[i] = float32(fixture.Round(v*32768)) / 32768
		}
		var err error
		if f < p.UpdateFrames {
			err = pre.Update(frame)
		} else {
			_, err = pre.Run(frame)
		}
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range frame {
			pp = append(pp, float64(v)*32768)
		}
	}
	return aec, pp
}

func f64[T int16 | float32](x []T, scale float64) []float64 {
	out := make([]float64, len(x))
	for i, v := range x {
		out[i] = float64(v) * scale
	}
	return out
}

func energy(x []float64) float64 {
	var e float64
	for _, v := range x {
		e += v * v
	}
	return e
}

// ratioDB is 10 log10 of the energy of a over b.
func ratioDB(a, b []float64) float64 { return 10 * math.Log10(energy(a)/max(energy(b), 1e-9)) }

func corr(a, b []float64) float64 {
	var ma, mb float64
	for i := range a {
		ma += a[i]
		mb += b[i]
	}
	ma /= float64(len(a))
	mb /= float64(len(b))
	var sab, saa, sbb float64
	for i := range a {
		sab += (a[i] - ma) * (b[i] - mb)
		saa += (a[i] - ma) * (a[i] - ma)
		sbb += (b[i] - mb) * (b[i] - mb)
	}
	return sab / math.Sqrt(saa*sbb)
}

func rmsDBFS(x []float64) float64 {
	return 10*math.Log10(energy(x)/float64(len(x))) - 20*math.Log10(32768)
}

// span returns x between two times in seconds.
func span(x []float64, rate int, from, to float64) []float64 {
	return x[int(from*float64(rate)):min(len(x), int(to*float64(rate)))]
}

// convergence is the start of the first 250 ms window whose echo removal
// reaches db, or -1.
func convergence(mic, out []float64, rate int, db float64) float64 {
	w := rate / 4
	for i := 0; i+w <= len(out); i += w {
		if ratioDB(mic[i:i+w], out[i:i+w]) >= db {
			return float64(i) / float64(rate)
		}
	}
	return -1
}

// segmentDrift is the worst difference in dB between the energies of
// 20 ms segments of a and b, over segments of b above -60 dBFS.
func segmentDrift(a, b []float64, rate int) float64 {
	w := rate / 50
	var worst float64
	for i := 0; i+w <= len(b); i += w {
		eb := energy(b[i : i+w])
		if 10*math.Log10(eb/float64(w))-20*math.Log10(32768) < -60 {
			continue
		}
		worst = max(worst, math.Abs(10*math.Log10(max(energy(a[i:i+w]), 1e-9)/eb)))
	}
	return worst
}

// metric is one quality measurement on the Go and the C output.
type metric struct {
	name   string
	unit   string
	goVal  float64
	cVal   float64
	within float64 // allowed |go - C|
}

// TestQuality measures echo removal, convergence, near-end preservation,
// noise reduction and AGC level on the scenarios, for the Go port and for
// the C reference outputs, and requires them to agree. It logs a table of
// the measurements.
func TestQuality(t *testing.T) {
	ref, err := fixture.Load()
	if err != nil {
		t.Fatal(err)
	}
	rate := ref.Rate
	runs := map[string]fixture.Run{}
	for _, r := range ref.Runs {
		runs[r.Name] = r
	}
	type outputs struct{ mic, near, goAEC, goPP, cAEC, cPP []float64 }
	load := func(name string) outputs {
		r, ok := runs[name]
		if !ok {
			t.Fatalf("no fixture run %s", name)
		}
		rec, err := fixture.Open(r)
		if err != nil {
			t.Fatal(err)
		}
		goAEC, goPP := chain(t, rate, r, rec)
		n := len(goAEC)
		return outputs{
			mic:   f64(rec.Mic[:n], 32768),
			near:  f64(rec.Near, 32768),
			goAEC: goAEC, goPP: goPP,
			cAEC: f64(rec.AEC, 1), cPP: f64(rec.PP, 1),
		}
	}
	var ms []metric
	add := func(name, unit string, within float64, f func(aec, pp []float64) float64, o outputs) {
		g, c := f(o.goAEC, o.goPP), f(o.cAEC, o.cPP)
		ms = append(ms, metric{name, unit, g, c, within})
	}
	// The preprocessor's output lags its input by one frame.
	lag := func(x []float64, frame int) []float64 { return x[frame:] }

	for _, name := range []string{"echo_only", "echo_only_f160", "echo_linear", "path_change"} {
		o := load(name)
		frame := runs[name].Pipeline.Frame
		end := float64(len(o.goAEC)) / float64(rate)
		last := 4.0 // after convergence
		if name == "path_change" {
			last = 2 // after reconverging
		}
		from := end - last
		add(fmt.Sprintf("%s: canceller ERLE, last %g s", name, last), "dB", 0.1, func(aec, _ []float64) float64 {
			return ratioDB(span(o.mic, rate, from, end), span(aec, rate, from, end))
		}, o)
		add(fmt.Sprintf("%s: canceller + preprocessor ERLE, last %g s", name, last), "dB", 0.1, func(_, pp []float64) float64 {
			mic := o.mic[:len(o.mic)-frame]
			return ratioDB(span(mic, rate, from, end), span(lag(pp, frame), rate, from, end))
		}, o)
		add(name+": canceller reaches 10 dB", "s", 0, func(aec, _ []float64) float64 {
			return convergence(o.mic, aec, rate, 10)
		}, o)
		add(name+": canceller + preprocessor reach 20 dB", "s", 0, func(_, pp []float64) float64 {
			return convergence(o.mic[:len(o.mic)-frame], lag(pp, frame), rate, 20)
		}, o)
		add(name+": worst 20 ms segment energy drift, canceller", "dB", 0.1, func(aec, _ []float64) float64 {
			return segmentDrift(aec, o.cAEC, rate)
		}, o)
		add(name+": worst 20 ms segment energy drift, preprocessor", "dB", 0.1, func(_, pp []float64) float64 {
			return segmentDrift(pp, o.cPP, rate)
		}, o)
		if name == "path_change" {
			add(name+": canceller back to 10 dB after the 5 s change", "s", 0, func(aec, _ []float64) float64 {
				return convergence(span(o.mic, rate, 5, end), span(aec, rate, 5, end), rate, 10)
			}, o)
		}
	}

	{
		o := load("double_talk")
		frame := runs["double_talk"].Pipeline.Frame
		for _, s := range [][2]float64{{3.0, 5.7}, {6.2, 7.8}} {
			label := fmt.Sprintf("double_talk: correlation with near end %.1f-%.1f s", s[0], s[1])
			near := span(o.near, rate, s[0], s[1])
			ms = append(ms, metric{label + ", mic", "", corr(near, span(o.mic, rate, s[0], s[1])), corr(near, span(o.mic, rate, s[0], s[1])), 0})
			add(label+", canceller", "", 0.001, func(aec, _ []float64) float64 {
				return corr(near, span(aec, rate, s[0], s[1]))
			}, o)
			add(label+", preprocessor", "", 0.001, func(_, pp []float64) float64 {
				return corr(near, span(lag(pp, frame), rate, s[0], s[1]))
			}, o)
		}
	}

	{
		o := load("noise")
		frame := runs["noise"].Pipeline.Frame
		// Noise alone between the two utterances, after the estimate settles.
		add("noise: noise between utterances, output/input", "dB", 0.1, func(_, pp []float64) float64 {
			return -ratioDB(span(o.mic, rate, 3.8, 4.15), span(lag(pp, frame), rate, 3.8, 4.15))
		}, o)
		add("noise: speech 1.0-3.7 s, output/input", "dB", 0.1, func(_, pp []float64) float64 {
			return -ratioDB(span(o.mic, rate, 1.0, 3.7), span(lag(pp, frame), rate, 1.0, 3.7))
		}, o)
	}

	{
		o := load("level")
		frame := runs["level"].Pipeline.Frame
		for _, s := range [][2]float64{{1, 3.5}, {4.2, 5.4}, {6.1, 7.5}} {
			ms = append(ms, metric{fmt.Sprintf("level: input %.1f-%.1f s", s[0], s[1]), "dBFS",
				rmsDBFS(span(o.mic, rate, s[0], s[1])), rmsDBFS(span(o.mic, rate, s[0], s[1])), 0})
			add(fmt.Sprintf("level: AGC output %.1f-%.1f s", s[0], s[1]), "dBFS", 0.1, func(_, pp []float64) float64 {
				return rmsDBFS(span(lag(pp, frame), rate, s[0], s[1]))
			}, o)
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "\n| Measurement | Go | C |\n|---|---|---|\n")
	for _, m := range ms {
		fmt.Fprintf(&b, "| %s | %.3f %s | %.3f %s |\n", m.name, m.goVal, m.unit, m.cVal, m.unit)
		if math.Abs(m.goVal-m.cVal) > m.within || math.IsNaN(m.goVal) != math.IsNaN(m.cVal) {
			t.Errorf("%s: Go %.3f, C %.3f; want within %g", m.name, m.goVal, m.cVal, m.within)
		}
	}
	t.Log(b.String())
}

// TestEdge checks the silence and full-scale scenario: outputs stay finite
// and within [-1, 1], and digital silence stays silent.
func TestEdge(t *testing.T) {
	ref, err := fixture.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range ref.Runs {
		if r.Scenario != "edge" {
			continue
		}
		rec, err := fixture.Open(r)
		if err != nil {
			t.Fatal(err)
		}
		aec, pp := chain(t, ref.Rate, r, rec)
		for name, x := range map[string][]float64{"canceller": aec, "preprocessor": pp} {
			for i, v := range x {
				if math.IsNaN(v) || math.Abs(v) > 32768 {
					t.Fatalf("%s: %s sample %d is %v", r.Name, name, i, v/32768)
				}
			}
			// The preprocessor's analysis window reaches one frame ahead of
			// its output, so stop two frames before the square wave starts.
			quiet := ref.Rate - 2*r.Pipeline.Frame
			if e := energy(x[:quiet]); e != 0 {
				t.Errorf("%s: %s output energy %v over the digital silence", r.Name, name, e)
			}
		}
	}
}

func BenchmarkRun(b *testing.B) {
	for _, bc := range []struct {
		name string
		cfg  Config
	}{
		{"256", Config{FrameSize: 256, SampleRate: 16000}},
		{"256_agc", Config{FrameSize: 256, SampleRate: 16000, AGC: true}},
		{"160", Config{FrameSize: 160, SampleRate: 16000}},
	} {
		b.Run(bc.name, func(b *testing.B) {
			p, err := New(bc.cfg)
			if err != nil {
				b.Fatal(err)
			}
			frame := make([]float32, bc.cfg.FrameSize)
			for i := range frame {
				frame[i] = float32(math.Sin(float64(i)/7)) * 0.1
			}
			for b.Loop() {
				if _, err := p.Run(frame); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkChain is talkie's echo stage per frame: the canceller with a
// 250 ms filter, then the linked preprocessor with wonky-talkie's settings.
func BenchmarkChain(b *testing.B) {
	c, err := echo.New(256, 4000, 16000)
	if err != nil {
		b.Fatal(err)
	}
	p, err := New(Config{FrameSize: 256, SampleRate: 16000, NoiseSuppressDB: -10, Echo: c})
	if err != nil {
		b.Fatal(err)
	}
	mic, played, out := make([]float32, 256), make([]float32, 256), make([]float32, 256)
	for i := range mic {
		played[i] = float32(math.Sin(float64(i)/5)) * 0.3
		mic[i] = float32(math.Sin(float64(i)/7)) * 0.1
	}
	for b.Loop() {
		if err := c.Cancel(mic, played, out); err != nil {
			b.Fatal(err)
		}
		if _, err := p.Run(out); err != nil {
			b.Fatal(err)
		}
	}
}
