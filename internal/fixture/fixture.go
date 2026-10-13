// Package fixture loads the parity fixtures that tools/fixtures records in
// testdata/, for the echo and preprocess tests.
package fixture

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"

	"github.com/androiddrew/go-speexdsp/internal/wav"
)

// Dir is the repository's testdata directory.
var Dir = func() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "testdata")
}()

// Pipeline is a run's canceller and preprocessor settings.
type Pipeline struct {
	Frame         int     `json:"frame"`
	Filter        int     `json:"filter"`
	Denoise       bool    `json:"denoise"`
	NoiseSuppress int     `json:"noise_suppress_db"`
	EchoSuppress  int     `json:"echo_suppress_db"`
	EchoActive    int     `json:"echo_suppress_active_db"`
	AGC           bool    `json:"agc"`
	AGCLevel      float32 `json:"agc_level"`
	LinkEcho      bool    `json:"link_echo"`
	AGCIncrement  int     `json:"agc_increment"`
	AGCDecrement  int     `json:"agc_decrement"`
	AGCMaxGain    int     `json:"agc_max_gain"`
	VAD           bool    `json:"vad"`
	ResetFrame    int     `json:"reset_frame"` // > 0: reset the canceller before this frame
	ProbStart     int     `json:"prob_start"`
	ProbContinue  int     `json:"prob_continue"`
}

// Run is one scenario through one pipeline.
type Run struct {
	Name     string   `json:"name"`
	Scenario string   `json:"scenario"`
	Pipeline Pipeline `json:"pipeline"`
}

// Reference is testdata/reference.json.
type Reference struct {
	Upstream string   `json:"upstream"`
	Rate     int      `json:"rate"`
	Frames   []string `json:"frame_fields"`
	Runs     []Run    `json:"runs"`
}

// Per-frame fields in frames.f32, in reference.json's frame_fields order.
const (
	Leak = iota
	Adapted
	Prob
	AGCGain
	Loudness
	VAD
	NumFields
)

// Load reads testdata/reference.json.
func Load() (*Reference, error) {
	b, err := os.ReadFile(filepath.Join(Dir, "reference.json"))
	if err != nil {
		return nil, err
	}
	var r Reference
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	want := []string{"leak", "adapted", "prob", "agc_gain", "loudness", "vad"}
	if fmt.Sprint(r.Frames) != fmt.Sprint(want) {
		return nil, fmt.Errorf("reference.json frame fields %v, want %v", r.Frames, want)
	}
	return &r, nil
}

// Recording is a scenario's input and a run's C outputs, in 16-bit units.
type Recording struct {
	Mic, Ref, Near []float32 // in [-1, 1)
	AEC, PP        []int16   // C outputs, one per input sample in whole frames
	Frames         [][NumFields]float32
}

// Open loads a run's scenario and C outputs.
func Open(r Run) (*Recording, error) {
	var rec Recording
	var err error
	sdir := filepath.Join(Dir, "scenarios", r.Scenario)
	if rec.Mic, err = readWAV(filepath.Join(sdir, "mic.wav")); err != nil {
		return nil, err
	}
	if rec.Ref, err = readWAV(filepath.Join(sdir, "ref.wav")); err != nil {
		return nil, err
	}
	if _, err := os.Stat(filepath.Join(sdir, "near.wav")); err == nil {
		if rec.Near, err = readWAV(filepath.Join(sdir, "near.wav")); err != nil {
			return nil, err
		}
	}
	rdir := filepath.Join(Dir, "reference", r.Name)
	if rec.AEC, err = readS16(filepath.Join(rdir, "aec.s16")); err != nil {
		return nil, err
	}
	if rec.PP, err = readS16(filepath.Join(rdir, "pp.s16")); err != nil {
		return nil, err
	}
	b, err := os.ReadFile(filepath.Join(rdir, "frames.f32"))
	if err != nil {
		return nil, err
	}
	rec.Frames = make([][NumFields]float32, len(b)/(4*NumFields))
	for i := range rec.Frames {
		for j := range NumFields {
			rec.Frames[i][j] = math.Float32frombits(binary.LittleEndian.Uint32(b[4*(i*NumFields+j):]))
		}
	}
	n := len(rec.Mic) / r.Pipeline.Frame
	if len(rec.Frames) != n || len(rec.AEC) != n*r.Pipeline.Frame || len(rec.PP) != n*r.Pipeline.Frame {
		return nil, fmt.Errorf("run %s: fixture lengths do not match %d frames", r.Name, n)
	}
	return &rec, nil
}

func readWAV(path string) ([]float32, error) {
	s, _, err := wav.Read(path)
	return s, err
}

func readS16(path string) ([]int16, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	v := make([]int16, len(b)/2)
	for i := range v {
		v[i] = int16(binary.LittleEndian.Uint16(b[2*i:]))
	}
	return v, nil
}

// Round is upstream's WORD2INT: a 16-bit-unit value rounded half up and
// saturated to int16.
func Round(x float32) int16 {
	switch {
	case x < -32767.5:
		return -32768
	case x > 32766.5:
		return 32767
	}
	return int16(math.Floor(.5 + float64(x)))
}
