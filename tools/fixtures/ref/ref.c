/* ref records SpeexDSP 1.2.1 outputs for go-speexdsp's parity tests.
 *
 * It is compiled by tools/fixtures together with the pinned upstream
 * sources (filterbank.c fftwrap.c smallft.c; it includes mdf.c and
 * preprocess.c itself) using -DFLOATING_POINT -DUSE_SMALLFT -ffp-contract=off. All files are raw
 * little-endian float32 unless noted.
 *
 *   ref fft N in out
 *       For each N-value vector in `in`, writes spx_fft(x) then
 *       spx_ifft(spx_fft(x)) to `out`.
 *
 *   ref run FRAME FILTER RATE mic.s16 ref.s16 outdir DENOISE NS ES ESA
 *           AGC AGC_LEVEL LINK AGC_INC AGC_DEC AGC_MAX VAD PSTART PCONT RESET
 *           UPDATE
 *       Runs mic and ref (raw int16) frame by frame through
 *       speex_echo_cancellation and then speex_preprocess_run, and writes
 *       outdir/aec.s16 (the canceller's output), outdir/pp.s16 (the
 *       preprocessor's) and outdir/frames.f32 with FRAME_FIELDS floats per
 *       frame (see below). AGC_INC, AGC_DEC, AGC_MAX, PSTART and PCONT of 0
 *       leave the upstream default. RESET > 0 calls speex_echo_state_reset
 *       before frame RESET. The first UPDATE frames go through
 *       speex_preprocess_estimate_update instead of speex_preprocess_run;
 *       their pp.s16 is the unchanged input and their VAD field is -1.
 *
 * mdf.c and preprocess.c are included rather than linked so the recorder
 * can read state the public API only exposes rounded to integers.
 */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "mdf.c"
#include "preprocess.c"

static float *read_f32(const char *path, long *count)
{
   FILE *f = fopen(path, "rb");
   long size;
   float *buf;
   if (!f) { perror(path); exit(1); }
   fseek(f, 0, SEEK_END);
   size = ftell(f);
   fseek(f, 0, SEEK_SET);
   buf = malloc(size > 0 ? size : 1);
   if (size > 0 && fread(buf, 1, size, f) != (size_t)size) { perror(path); exit(1); }
   fclose(f);
   *count = size / (long)sizeof(float);
   return buf;
}

static void write_f32(FILE *f, const float *buf, long count)
{
   if (fwrite(buf, sizeof(float), count, f) != (size_t)count) { perror("write"); exit(1); }
}

static FILE *create(const char *path)
{
   FILE *f = fopen(path, "wb");
   if (!f) { perror(path); exit(1); }
   return f;
}

static int cmd_fft(int argc, char **argv)
{
   int n, v;
   long count;
   float *in, *fwd, *inv;
   void *table;
   FILE *out;
   if (argc != 3) { fprintf(stderr, "usage: ref fft N in out\n"); return 2; }
   n = atoi(argv[0]);
   in = read_f32(argv[1], &count);
   if (n <= 0 || count % n != 0) { fprintf(stderr, "fft: %ld values is not a multiple of %d\n", count, n); return 1; }
   fwd = malloc(n * sizeof(float));
   inv = malloc(n * sizeof(float));
   table = spx_fft_init(n);
   out = create(argv[2]);
   for (v = 0; v < count / n; v++)
   {
      spx_fft(table, in + (long)v * n, fwd);
      spx_ifft(table, fwd, inv);
      write_f32(out, fwd, n);
      write_f32(out, inv, n);
   }
   fclose(out);
   spx_fft_destroy(table);
   free(in); free(fwd); free(inv);
   return 0;
}

static short *read_s16(const char *path, long *count)
{
   FILE *f = fopen(path, "rb");
   long size;
   short *buf;
   if (!f) { perror(path); exit(1); }
   fseek(f, 0, SEEK_END);
   size = ftell(f);
   fseek(f, 0, SEEK_SET);
   buf = malloc(size > 0 ? size : 1);
   if (size > 0 && fread(buf, 1, size, f) != (size_t)size) { perror(path); exit(1); }
   fclose(f);
   *count = size / (long)sizeof(short);
   return buf;
}

static FILE *create_in(const char *dir, const char *name)
{
   char path[4096];
   snprintf(path, sizeof path, "%s/%s", dir, name);
   return create(path);
}

/* Per-frame values in frames.f32, in this order. */
enum {
   F_LEAK,      /* SpeexEchoState.leak_estimate */
   F_ADAPTED,   /* SpeexEchoState.adapted */
   F_PROB,      /* SpeexPreprocessState.speech_prob */
   F_AGC_GAIN,  /* SpeexPreprocessState.agc_gain (linear) */
   F_LOUDNESS,  /* SpeexPreprocessState.loudness */
   F_VAD,       /* speex_preprocess_run's return value */
   FRAME_FIELDS
};

