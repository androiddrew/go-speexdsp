# Attribution and terms

go-speexdsp is BSD-3-Clause (LICENSE/NOTICE), the license of the code it
ports. Retained upstream notices are under `third_party/licenses/` and at
the top of each ported file.

| Component | Source/version | Terms |
|---|---|---|
| FFT (`internal/fft/smallft.go`) | `libspeexdsp/smallft.c`, SpeexDSP 1.2.1, commit `1b28a0f` | BSD-style, (C) 1994-2001 XIPHOPHORUS Company, `speexdsp-COPYING.txt` |
| FFT scaling (`internal/fft/fft.go`) | `libspeexdsp/fftwrap.c` (USE_SMALLFT), SpeexDSP 1.2.1 | BSD-3-Clause, Copyright (C) 2005-2006 Jean-Marc Valin, `speexdsp-COPYING.txt` |

Test fixtures (`testdata/`, not part of the library):

| Files | Source | Terms |
|---|---|---|
| `testdata/fft/in_*.f32` | Seeded random vectors written by `tools/fixtures` | BSD-3-Clause |
| `testdata/fft/ref_*.f32`, `testdata/reference.json` | SpeexDSP 1.2.1 `spx_fft`/`spx_ifft` outputs for the inputs above, recorded by `tools/fixtures` | BSD-3-Clause (upstream outputs) |
