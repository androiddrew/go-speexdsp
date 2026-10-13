Source recordings for the scenario fixtures, 16 kHz mono 16-bit PCM.

- `af_heart-weather.wav`, `am_michael-timer.wav`: the "agent". Synthesized
  with Kokoro-82M v1.0 (Apache-2.0) through kokoro-run, voices `af_heart` and
  `am_michael`, resampled from 24 kHz with ffmpeg and cut to 10 s and 8 s.
- `user-command.wav` ("So if we turn that off, does it work with") and
  `user-reply.wav` ("Turn on the office lights."): real human speech, samples
  416000-459200 and 496000-521600 of
  `notebooks/training_tutorial_data/turn_on_the_office_lights_test_clip.wav`
  from openWakeWord (https://github.com/dscripka/openWakeWord, Apache-2.0),
  as used by talkie-core's simulated room.
