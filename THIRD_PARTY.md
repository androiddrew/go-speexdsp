# Attribution and terms

go-speexdsp is BSD-3-Clause (LICENSE/NOTICE), the license of the code it
ports. Retained upstream notices are under `third_party/licenses/` and at
the top of each ported file.

| Component | Source/version | Terms |
|---|---|---|
| Echo canceller (`internal/mdf/mdf.go`) | `libspeexdsp/mdf.c`, SpeexDSP 1.2.1, commit `1b28a0f` | BSD-3-Clause (author-name variant), Copyright (C) 2003-2008 Jean-Marc Valin; notice at the top of `mdf.go` |
| Preprocessor (`preprocess/preprocess.go`) | `libspeexdsp/preprocess.c`, SpeexDSP 1.2.1 | BSD-3-Clause (author-name variant), Copyright (C) 2003 Epic Games (written by Jean-Marc Valin), Copyright (C) 2004-2006 Epic Games; notice at the top of `preprocess.go` |
| Bark filter bank (`preprocess/filterbank.go`) | `libspeexdsp/filterbank.c`, SpeexDSP 1.2.1 | BSD-3-Clause, Copyright (C) 2006 Jean-Marc Valin; notice at the top of `filterbank.go` |
| FFT (`internal/fft/smallft.go`) | `libspeexdsp/smallft.c`, SpeexDSP 1.2.1, commit `1b28a0f` | BSD-style, (C) 1994-2001 XIPHOPHORUS Company, `speexdsp-COPYING.txt` |
| FFT scaling (`internal/fft/fft.go`) | `libspeexdsp/fftwrap.c` (USE_SMALLFT), SpeexDSP 1.2.1 | BSD-3-Clause, Copyright (C) 2005-2006 Jean-Marc Valin, `speexdsp-COPYING.txt` |

Test fixtures (`testdata/`, not part of the library):

| Files | Source | Terms |
|---|---|---|
| `testdata/fft/in_*.f32` | Seeded random vectors written by `tools/fixtures` | BSD-3-Clause |
| `testdata/clips/af_heart-weather.wav`, `am_michael-timer.wav` | Synthesized with Kokoro-82M v1.0 through kokoro-run (voices `af_heart`, `am_michael`), resampled to 16 kHz | Kokoro-82M is Apache-2.0 |
| `testdata/clips/user-command.wav`, `user-reply.wav` | Excerpts of `turn_on_the_office_lights_test_clip.wav` from openWakeWord (github.com/dscripka/openWakeWord) | Apache-2.0 |
| `testdata/scenarios/*/*.wav` | Composed from the clips above with seeded room simulation and noise by `tools/fixtures` | Apache-2.0 (from the clips) |
| `testdata/fft/ref_*.f32`, `testdata/reference/`, `testdata/reference.json` | SpeexDSP 1.2.1 outputs for the inputs above, recorded by `tools/fixtures` | BSD-3-Clause (upstream outputs) |
