// Command fixtures records the parity fixtures in testdata/ from the pinned
// SpeexDSP C sources. CI never runs it; the committed fixtures are the
// reference.
//
//	git clone https://github.com/xiph/speexdsp /tmp/speexdsp
//	git -C /tmp/speexdsp checkout SpeexDSP-1.2.1
//	go run ./tools/fixtures -speexdsp /tmp/speexdsp
//
// It compiles tools/fixtures/ref/ref.c with upstream's mdf.c, preprocess.c,
// filterbank.c, fftwrap.c and smallft.c, generates the inputs with fixed
// seeds, runs the C code on them, and writes the inputs and outputs under
// testdata/ along with testdata/reference.json. Running it twice gives
// byte-identical files.
//
// Inputs: testdata/clips (committed source recordings). Outputs:
//
//	testdata/fft/in_N.f32, ref_N.f32       FFT vectors and spx_fft/spx_ifft
//	testdata/scenarios/NAME/{mic,ref,near}.wav  scenario recordings
//	testdata/reference/RUN/aec.s16         speex_echo_cancellation output
//	testdata/reference/RUN/pp.s16          speex_preprocess_run output
//	testdata/reference/RUN/frames.f32      per-frame state (see ref.c)
package main

import (
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/androiddrew/go-speexdsp/internal/wav"
)

const (
	upstreamTag    = "SpeexDSP-1.2.1"
	upstreamCommit = "1b28a0f61bc31162979e1f26f3981fc3637095c8"
)

var cflags = []string{"-DFLOATING_POINT", "-DUSE_SMALLFT", "-DEXPORT=", "-O2", "-ffp-contract=off"}

// fftSizes covers the sizes the canceller and preprocessor use (twice the
// frame size: 2*160, 2*256, 2*480, ...) and every radix path in smallft,
// including the general-radix passes (7, 11, 5 backward).
var fftSizes = []int{1, 2, 3, 4, 5, 6, 7, 8, 11, 12, 15, 16, 30, 60, 64, 100, 128, 160, 240, 256, 320, 384, 480, 512, 640, 882, 960, 1024, 2048}

// fftVectors is how many random vectors each size is checked on.
const fftVectors = 3

// Reference is testdata/reference.json: what produced the fixtures.
type Reference struct {
	Upstream  string     `json:"upstream"`
	Commit    string     `json:"commit"`
	Compiler  string     `json:"compiler"`
	CFlags    []string   `json:"cflags"`
	FFTSizes  []int      `json:"fft_sizes"`
	FFTVecs   int        `json:"fft_vectors"`
	Generator string     `json:"generator"`
	Rate      int        `json:"rate"`
	Frames    []string   `json:"frame_fields"`
	Scenarios []scenario `json:"scenarios"`
	Runs      []runSpec  `json:"runs"`
}

func main() {
	src := flag.String("speexdsp", "", "checkout of github.com/xiph/speexdsp at "+upstreamTag)
	out := flag.String("out", "testdata", "output directory")
	flag.Parse()
	if *src == "" {
		log.Fatal("-speexdsp is required")
	}
	commit := strings.TrimSpace(run("git", "-C", *src, "rev-parse", "HEAD"))
	if commit != upstreamCommit {
		log.Fatalf("%s is at %s, want %s (%s)", *src, commit, upstreamCommit, upstreamTag)
	}

	work, err := os.MkdirTemp("", "speexdsp-fixtures")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(work)
	ref := build(*src, work)

	if err := os.MkdirAll(filepath.Join(*out, "fft"), 0o755); err != nil {
		log.Fatal(err)
	}
	recordFFT(ref, *out)
	scen := recordScenarios(ref, *out, work)

	cc, _, _ := strings.Cut(run("gcc", "--version"), "\n")
	writeJSON(filepath.Join(*out, "reference.json"), Reference{
		Upstream:  upstreamTag,
		Commit:    upstreamCommit,
		Compiler:  cc,
		CFlags:    cflags,
		FFTSizes:  fftSizes,
		FFTVecs:   fftVectors,
		Generator: "go run ./tools/fixtures",
		Rate:      rate,
		Frames:    []string{"leak", "adapted", "prob", "agc_gain", "loudness", "vad"},
		Scenarios: scen,
		Runs:      runs(),
	})
}

// build compiles ref.c against the upstream sources and returns the binary.
func build(src, work string) string {
	// speexdsp_types.h includes it as "speexdsp_config_types.h", so it goes
	// directly on the include path.
	inc := filepath.Join(work, "include")
	if err := os.MkdirAll(inc, 0o755); err != nil {
		log.Fatal(err)
	}
	// The autotools build generates this header; these are its values on
	// every platform the fixtures are recorded on.
	types := "#ifndef __SPEEX_TYPES_H__\n#define __SPEEX_TYPES_H__\n#include <stdint.h>\n" +
		"typedef int16_t spx_int16_t;\ntypedef uint16_t spx_uint16_t;\n" +
		"typedef int32_t spx_int32_t;\ntypedef uint32_t spx_uint32_t;\n#endif\n"
	if err := os.WriteFile(filepath.Join(inc, "speexdsp_config_types.h"), []byte(types), 0o644); err != nil {
		log.Fatal(err)
	}
	lib := filepath.Join(src, "libspeexdsp")
	bin := filepath.Join(work, "ref")
	args := append([]string{}, cflags...)
	args = append(args, "-I"+filepath.Join(work, "include"), "-I"+filepath.Join(src, "include"), "-I"+lib,
		"-o", bin, filepath.Join("tools", "fixtures", "ref", "ref.c"))
	// ref.c includes mdf.c and preprocess.c itself.
	for _, f := range []string{"filterbank.c", "fftwrap.c", "smallft.c"} {
		args = append(args, filepath.Join(lib, f))
	}
	args = append(args, "-lm")
	run("gcc", args...)
	return bin
}

