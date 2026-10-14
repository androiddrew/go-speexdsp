# go-speexdsp

`github.com/androiddrew/go-speexdsp` is a pure-Go port of the **acoustic echo
canceller** and the **preprocessor** (noise suppression, residual echo
suppression, automatic gain control) of
[SpeexDSP](https://github.com/xiph/speexdsp) 1.2.1, the version Ubuntu 24.04
ships as `libspeexdsp1`. There is no cgo, so it builds anywhere Go does.

The port follows upstream's default floating-point build expression by
expression. On every recorded scenario its output matches the C library
compiled from the pinned source, sample for sample, on amd64 and arm64 (see
[Parity](#parity)).

```go
import (
    "github.com/androiddrew/go-speexdsp/echo"
    "github.com/androiddrew/go-speexdsp/preprocess"
)

// 16 ms frames at 16 kHz; the canceller models 250 ms of echo path.
canceller, err := echo.New(256, 4000, 16000)
if err != nil { return err }
pre, err := preprocess.New(preprocess.Config{
    FrameSize:            256,
    SampleRate:           16000,
    NoiseSuppressDB:      -10, // mild denoising
    EchoSuppressDB:       -40, // residual echo floor while the near end is silent
    EchoSuppressActiveDB: -15, // ... and while it talks
    Echo:                 canceller,
})
if err != nil { return err }

out := make([]float32, 256)
for { // one frame of each, float32 in [-1, 1], from a full-duplex device
    mic, played := nextFrames()
    if err := canceller.Cancel(mic, played, out); err != nil { return err }
    if _, err := pre.Run(out); err != nil { return err }
    // out is the cleaned microphone signal, one frame late (see below).
}
```

## API and guarantees

### package echo

- `echo.New(frameSize, filterLength, sampleRate)` is `speex_echo_state_init`
  plus `SPEEX_ECHO_SET_SAMPLING_RATE`. The filter is rounded up to whole
  frames (`FilterLength`). Its cost grows with `filterLength/frameSize`.
- `Cancel(mic, played, out)` is `speex_echo_cancellation`. `played` is what
  the speaker played during `mic`'s frame. `out` may be `mic`. Frames of the
  wrong length, or samples that are nonfinite or outside [-1, 1], return
  `ErrInvalidAudio` and leave the canceller unchanged.
- `Reset` is `speex_echo_state_reset`. As upstream does, it keeps the leak
  estimate and the per-block adaptation proportions until the filter adapts
  again, so a canceller reset after adapting is not identical to a new one.
- `Leak` is upstream's leak estimate (0 to 1). `ImpulseResponse` returns the
  learned echo path in natural units: a tap of 0.5 at index d means half the
  played signal returns d samples later.
- `Snapshot` and `Restore` save and restore the canceller's whole state, so a
  later session can start with the echo path already learned instead of
  learning it again. A restored canceller continues bit-exactly; a snapshot
  only restores into a canceller with the same frame size, filter length and
  sample rate (`ErrInvalidSnapshot` otherwise). It is about 126 KB for a
  4096-sample filter at 256-sample frames. Upstream has no equivalent.
- If upstream's sanity checks decide the filter has diverged, it outputs a
  silent frame and resets itself, as upstream does.

### package preprocess

- `preprocess.New(Config)` is `speex_preprocess_state_init` plus its ctls.
  Zero values select upstream's defaults: noise -15 dB, echo -40 dB, echo
  while the near end talks -15 dB, AGC level 8000, AGC steps +12 and -40 dB/s,
  speech start and continue thresholds 35% and 20%. `DisableDenoise` replaces
  `SET_DENOISE`, so the zero value keeps denoising on. As upstream, the sign of
  the suppression settings is ignored. A setting of zero always selects its
  default, so 0 dB of noise suppression cannot be expressed.
- `Run(frame)` is `speex_preprocess_run`, in place. It returns upstream's
  speech decision, which is always true unless `Config.VAD` is set. The output
  lags the input by one frame, because upstream overlaps each frame with the
  previous one.
- `Update(frame)` is `speex_preprocess_estimate_update`: it feeds a frame
  into the noise estimate without producing output.
- `SpeechProbability`, `AGCGain` (dB), `Loudness`, `PSD` and `NoisePSD` read
  upstream's state without the integer rounding its getters apply.
- `Config.Echo` links a canceller for residual echo suppression. It must use
  the same frame size and sample rate. Run the canceller on each frame first.
  Upstream applies the residual echo gains together with the noise gains, so
  `DisableDenoise` turns off residual echo suppression as well.
- Bad settings return `ErrInvalidConfig`, bad frames `ErrInvalidAudio`.

Upstream's interface is 16-bit integer audio. This one is float32 in [-1, 1],
scaled by 32768 internally so upstream's constants, which are in 16-bit units,
apply unchanged. Outputs are saturated to the int16 range as upstream's are,
but they are not rounded to 16 bits. The canceller computes its internal echo
history from the rounded output, exactly as upstream does, so its state does
not depend on the rounding.

## Threading and sharing

A `Canceller` and a `Preprocessor` are not safe for concurrent use, and
neither starts goroutines. A linked preprocessor reads its canceller's state
in every `Run`, so use the pair from one goroutine. For many audio streams,
create one canceller and one preprocessor per stream. `Cancel`, `Run` and
`Update` do not allocate.

## Recommended settings for 16 kHz voice

The settings in the example above: 256-sample frames, a 4000-sample (250 ms)
filter, and the linked preprocessor with denoising at -10 dB and echo
suppression at -40/-15 dB. Run the canceller only while the
speaker is playing, plus a hold of about 1 s for the echo tail. Leave the AGC
off if anything downstream compares levels.

## Measured quality

From `TestQuality`, on the scenarios in `testdata/scenarios`. Each scenario is
built from real speech through a simulated room: 80 ms delay, -6 dB, 300 ms
RT60, a tanh speaker nonlinearity and -65 dBFS noise. The C reference gives
the same numbers.

| Scenario | Canceller | Canceller + preprocessor |
|---|---|---|
| Echo only, ERLE over the last 4 s of 8 | 17.7 dB | 32.9 dB |
| Echo only, 160-sample frames | 18.6 dB | 33.1 dB |
| Echo only, linear speaker (6 s clip) | 10.1 dB | 23.9 dB |
| Echo only, time to 10 dB / 20 dB in a 250 ms window | 3.0 s | 2.25 s |
| Echo path change at 5 s, time back to 10 dB | 3.0 s | |
| Double-talk, correlation with the near end in its two spurts (mic: 0.84, 0.78) | 0.95, 0.95 | 0.96, 0.96 |
| Noise between utterances, -35 dBFS fan noise | | -15.6 dB |
| Speech in the same noise | | -0.2 dB |
| AGC: -37.5 dBFS talker | | -28.5 dBFS |
| AGC: -17.1 dBFS talker | | -21.7 dBFS |

These are Speex's numbers, not promises for a real room: wonky-talkie measured
3.5 to 3.9 dB of echo removed by this canceller in its first real-room tests.
The canceller is linear. Speaker distortion, clock drift between the speaker
and the mic, and a misaligned reference all limit it.

## Cost

Per frame, `go test -bench .`, Go 1.26:

| | AMD Ryzen 5 5600X | Jetson Orin (Cortex-A78AE) |
|---|---|---|
| Canceller, 256 samples, 4000-tap filter | 50 µs | 103 µs |
| Canceller, 160 samples, 4000-tap filter | 43 µs | 83 µs |
| Preprocessor, 256 samples | 16 µs | 30 µs |
| Preprocessor with AGC | 19 µs | 31 µs |
| Canceller + linked preprocessor (talkie's stage) | 63 µs | 130 µs |

A 256-sample frame is 16 ms of audio, so talkie's stage uses about 0.4% of a
desktop core and 0.8% of an Orin core. The C library built with gcc -O2 takes
35 µs and 48 µs for the first and last rows on the desktop.

## Parity

`tools/fixtures` compiles the pinned upstream source (tag `SpeexDSP-1.2.1`,
commit `1b28a0f`) with `-DFLOATING_POINT -DUSE_SMALLFT -O2 -ffp-contract=off`
and records its outputs on the scenarios into `testdata/reference`. The tests
check:

- `internal/fft`: `spx_fft`/`spx_ifft` bit for bit, for 29 sizes from 1 to
  2048, including 320, 882 and other non-powers of two.
- `echo`: on all 11 runs, every output sample rounds to C's int16 (at most
  0.5 LSB apart, C's rounding), and the leak estimate is bit-identical in
  every frame. One run resets the canceller mid-stream.
- `preprocess`: the same for the output, and bit-identical speech
  probability, AGC gain, loudness and speech decision in every frame. The runs
  cover the linked canceller, 160- and 256-sample frames, no denoise, AGC with
  default and custom steps, the VAD, and the estimate update.

Parity holds on amd64 (GOAMD64 v1 and v3) and arm64 (Jetson Orin). Go may
fuse `x*y + z` into one multiply-add on arm64 and with GOAMD64=v3, which
changes the rounding. The port therefore wraps the products that feed sums in
explicit conversions, which the Go spec says prevent fusion. CI runs the tests
with GOAMD64=v3 as well.

## Development

```bash
go vet ./...
go test -race ./...                 # everything is pure Go; nothing skips
GOAMD64=v3 go test ./...            # parity with FMA available
go test -run '^$' -bench . ./...
```

To re-record the fixtures, which CI never does, check out the pinned source
and run the recorder; the result must be byte-identical to the committed
files:

```bash
git clone https://github.com/xiph/speexdsp /tmp/speexdsp
git -C /tmp/speexdsp checkout SpeexDSP-1.2.1
go run ./tools/fixtures -speexdsp /tmp/speexdsp
git status --short testdata        # empty
```

It needs gcc. The scenarios are composed from the clips in `testdata/clips`
(see its README).

## Known limits

- Float build only; the fixed-point paths are not ported.
- One microphone and one speaker (`speex_echo_state_init_mc` is not ported).
- The frames passed to `Cancel` must already be aligned. The playback-buffer
  API for unaligned devices (`speex_echo_capture`/`speex_echo_playback`) is
  not ported.
- Dereverberation is not ported; it is unfinished upstream and has no
  effect there.
- The preprocessor's speech probability is a crude voice activity detector.
  Use [go-silero-vad](https://github.com/androiddrew/go-silero-vad) to detect
  speech.
- The test data is about 11 MB and is part of the module download.

## License

BSD-3-Clause, the license of the code it ports. See LICENSE, NOTICE and
THIRD_PARTY.md; each ported file keeps its upstream notice.
