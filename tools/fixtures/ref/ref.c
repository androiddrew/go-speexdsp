/* ref records SpeexDSP 1.2.1 outputs for go-speexdsp's parity tests.
 *
 * It is compiled by tools/fixtures together with the pinned upstream
 * sources (mdf.c preprocess.c filterbank.c fftwrap.c smallft.c) using
 * -DFLOATING_POINT -DUSE_SMALLFT -ffp-contract=off. All files are raw
 * little-endian float32 unless noted.
 *
 *   ref fft N in out
 *       For each N-value vector in `in`, writes spx_fft(x) then
 *       spx_ifft(spx_fft(x)) to `out`.
 */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "fftwrap.h"

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

int main(int argc, char **argv)
{
   if (argc >= 2 && strcmp(argv[1], "fft") == 0)
      return cmd_fft(argc - 2, argv + 2);
   fprintf(stderr, "usage: ref fft ...\n");
   return 2;
}
