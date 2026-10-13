# Plan: go-speexdsp

## Context
talkie-core plays the agent's voice through a speaker, and that voice comes back into the microphone. Today talkie has no echo canceller. Only duck-and-verify keeps the agent from interrupting itself: briefly turn the agent down, then check that the voice in the mic didn't follow it down. With a real Kokoro voice in the simulated room, a real interruption cleared the 6 dB threshold by only 1.4 dB (7.4 dB measured).

The plan is a pure-Go port of SpeexDSP's echo canceller and preprocessor:
- No cgo, so it runs wherever talkie runs: the desktop, the Orin, containers and other operating systems.
- talkie owns the canceller and can test it, unlike a system-level one such as PipeWire's.
- It is a standalone library in the model family, published like go-silero-vad, then wired into talkie-core.

**Decided (2026-10-13):**
- Name: `go-speexdsp`.
- v0.1.0 scope: the echo canceller plus the full preprocessor, meaning residual echo suppression, noise suppression and automatic gain control.

## Upstream
- **Source:** xiph/speexdsp at tag `SpeexDSP-1.2.1` (commit `1b28a0f61bc31162979e1f26f3981fc3637095c8`, 2022-06-13). It is the version Ubuntu 24.04 ships as `libspeexdsp1`, the one wonky-talkie used.
- **Build to match:** the default floating-point build, which uses the `smallft` FFT (FFTPACK from Ogg Vorbis). The fixed-point paths are not ported.
- **Files to port:**

  | File | Lines | Becomes |
  |---|---|---|
  | `libspeexdsp/mdf.c` | 1279 | the echo canceller |
  | `libspeexdsp/preprocess.c` | 1215 | the preprocessor |
  | `libspeexdsp/filterbank.c` + `.h` | 293 | the preprocessor's Bark-scale bands |
  | `libspeexdsp/smallft.c` | 1261 | the FFT |
  | `libspeexdsp/fftwrap.c` | 448 | the FFT's scaling and packing (smallft path only) |

  The float versions of `math_approx.h` become Go `math`.
- **License:** BSD-3-Clause (Xiph.org, Jean-Marc Valin, CSIRO and others). Ported files keep the upstream copyright notice.
  - Recommendation: license the whole module BSD-3-Clause rather than the family's usual Apache-2.0. It is almost entirely ported code, and one license avoids mixing two per file.
  - Upstream's COPYING goes in `LICENSE` with our copyright line added. NOTICE and THIRD_PARTY.md record the lineage.

## Module
- `github.com/androiddrew/go-speexdsp`, Go 1.25.0, no cgo, no dependencies outside the standard library.
- Samples are float32 in [-1, 1], 16-bit-scale values ÷ 32768, as everywhere else in the family. Internally, values are scaled ×32768 so Speex's constants (noise floors, AGC levels, leak estimates, all in 16-bit units) apply unchanged.
- Arithmetic is float32 in the same order as the C code, so the parity tests can be tight. Avoid the Go compiler fusing multiply-adds where it matters; on arm64 (the Orin) the Go compiler may fuse x*y+z, so measure the drift on both architectures.

## Layout
```
echo/            Canceller: mdf.c, single microphone and single speaker
preprocess/      Preprocessor: preprocess.c + filterbank.c
internal/fft/    smallft.c (spx_drft) + fftwrap.c's smallft scaling and packing
internal/room/   simulated speaker -> room -> mic path for scenario tests
                 (from talkie-core/internal/sim: delay, reverb, distortion, noise)
tools/fixtures/  reference recorder: compiles the pinned C sources and runs the scenarios
testdata/        speech clips, scenario WAVs, reference outputs
README.md CHANGELOG.md LICENSE NOTICE THIRD_PARTY.md .github/workflows/ci.yml
```

## Public API (v0.1.0)

