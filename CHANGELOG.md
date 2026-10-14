# Changelog

## [Unreleased]

## [0.1.0] - 2026-10-13

### Added

- Package `echo`: the SpeexDSP 1.2.1 echo canceller (`mdf.c`, float build,
  two-path), one microphone and one speaker, float32 samples in [-1, 1].
- Package `preprocess`: the SpeexDSP 1.2.1 preprocessor (`preprocess.c`,
  `filterbank.c`): noise suppression, residual echo suppression through a
  linked canceller, AGC, the speech probability and decision, and the
  estimate update.
- The smallft FFT (`smallft.c`, `fftwrap.c`) as an internal package.
- Parity fixtures recorded from the pinned C source over seven scenarios
  (echo only, linear echo, double-talk, echo path change, noise, level change,
  silence and full scale). Outputs match C to within its int16 rounding on
  every sample, and the per-frame state matches bit for bit, on amd64 and
  arm64.

### Known limits

- Float build only; one microphone and one speaker; frames must already be
  aligned (no `speex_echo_capture`/`speex_echo_playback`).
- Dereverberation is not ported (unfinished upstream).
- Outputs are saturated like upstream's but not rounded to 16 bits.
- A zero setting always selects the upstream default, so 0 dB of noise or
  echo suppression cannot be configured.
- About 11 MB of test data is part of the module.

[Unreleased]: https://github.com/androiddrew/go-speexdsp/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/androiddrew/go-speexdsp/releases/tag/v0.1.0