static int cmd_run(int argc, char **argv)
{
   int frame, filter, rate;
   int denoise, ns, es, esa, agc, link, inc, dec, maxgain, vad, pstart, pcont, reset, update;
   float agc_level;
   long nmic, nref, f, frames;
   short *mic, *ref, *out;
   SpeexEchoState *echo;
   SpeexPreprocessState *pp;
   FILE *aec_out, *pp_out, *frames_out;
   const char *dir;

   if (argc != 21) { fprintf(stderr, "usage: ref run FRAME FILTER RATE mic.s16 ref.s16 outdir DENOISE NS ES ESA AGC AGC_LEVEL LINK AGC_INC AGC_DEC AGC_MAX VAD PSTART PCONT RESET UPDATE\n"); return 2; }
   frame = atoi(argv[0]); filter = atoi(argv[1]); rate = atoi(argv[2]);
   mic = read_s16(argv[3], &nmic);
   ref = read_s16(argv[4], &nref);
   dir = argv[5];
   denoise = atoi(argv[6]); ns = atoi(argv[7]); es = atoi(argv[8]); esa = atoi(argv[9]);
   agc = atoi(argv[10]); agc_level = atof(argv[11]); link = atoi(argv[12]);
   inc = atoi(argv[13]); dec = atoi(argv[14]); maxgain = atoi(argv[15]);
   vad = atoi(argv[16]); pstart = atoi(argv[17]); pcont = atoi(argv[18]);
   reset = atoi(argv[19]); update = atoi(argv[20]);
   if (nmic != nref) { fprintf(stderr, "run: mic has %ld samples, ref %ld\n", nmic, nref); return 1; }

   echo = speex_echo_state_init(frame, filter);
   speex_echo_ctl(echo, SPEEX_ECHO_SET_SAMPLING_RATE, &rate);
   pp = speex_preprocess_state_init(frame, rate);
   speex_preprocess_ctl(pp, SPEEX_PREPROCESS_SET_DENOISE, &denoise);
   speex_preprocess_ctl(pp, SPEEX_PREPROCESS_SET_NOISE_SUPPRESS, &ns);
   speex_preprocess_ctl(pp, SPEEX_PREPROCESS_SET_ECHO_SUPPRESS, &es);
   speex_preprocess_ctl(pp, SPEEX_PREPROCESS_SET_ECHO_SUPPRESS_ACTIVE, &esa);
   speex_preprocess_ctl(pp, SPEEX_PREPROCESS_SET_AGC, &agc);
   speex_preprocess_ctl(pp, SPEEX_PREPROCESS_SET_AGC_LEVEL, &agc_level);
   if (inc) speex_preprocess_ctl(pp, SPEEX_PREPROCESS_SET_AGC_INCREMENT, &inc);
   if (dec) speex_preprocess_ctl(pp, SPEEX_PREPROCESS_SET_AGC_DECREMENT, &dec);
   if (maxgain) speex_preprocess_ctl(pp, SPEEX_PREPROCESS_SET_AGC_MAX_GAIN, &maxgain);
   speex_preprocess_ctl(pp, SPEEX_PREPROCESS_SET_VAD, &vad);
   if (pstart) speex_preprocess_ctl(pp, SPEEX_PREPROCESS_SET_PROB_START, &pstart);
   if (pcont) speex_preprocess_ctl(pp, SPEEX_PREPROCESS_SET_PROB_CONTINUE, &pcont);
   if (link) speex_preprocess_ctl(pp, SPEEX_PREPROCESS_SET_ECHO_STATE, echo);

   aec_out = create_in(dir, "aec.s16");
   pp_out = create_in(dir, "pp.s16");
   frames_out = create_in(dir, "frames.f32");
   out = malloc(frame * sizeof(short));
   frames = nmic / frame;
   for (f = 0; f < frames; f++)
   {
      float rec[FRAME_FIELDS];
      int speech;
      if (reset > 0 && f == reset)
         speex_echo_state_reset(echo);
      speex_echo_cancellation(echo, mic + f * frame, ref + f * frame, out);
      if (fwrite(out, sizeof(short), frame, aec_out) != (size_t)frame) { perror("write"); exit(1); }
      if (f < update)
      {
         speex_preprocess_estimate_update(pp, out);
         speech = -1;
      } else
         speech = speex_preprocess_run(pp, out);
      if (fwrite(out, sizeof(short), frame, pp_out) != (size_t)frame) { perror("write"); exit(1); }
      rec[F_LEAK] = echo->leak_estimate;
      rec[F_ADAPTED] = echo->adapted;
      rec[F_PROB] = pp->speech_prob;
      rec[F_AGC_GAIN] = pp->agc_gain;
      rec[F_LOUDNESS] = pp->loudness;
      rec[F_VAD] = speech;
      write_f32(frames_out, rec, FRAME_FIELDS);
   }
   fclose(aec_out); fclose(pp_out); fclose(frames_out);
   speex_preprocess_state_destroy(pp);
   speex_echo_state_destroy(echo);
   free(mic); free(ref); free(out);
   return 0;
}

int main(int argc, char **argv)
{
   if (argc >= 2 && strcmp(argv[1], "fft") == 0)
      return cmd_fft(argc - 2, argv + 2);
   if (argc >= 2 && strcmp(argv[1], "run") == 0)
      return cmd_run(argc - 2, argv + 2);
   fprintf(stderr, "usage: ref fft|run ...\n");
   return 2;
}