### package echo
```go
// Canceller removes the echo of what the speaker played from the microphone.
type Canceller struct{ ... }

// New: frameSize samples per call (256 = 16 ms at 16 kHz is the recommended size),
// filterLength samples of room response it can model (4000 = 250 ms at 16 kHz),
// sampleRate in Hz. It is speex_echo_state_init plus SPEEX_ECHO_SET_SAMPLING_RATE.
func New(frameSize, filterLength, sampleRate int) (*Canceller, error)

// Cancel writes mic with the echo of played removed into out, one frame each.
// played is what the speaker played during mic's frame (a full-duplex device
// keeps them aligned). This is speex_echo_cancellation.
func (c *Canceller) Cancel(mic, played, out []float32)

func (c *Canceller) Reset()                        // speex_echo_state_reset
func (c *Canceller) FrameSize() int
func (c *Canceller) ImpulseResponse() []float32    // SPEEX_ECHO_GET_IMPULSE_RESPONSE, for diagnostics
```
- **Not in v0.1.0:**
  - `speex_echo_capture`/`speex_echo_playback`, the variant with a separate playback buffer for unaligned devices. talkie's duplex device aligns the frames.
  - Multiple microphones or speakers (`speex_echo_state_init_mc`).

### package preprocess
```go
type Config struct {
	FrameSize, SampleRate int
	Denoise         bool    // SET_DENOISE; default true, as upstream
	NoiseSuppressDB int     // SET_NOISE_SUPPRESS; default -15
	AGC             bool    // SET_AGC; default false
	AGCLevel        float32 // SET_AGC_LEVEL, in 16-bit units; default 8000
	AGCIncrement, AGCDecrement, AGCMaxGain int // dB/s, dB/s, dB; upstream defaults
	EchoSuppressDB, EchoSuppressActiveDB int  // defaults -40, -15
	// Echo links the canceller for residual echo suppression (SET_ECHO_STATE).
	Echo *echo.Canceller
}
// The Go zero value cannot say "true", so Denoise needs an explicit default
// (DefaultConfig(frameSize, rate), or a Disable field). Settle this when
// writing the code.

type Preprocessor struct{ ... }
func New(c Config) (*Preprocessor, error)        // speex_preprocess_state_init + ctls
func (p *Preprocessor) Run(frame []float32) bool // in place; reports speech (speex_preprocess_run)
func (p *Preprocessor) Update(frame []float32)   // speex_preprocess_estimate_update
func (p *Preprocessor) SpeechProbability() float32 // GET_PROB
func (p *Preprocessor) AGCGain() float32           // GET_AGC_GAIN
func (p *Preprocessor) Loudness() float32          // GET_AGC_LOUDNESS
func (p *Preprocessor) PSD() []float32; func (p *Preprocessor) NoisePSD() []float32
```
- **Port** all of `speex_preprocess_run`: noise estimation and suppression, residual echo suppression, AGC, and the speech probability (its VAD).
- **Not ported:** dereverb, which is unfinished upstream. Its control has no effect.
- **Document** that Silero is the better VAD. Speex's speech probability still matters because the AGC depends on it.

## Fixtures and tests

### Reference recorder (`tools/fixtures`)
- Clone the pinned tag and compile its C sources directly with gcc:
  - Sources: `mdf.c preprocess.c filterbank.c fftwrap.c smallft.c`
  - Flags: `-DFLOATING_POINT -DUSE_SMALLFT -DEXPORT= -O2 -ffp-contract=off`
- Link them with a small `ref.c` that runs each scenario through the canceller and preprocessor with the scenario's settings.
- Record into `testdata/reference/`:
  - the 16-bit outputs, after the canceller and after the preprocessor;
  - per-frame values: AGC gain, loudness, speech probability, and the canceller's leak estimate.
- Record the toolchain and commit as phonmatchnet's `reference.json` does.

### Scenarios (`tools/fixtures/scenarios.go` writes them; CI never regenerates them)
All are 16 kHz mono with a fixed seed, using the speech clips from talkie-core's sim (openWakeWord, Apache-2.0) and Kokoro clips for the "agent". `mic` is what the mic hears; `ref` is what the speaker played.