// recordFFT writes fft/in_N.f32 (random vectors in 16-bit scale) and
// fft/ref_N.f32 (spx_fft then spx_ifft of each vector).
func recordFFT(ref, out string) {
	for _, n := range fftSizes {
		r := rand.New(rand.NewPCG(uint64(n), 0x5eed))
		in := make([]float32, n*fftVectors)
		for i := range in {
			in[i] = float32(math.Round((r.Float64()*2 - 1) * 32767))
		}
		inPath := filepath.Join(out, "fft", fmt.Sprintf("in_%d.f32", n))
		writeF32(inPath, in)
		run(ref, "fft", fmt.Sprint(n), inPath, filepath.Join(out, "fft", fmt.Sprintf("ref_%d.f32", n)))
	}
}

// recordScenarios writes the scenario recordings and runs each run's
// pipeline over them in C.
func recordScenarios(ref, out, work string) []scenario {
	load := func(name string) []float32 {
		s, r, err := wav.Read(filepath.Join(out, "clips", name))
		if err != nil {
			log.Fatal(err)
		}
		if r != rate {
			log.Fatalf("%s: %d Hz, want %d", name, r, rate)
		}
		return s
	}
	c := clips{
		heart:   load("af_heart-weather.wav"),
		michael: load("am_michael-timer.wav"),
		command: load("user-command.wav"),
		reply:   load("user-reply.wav"),
	}
	scen := scenarios(c)
	byName := map[string]scenario{}
	for _, s := range scen {
		dir := filepath.Join(out, "scenarios", s.Name)
		mkdir(dir)
		writeWAV(filepath.Join(dir, "mic.wav"), s.mic)
		writeWAV(filepath.Join(dir, "ref.wav"), s.ref)
		if s.near != nil {
			writeWAV(filepath.Join(dir, "near.wav"), s.near)
		}
		writeS16(filepath.Join(work, s.Name+".mic.s16"), s.mic)
		writeS16(filepath.Join(work, s.Name+".ref.s16"), s.ref)
		byName[s.Name] = s
	}
	for _, r := range runs() {
		if _, ok := byName[r.Scenario]; !ok {
			log.Fatalf("run %s: no scenario %s", r.Name, r.Scenario)
		}
		dir := filepath.Join(out, "reference", r.Name)
		mkdir(dir)
		p := r.Pipeline
		b := func(v bool) string {
			if v {
				return "1"
			}
			return "0"
		}
		run(ref, "run", fmt.Sprint(p.Frame), fmt.Sprint(p.Filter), fmt.Sprint(rate),
			filepath.Join(work, r.Scenario+".mic.s16"), filepath.Join(work, r.Scenario+".ref.s16"), dir,
			b(p.Denoise), fmt.Sprint(p.NoiseSuppress), fmt.Sprint(p.EchoSuppress), fmt.Sprint(p.EchoActive),
			b(p.AGC), fmt.Sprint(p.AGCLevel), b(p.LinkEcho),
			fmt.Sprint(p.AGCIncrement), fmt.Sprint(p.AGCDecrement), fmt.Sprint(p.AGCMaxGain),
			b(p.VAD), fmt.Sprint(p.ProbStart), fmt.Sprint(p.ProbContinue), fmt.Sprint(p.ResetFrame))
	}
	return scen
}

func mkdir(dir string) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Fatal(err)
	}
}

func writeWAV(path string, s []float32) {
	if err := wav.Write(path, s, rate); err != nil {
		log.Fatal(err)
	}
}

func writeS16(path string, s []float32) {
	q := wav.Quantize(s)
	b := make([]byte, 2*len(q))
	for i, v := range q {
		binary.LittleEndian.PutUint16(b[2*i:], uint16(v))
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		log.Fatal(err)
	}
}

func writeF32(path string, v []float32) {
	b := make([]byte, 4*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint32(b[4*i:], math.Float32bits(x))
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		log.Fatal(err)
	}
}

func writeJSON(path string, v any) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
		log.Fatal(err)
	}
}

func run(name string, args ...string) string {
	cmd := exec.Command(name, args...)
	cmd.Stderr = os.Stderr
	b, err := cmd.Output()
	if err != nil {
		log.Fatalf("%s %s: %v", name, strings.Join(args, " "), err)
	}
	return string(b)
}
