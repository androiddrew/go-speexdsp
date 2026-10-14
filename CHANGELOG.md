# Changelog

## [Unreleased]

## [0.1.0] - 2026-10-13

### Added

- Package `echo`: a pure-Go port of the SpeexDSP 1.2.1 echo canceller
  (`mdf.c`, float build, two-path filter) for one microphone and one speaker,
  taking float32 samples in [-1, 1].
  - `New(frameSize, filterLength, sampleRate)` and `Cancel(mic, played, out)`.
    `out` may be `mic`.
  - `Reset`, plus diagnostics: `Leak` (upstream's leak estimate) and
    `ImpulseResponse` (the learned echo path, in natural units).
- Package `preprocess`: a port of the SpeexDSP 1.2.1 preprocessor
  (`preprocess.c`, `filterbank.c`).
  - Noise suppression, residual echo suppression through a linked
    `echo.Canceller`, and AGC.
  - The speech probability, and the speech decision with `Config.VAD`.
  - `Update` (`speex_preprocess_estimate_update`).
  - `Config` zero values select upstream's defaults. `DisableDenoise` keeps
    denoising on by default.
  - `SpeechProbability`, `AGCGain`, `Loudness`, `PSD` and `NoisePSD` report
    upstream's state without its integer rounding.
- Input validation with `ErrInvalidConfig` and `ErrInvalidAudio`. `Cancel`,
  `Run` and `Update` do not allocate.
- Parity fixtures recorded by `tools/fixtures` from the pinned C source, over
  seven scenarios: echo only, linear echo, double-talk, echo path change,
  noise, level change, and silence with full scale.
  - Outputs match C within its int16 rounding on every sample.
  - Per-frame state matches bit for bit, on amd64 (GOAMD64 v1 and v3) and
    arm64.
- Quality tests:
  - The canceller removes 17.7 dB of echo on the echo-only scenario, 32.9 dB
    with the linked preprocessor.
  - Near-end correlation in double-talk rises from 0.84 to 0.95.
- Benchmarks: canceller plus linked preprocessor at 63 µs per 16 ms frame on
  a Ryzen 5 5600X, 130 µs on a Jetson Orin.

### Known limits

- Float build only. One microphone and one speaker. Frames must already be
  aligned (no `speex_echo_capture`/`speex_echo_playback`).
- Dereverberation is not ported; it is unfinished upstream.
- Outputs are saturated like upstream's but not rounded to 16 bits.
- A zero setting always selects the upstream default, so 0 dB of noise or
  echo suppression cannot be configured.
- About 11 MB of test data is part of the module download.

[Unreleased]: https://github.com/androiddrew/go-speexdsp/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/androiddrew/go-speexdsp/releases/tag/v0.1.0