1. **Echo only:** the agent talks and nobody else does. Tests convergence and echo removed (ERLE).
2. **Double-talk:** the user starts talking 3 s into the agent's speech. The filter must not diverge, and the user's voice must survive.
3. **Echo path change:** the room's impulse response switches halfway through, as if the speaker moved. Tests reconvergence.
4. **Noise:** the user talks over a steady fan-like noise with no agent. Tests noise suppression.
5. **Level change:** a quiet talker, then a loud one. Tests AGC tracking.
6. **Silence and full scale:** edge cases with no NaNs and correct clipping.

### Tests (pure Go: they always run, in CI too)
- **FFT:** the port matches C `spx_fft`/`spx_ifft` output exactly in packing and scaling, for the sizes used (2 × frameSize, and preprocess's sizes, including non-powers of two such as 320).
- **Parity:**
  - Go output vs the C reference: within 1 LSB per sample over the first second.
  - Over each whole scenario, segmental output energy within 0.1 dB. Adaptive filters can let float rounding drift, so the test reports the worst drift.
  - The same for AGC gain and speech probability, frame by frame.
- **Quality**, with the C reference's results as the baseline:
  - ERLE after convergence;
  - convergence time;
  - near-end preservation during double-talk (correlation with the clean near-end speech);
  - noise reduction in dB;
  - AGC settled level.
- **Validation:** wrong frame sizes and nil/short slices give errors from `New` and documented panics or errors in `Cancel`/`Run`.
- **Benchmarks:** cost per 256-sample frame with a 4000-tap filter, for the canceller and the preprocessor. Target: well under 1 ms on the desktop; measure on the Orin.

## Docs and release
- **README:** what it is (a port of SpeexDSP 1.2.1's echo canceller and preprocessor), a quick start (cancel, then preprocess with the linked canceller), recommended settings for 16 kHz voice, measured quality and cost, and known limits.
- **Known limits:** float only; one mic and one speaker; aligned frames; dereverb not ported.
- **CHANGELOG, LICENSE (BSD-3), NOTICE and THIRD_PARTY.md.**
- **CI:** `go vet`, `go test -race ./...`, staticcheck v0.8.2 and govulncheck, on Go 1.25.x and stable. No native dependencies.
- **Release:** v0.1.0, with commits authored as the user only.

## Milestones
1. `internal/fft`: smallft port with exact parity against the C FFT.
2. `echo`: mdf port with parity on the echo-only and double-talk scenarios.
3. `preprocess`: denoise and residual echo suppression, with parity.
4. `preprocess`: AGC and speech probability, with parity.
5. Quality tests, benchmarks (desktop and Orin), README and release.

## Then: talkie-core integration (not part of this library)
- **Engine echo stage:** port wonky's `EchoStage`.
  - 256-sample frames at 16 kHz and a 250 ms filter.
  - The preprocessor is linked to the canceller for residual suppression, with mild denoising (-10 dB) and echo suppression of -40 / -15 dB (wonky's settings).
  - It runs only while the speaker is playing, plus a 1 s hold for the echo tail.
  - The echo removed (ERLE) is reported on `PlaybackFinished`.
- **AGC stays off in talkie** until duck-and-verify measures energies before the gain stage. Otherwise the AGC would undo the loudness drop that verification looks for.
- **Rerun the echo scenarios with the canceller** (wonky's `test_aec_*` cases, skipped so far) and the real-model barge-in test. The goal is a margin well above the 1.4 dB measured without it.

## Verification
1. `go vet ./... && go test -race ./...` on amd64. Everything is pure Go.
2. Regenerate the references (`tools/fixtures`) from the pinned C source and confirm the committed fixtures are byte-identical.
3. Parity and quality report: the worst per-sample and per-segment drift, and ERLE, Go vs C, for every scenario.
4. `go test -bench .` on the desktop and the Orin.
5. After the tag, `go list -m github.com/androiddrew/go-speexdsp@v0.1.0` resolves.
