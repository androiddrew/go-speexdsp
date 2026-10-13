package fft

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func readF32(t *testing.T, path string) []float32 {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:]))
	}
	return v
}

func fftSizes(t *testing.T) (sizes []int, vectors int) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "reference.json"))
	if err != nil {
		t.Fatal(err)
	}
	var ref struct {
		FFTSizes []int `json:"fft_sizes"`
		FFTVecs  int   `json:"fft_vectors"`
	}
	if err := json.Unmarshal(b, &ref); err != nil {
		t.Fatal(err)
	}
	return ref.FFTSizes, ref.FFTVecs
}

// TestParity checks Forward and Inverse against spx_fft and spx_ifft from
// the pinned C sources, bit for bit.
func TestParity(t *testing.T) {
	sizes, vectors := fftSizes(t)
	for _, n := range sizes {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			dir := filepath.Join("..", "..", "testdata", "fft")
			in := readF32(t, filepath.Join(dir, fmt.Sprintf("in_%d.f32", n)))
			ref := readF32(t, filepath.Join(dir, fmt.Sprintf("ref_%d.f32", n)))
			if len(in) != n*vectors || len(ref) != 2*n*vectors {
				t.Fatalf("fixture sizes %d, %d; want %d, %d", len(in), len(ref), n*vectors, 2*n*vectors)
			}
			f := New(n)
			fwd := make([]float32, n)
			inv := make([]float32, n)
			for v := range vectors {
				f.Forward(in[v*n:(v+1)*n], fwd)
				f.Inverse(fwd, inv)
				want := ref[2*v*n : 2*(v+1)*n]
				for i := range n {
					if math.Float32bits(fwd[i]) != math.Float32bits(want[i]) {
						t.Fatalf("vector %d: Forward[%d] = %v, C = %v", v, i, fwd[i], want[i])
					}
					if math.Float32bits(inv[i]) != math.Float32bits(want[n+i]) {
						t.Fatalf("vector %d: Inverse[%d] = %v, C = %v", v, i, inv[i], want[n+i])
					}
				}
			}
		})
	}
}

// TestLayout checks the packed half-complex layout on a cosine, so the
// layout the canceller relies on is pinned independently of the fixtures.
func TestLayout(t *testing.T) {
	const n, k = 16, 3
	f := New(n)
	in := make([]float32, n)
	for i := range in {
		in[i] = float32(math.Cos(2 * math.Pi * k * float64(i) / n))
	}
	out := make([]float32, n)
	f.Forward(in, out)
	for i, v := range out {
		want := 0.0
		if i == 2*k-1 {
			want = 0.5 // real part of bin k, scaled by 1/n
		}
		if math.Abs(float64(v)-want) > 1e-6 {
			t.Errorf("out[%d] = %v, want %v", i, v, want)
		}
	}
}

func BenchmarkForward512(b *testing.B) {
	f := New(512)
	in := make([]float32, 512)
	for i := range in {
		in[i] = float32(i % 17)
	}
	out := make([]float32, 512)
	for b.Loop() {
		f.Forward(in, out)
	}
}
