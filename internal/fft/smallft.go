// Ported from SpeexDSP 1.2.1 libspeexdsp/smallft.c, which carries this notice:
//
//	THIS FILE IS PART OF THE OggVorbis SOFTWARE CODEC SOURCE CODE.
//	USE, DISTRIBUTION AND REPRODUCTION OF THIS LIBRARY SOURCE IS
//	GOVERNED BY A BSD-STYLE SOURCE LICENSE INCLUDED WITH THIS SOURCE
//	IN 'COPYING'. PLEASE READ THESE TERMS BEFORE DISTRIBUTING.
//
//	THE OggVorbis SOURCE CODE IS (C) COPYRIGHT 1994-2001
//	by the XIPHOPHORUS Company http://www.xiph.org/
//
// The translation keeps upstream's index arithmetic and gotos line for line,
// because the parity tests require the same float32 operations in the same
// order. Products that are summed are wrapped in float32() so the compiler
// cannot fuse them into multiply-adds (the Go spec allows fusion otherwise,
// and arm64 and GOAMD64=v3 do it); the C reference is built with
// -ffp-contract=off to match.

package fft

import "math"

// drfti1 factors n and fills the twiddle table wa (smallft.c drfti1).
func drfti1(n int, wa []float32, ifac []int) {
	ntryh := [4]int{4, 2, 3, 5}
	const tpi = float32(6.28318530717958648)
	var arg, argh, argld, fi float32
	var ntry, i, ib, ld, ii, ip, is, nq, nr, ido, ipm, nfm1, k1, l1, l2 int
	j := -1
	nl := n
	nf := 0

L101:
	j++
	if j < 4 {
		ntry = ntryh[j]
	} else {
		ntry += 2
	}

L104:
	nq = nl / ntry
	nr = nl - ntry*nq
	if nr != 0 {
		goto L101
	}

	nf++
	ifac[nf+1] = ntry
	nl = nq
	if ntry != 2 {
		goto L107
	}
	if nf == 1 {
		goto L107
	}

	for i = 1; i < nf; i++ {
		ib = nf - i + 1
		ifac[ib+1] = ifac[ib]
	}
	ifac[2] = 2

L107:
	if nl != 1 {
		goto L104
	}
	ifac[0] = n
	ifac[1] = nf
	argh = tpi / float32(n)
	is = 0
	nfm1 = nf - 1
	l1 = 1

	if nfm1 == 0 {
		return
	}

	for k1 = 0; k1 < nfm1; k1++ {
		ip = ifac[k1+2]
		ld = 0
		l2 = l1 * ip
		ido = n / l2
		ipm = ip - 1

		for j = 0; j < ipm; j++ {
			ld += l1
			i = is
			argld = float32(ld) * argh
			fi = 0
			for ii = 2; ii < ido; ii += 2 {
				fi += 1
				arg = fi * argld
				// C calls the double-precision cos/sin and narrows the result.
				wa[i] = float32(math.Cos(float64(arg)))
				i++
				wa[i] = float32(math.Sin(float64(arg)))
				i++
			}
			is += ido
		}
		l1 = l2
	}
}

// fdrffti (smallft.c fdrffti).
func fdrffti(n int, wsave []float32, ifac []int) {
	if n == 1 {
		return
	}
	drfti1(n, wsave[n:], ifac)
}

// dradf2 is the radix-2 forward pass (smallft.c dradf2).
func dradf2(ido, l1 int, cc, ch, wa1 []float32) {
	var i, k, t0, t1, t2, t3, t4, t5, t6 int
	var ti2, tr2 float32

	t1 = 0
	t2 = l1 * ido
	t0 = t2
	t3 = ido << 1
	for k = 0; k < l1; k++ {
		ch[t1<<1] = cc[t1] + cc[t2]
		ch[(t1<<1)+t3-1] = cc[t1] - cc[t2]
		t1 += ido
		t2 += ido
	}

	if ido < 2 {
		return
	}
	if ido == 2 {
		goto L105
	}

	t1 = 0
	t2 = t0
	for k = 0; k < l1; k++ {
		t3 = t2
		t4 = (t1 << 1) + (ido << 1)
		t5 = t1
		t6 = t1 + t1
		for i = 2; i < ido; i += 2 {
			t3 += 2
			t4 -= 2
			t5 += 2
			t6 += 2
			tr2 = float32(wa1[i-2]*cc[t3-1]) + float32(wa1[i-1]*cc[t3])
			ti2 = float32(wa1[i-2]*cc[t3]) - float32(wa1[i-1]*cc[t3-1])
			ch[t6] = cc[t5] + ti2
			ch[t4] = ti2 - cc[t5]
			ch[t6-1] = cc[t5-1] + tr2
			ch[t4-1] = cc[t5-1] - tr2
		}
		t1 += ido
		t2 += ido
	}

	if ido%2 == 1 {
		return
	}

L105:
	t1 = ido
	t2 = t1 - 1
	t3 = t2
	t2 += t0
	for k = 0; k < l1; k++ {
		ch[t1] = -cc[t2]
		ch[t1-1] = cc[t3]
		t1 += ido << 1
		t2 += ido
		t3 += ido
	}
}

// dradf4 is the radix-4 forward pass (smallft.c dradf4).
func dradf4(ido, l1 int, cc, ch, wa1, wa2, wa3 []float32) {
	const hsqt2 = float32(.70710678118654752)
	var i, k, t0, t1, t2, t3, t4, t5, t6 int
	var ci2, ci3, ci4, cr2, cr3, cr4, ti1, ti2, ti3, ti4, tr1, tr2, tr3, tr4 float32
	t0 = l1 * ido

	t1 = t0
	t4 = t1 << 1
	t2 = t1 + (t1 << 1)
	t3 = 0

	for k = 0; k < l1; k++ {
		tr1 = cc[t1] + cc[t2]
		tr2 = cc[t3] + cc[t4]

		t5 = t3 << 2
		ch[t5] = tr1 + tr2
		ch[(ido<<2)+t5-1] = tr2 - tr1
		t5 += ido << 1
		ch[t5-1] = cc[t3] - cc[t4]
		ch[t5] = cc[t2] - cc[t1]

		t1 += ido
		t2 += ido
		t3 += ido
		t4 += ido
	}

	if ido < 2 {
		return
	}
	if ido == 2 {
		goto L105
	}

	t1 = 0
	for k = 0; k < l1; k++ {
		t2 = t1
		t4 = t1 << 2
		t6 = ido << 1
		t5 = t6 + t4
		for i = 2; i < ido; i += 2 {
			t2 += 2
			t3 = t2
			t4 += 2
			t5 -= 2

			t3 += t0
			cr2 = float32(wa1[i-2]*cc[t3-1]) + float32(wa1[i-1]*cc[t3])
			ci2 = float32(wa1[i-2]*cc[t3]) - float32(wa1[i-1]*cc[t3-1])
			t3 += t0
			cr3 = float32(wa2[i-2]*cc[t3-1]) + float32(wa2[i-1]*cc[t3])
			ci3 = float32(wa2[i-2]*cc[t3]) - float32(wa2[i-1]*cc[t3-1])
			t3 += t0
			cr4 = float32(wa3[i-2]*cc[t3-1]) + float32(wa3[i-1]*cc[t3])
			ci4 = float32(wa3[i-2]*cc[t3]) - float32(wa3[i-1]*cc[t3-1])

			tr1 = cr2 + cr4
			tr4 = cr4 - cr2
			ti1 = ci2 + ci4
			ti4 = ci2 - ci4

			ti2 = cc[t2] + ci3
			ti3 = cc[t2] - ci3
			tr2 = cc[t2-1] + cr3
			tr3 = cc[t2-1] - cr3

			ch[t4-1] = tr1 + tr2
			ch[t4] = ti1 + ti2

			ch[t5-1] = tr3 - ti4
			ch[t5] = tr4 - ti3

			ch[t4+t6-1] = ti4 + tr3
			ch[t4+t6] = tr4 + ti3

			ch[t5+t6-1] = tr2 - tr1
			ch[t5+t6] = ti1 - ti2
		}
		t1 += ido
	}
	if ido&1 != 0 {
		return
	}

L105:
	t1 = t0 + ido - 1
	t2 = t1 + (t0 << 1)
	t3 = ido << 2
	t4 = ido
	t5 = ido << 1
	t6 = ido

	for k = 0; k < l1; k++ {
		ti1 = float32(-hsqt2 * (cc[t1] + cc[t2]))
		tr1 = float32(hsqt2 * (cc[t1] - cc[t2]))

		ch[t4-1] = tr1 + cc[t6-1]
		ch[t4+t5-1] = cc[t6-1] - tr1

		ch[t4] = ti1 - cc[t1+t0]
		ch[t4+t5] = ti1 + cc[t1+t0]

		t1 += ido
		t2 += ido
		t4 += t3
		t6 += ido
	}
}

// dradfg is the general-radix forward pass (smallft.c dradfg). Upstream
// passes the same buffer as cc, c1 and c2, and as ch and ch2.
func dradfg(ido, ip, l1, idl1 int, cc, c1, c2, ch, ch2, wa []float32) {
	const tpi = float32(6.283185307179586)
	var idij, ipph, i, j, k, l, ic, ik, is int
	var t0, t1, t2, t3, t4, t5, t6, t7, t8, t9, t10 int
	var dc2, ai1, ai2, ar1, ar2, ds2 float32
	var nbd int
	var dcp, arg, dsp, ar1h, ar2h float32
	var idp2, ipp2 int

	arg = tpi / float32(ip)
	dcp = float32(math.Cos(float64(arg)))
	dsp = float32(math.Sin(float64(arg)))
	ipph = (ip + 1) >> 1
	ipp2 = ip
	idp2 = ido
	nbd = (ido - 1) >> 1
	t0 = l1 * ido
	t10 = ip * ido

	if ido == 1 {
		goto L119
	}
	for ik = 0; ik < idl1; ik++ {
		ch2[ik] = c2[ik]
	}

	t1 = 0
	for j = 1; j < ip; j++ {
		t1 += t0
		t2 = t1
		for k = 0; k < l1; k++ {
			ch[t2] = c1[t2]
			t2 += ido
		}
	}

	is = -ido
	t1 = 0
	if nbd > l1 {
		for j = 1; j < ip; j++ {
			t1 += t0
			is += ido
			t2 = -ido + t1
			for k = 0; k < l1; k++ {
				idij = is - 1
				t2 += ido
				t3 = t2
				for i = 2; i < ido; i += 2 {
					idij += 2
					t3 += 2
					ch[t3-1] = float32(wa[idij-1]*c1[t3-1]) + float32(wa[idij]*c1[t3])
					ch[t3] = float32(wa[idij-1]*c1[t3]) - float32(wa[idij]*c1[t3-1])
				}
			}
		}
	} else {
		for j = 1; j < ip; j++ {
			is += ido
			idij = is - 1
			t1 += t0
			t2 = t1
			for i = 2; i < ido; i += 2 {
				idij += 2
				t2 += 2
				t3 = t2
				for k = 0; k < l1; k++ {
					ch[t3-1] = float32(wa[idij-1]*c1[t3-1]) + float32(wa[idij]*c1[t3])
					ch[t3] = float32(wa[idij-1]*c1[t3]) - float32(wa[idij]*c1[t3-1])
					t3 += ido
				}
			}
		}
	}

	t1 = 0
	t2 = ipp2 * t0
	if nbd < l1 {
		for j = 1; j < ipph; j++ {
			t1 += t0
			t2 -= t0
			t3 = t1
			t4 = t2
			for i = 2; i < ido; i += 2 {
				t3 += 2
				t4 += 2
				t5 = t3 - ido
				t6 = t4 - ido
				for k = 0; k < l1; k++ {
					t5 += ido
					t6 += ido
					c1[t5-1] = ch[t5-1] + ch[t6-1]
					c1[t6-1] = ch[t5] - ch[t6]
					c1[t5] = ch[t5] + ch[t6]
					c1[t6] = ch[t6-1] - ch[t5-1]
				}
			}
		}
	} else {
		for j = 1; j < ipph; j++ {
			t1 += t0
			t2 -= t0
			t3 = t1
			t4 = t2
			for k = 0; k < l1; k++ {
				t5 = t3
				t6 = t4
				for i = 2; i < ido; i += 2 {
					t5 += 2
					t6 += 2
					c1[t5-1] = ch[t5-1] + ch[t6-1]
					c1[t6-1] = ch[t5] - ch[t6]
					c1[t5] = ch[t5] + ch[t6]
					c1[t6] = ch[t6-1] - ch[t5-1]
				}
				t3 += ido
				t4 += ido
			}
		}
	}

L119:
	for ik = 0; ik < idl1; ik++ {
		c2[ik] = ch2[ik]
	}

	t1 = 0
	t2 = ipp2 * idl1
	for j = 1; j < ipph; j++ {
		t1 += t0
		t2 -= t0
		t3 = t1 - ido
		t4 = t2 - ido
		for k = 0; k < l1; k++ {
			t3 += ido
			t4 += ido
			c1[t3] = ch[t3] + ch[t4]
			c1[t4] = ch[t4] - ch[t3]
		}
	}

	ar1 = 1
	ai1 = 0
	t1 = 0
	t2 = ipp2 * idl1
	t3 = (ip - 1) * idl1
	for l = 1; l < ipph; l++ {
		t1 += idl1
		t2 -= idl1
		ar1h = float32(dcp*ar1) - float32(dsp*ai1)
		ai1 = float32(dcp*ai1) + float32(dsp*ar1)
		ar1 = ar1h
		t4 = t1
		t5 = t2
		t6 = t3
		t7 = idl1

		for ik = 0; ik < idl1; ik++ {
			ch2[t4] = c2[ik] + float32(ar1*c2[t7])
			t4++
			t7++
			ch2[t5] = ai1 * c2[t6]
			t5++
			t6++
		}

		dc2 = ar1
		ds2 = ai1
		ar2 = ar1
		ai2 = ai1

		t4 = idl1
		t5 = (ipp2 - 1) * idl1
		for j = 2; j < ipph; j++ {
			t4 += idl1
			t5 -= idl1

			ar2h = float32(dc2*ar2) - float32(ds2*ai2)
			ai2 = float32(dc2*ai2) + float32(ds2*ar2)
			ar2 = ar2h

			t6 = t1
			t7 = t2
			t8 = t4
			t9 = t5
			for ik = 0; ik < idl1; ik++ {
				ch2[t6] += float32(ar2 * c2[t8])
				t6++
				t8++
				ch2[t7] += float32(ai2 * c2[t9])
				t7++
				t9++
			}
		}
	}

	t1 = 0
	for j = 1; j < ipph; j++ {
		t1 += idl1
		t2 = t1
		for ik = 0; ik < idl1; ik++ {
			ch2[ik] += c2[t2]
			t2++
		}
	}

	if ido < l1 {
		goto L132
	}

	t1 = 0
	t2 = 0
	for k = 0; k < l1; k++ {
		t3 = t1
		t4 = t2
		for i = 0; i < ido; i++ {
			cc[t4] = ch[t3]
			t4++
			t3++
		}
		t1 += ido
		t2 += t10
	}

	goto L135

L132:
	for i = 0; i < ido; i++ {
		t1 = i
		t2 = i
		for k = 0; k < l1; k++ {
			cc[t2] = ch[t1]
			t1 += ido
			t2 += t10
		}
	}

L135:
	t1 = 0
	t2 = ido << 1
	t3 = 0
	t4 = ipp2 * t0
	for j = 1; j < ipph; j++ {
		t1 += t2
		t3 += t0
		t4 -= t0

		t5 = t1
		t6 = t3
		t7 = t4

		for k = 0; k < l1; k++ {
			cc[t5-1] = ch[t6]
			cc[t5] = ch[t7]
			t5 += t10
			t6 += ido
			t7 += ido
		}
	}

	if ido == 1 {
		return
	}
	if nbd < l1 {
		goto L141
	}

	t1 = -ido
	t3 = 0
	t4 = 0
	t5 = ipp2 * t0
	for j = 1; j < ipph; j++ {
		t1 += t2
		t3 += t2
		t4 += t0
		t5 -= t0
		t6 = t1
		t7 = t3
		t8 = t4
		t9 = t5
		for k = 0; k < l1; k++ {
			for i = 2; i < ido; i += 2 {
				ic = idp2 - i
				cc[i+t7-1] = ch[i+t8-1] + ch[i+t9-1]
				cc[ic+t6-1] = ch[i+t8-1] - ch[i+t9-1]
				cc[i+t7] = ch[i+t8] + ch[i+t9]
				cc[ic+t6] = ch[i+t9] - ch[i+t8]
			}
			t6 += t10
			t7 += t10
			t8 += ido
			t9 += ido
		}
	}
	return

L141:
	t1 = -ido
	t3 = 0
	t4 = 0
	t5 = ipp2 * t0
	for j = 1; j < ipph; j++ {
		t1 += t2
		t3 += t2
		t4 += t0
		t5 -= t0
		for i = 2; i < ido; i += 2 {
			t6 = idp2 + t1 - i
			t7 = i + t3
			t8 = i + t4
			t9 = i + t5
			for k = 0; k < l1; k++ {
				cc[t7-1] = ch[t8-1] + ch[t9-1]
				cc[t6-1] = ch[t8-1] - ch[t9-1]
				cc[t7] = ch[t8] + ch[t9]
				cc[t6] = ch[t9] - ch[t8]
				t6 += t10
				t7 += t10
				t8 += ido
				t9 += ido
			}
		}
	}
}

// drftf1 runs the forward passes for each factor (smallft.c drftf1).
func drftf1(n int, c, ch, wa []float32, ifac []int) {
	var i, k1, l1, l2, na, kh, nf, ip, iw, ido, idl1, ix2, ix3 int

	nf = ifac[1]
	na = 1
	l2 = n
	iw = n

	for k1 = 0; k1 < nf; k1++ {
		kh = nf - k1
		ip = ifac[kh+1]
		l1 = l2 / ip
		ido = n / l2
		idl1 = ido * l1
		iw -= (ip - 1) * ido
		na = 1 - na

		if ip != 4 {
			goto L102
		}

		ix2 = iw + ido
		ix3 = ix2 + ido
		if na != 0 {
			dradf4(ido, l1, ch, c, wa[iw-1:], wa[ix2-1:], wa[ix3-1:])
		} else {
			dradf4(ido, l1, c, ch, wa[iw-1:], wa[ix2-1:], wa[ix3-1:])
		}
		goto L110

	L102:
		if ip != 2 {
			goto L104
		}
		if na != 0 {
			goto L103
		}

		dradf2(ido, l1, c, ch, wa[iw-1:])
		goto L110

	L103:
		dradf2(ido, l1, ch, c, wa[iw-1:])
		goto L110

	L104:
		if ido == 1 {
			na = 1 - na
		}
		if na != 0 {
			goto L109
		}

		dradfg(ido, ip, l1, idl1, c, c, c, ch, ch, wa[iw-1:])
		na = 1
		goto L110

	L109:
		dradfg(ido, ip, l1, idl1, ch, ch, ch, c, c, wa[iw-1:])
		na = 0

	L110:
		l2 = l1
	}

	if na == 1 {
		return
	}

	for i = 0; i < n; i++ {
		c[i] = ch[i]
	}
}

// dradb2 is the radix-2 backward pass (smallft.c dradb2).
func dradb2(ido, l1 int, cc, ch, wa1 []float32) {
	var i, k, t0, t1, t2, t3, t4, t5, t6 int
	var ti2, tr2 float32

	t0 = l1 * ido

	t1 = 0
	t2 = 0
	t3 = (ido << 1) - 1
	for k = 0; k < l1; k++ {
		ch[t1] = cc[t2] + cc[t3+t2]
		ch[t1+t0] = cc[t2] - cc[t3+t2]
		t1 += ido
		t2 = t1 << 1
	}

	if ido < 2 {
		return
	}
	if ido == 2 {
		goto L105
	}

	t1 = 0
	t2 = 0
	for k = 0; k < l1; k++ {
		t3 = t1
		t4 = t2
		t5 = t4 + (ido << 1)
		t6 = t0 + t1
		for i = 2; i < ido; i += 2 {
			t3 += 2
			t4 += 2
			t5 -= 2
			t6 += 2
			ch[t3-1] = cc[t4-1] + cc[t5-1]
			tr2 = cc[t4-1] - cc[t5-1]
			ch[t3] = cc[t4] - cc[t5]
			ti2 = cc[t4] + cc[t5]
			ch[t6-1] = float32(wa1[i-2]*tr2) - float32(wa1[i-1]*ti2)
			ch[t6] = float32(wa1[i-2]*ti2) + float32(wa1[i-1]*tr2)
		}
		t1 += ido
		t2 = t1 << 1
	}

	if ido%2 == 1 {
		return
	}

L105:
	t1 = ido - 1
	t2 = ido - 1
	for k = 0; k < l1; k++ {
		ch[t1] = cc[t2] + cc[t2]
		ch[t1+t0] = -(cc[t2+1] + cc[t2+1])
		t1 += ido
		t2 += ido << 1
	}
}

// dradb3 is the radix-3 backward pass (smallft.c dradb3).
func dradb3(ido, l1 int, cc, ch, wa1, wa2 []float32) {
	const taur = float32(-.5)
	const taui = float32(.8660254037844386)
	var i, k, t0, t1, t2, t3, t4, t5, t6, t7, t8, t9, t10 int
	var ci2, ci3, di2, di3, cr2, cr3, dr2, dr3, ti2, tr2 float32
	t0 = l1 * ido

	t1 = 0
	t2 = t0 << 1
	t3 = ido << 1
	t4 = ido + (ido << 1)
	t5 = 0
	for k = 0; k < l1; k++ {
		tr2 = cc[t3-1] + cc[t3-1]
		cr2 = cc[t5] + float32(taur*tr2)
		ch[t1] = cc[t5] + tr2
		ci3 = float32(taui * (cc[t3] + cc[t3]))
		ch[t1+t0] = cr2 - ci3
		ch[t1+t2] = cr2 + ci3
		t1 += ido
		t3 += t4
		t5 += t4
	}

	if ido == 1 {
		return
	}

	t1 = 0
	t3 = ido << 1
	for k = 0; k < l1; k++ {
		t7 = t1 + (t1 << 1)
		t5 = t7 + t3
		t6 = t5
		t8 = t1
		t9 = t1 + t0
		t10 = t9 + t0

		for i = 2; i < ido; i += 2 {
			t5 += 2
			t6 -= 2
			t7 += 2
			t8 += 2
			t9 += 2
			t10 += 2
			tr2 = cc[t5-1] + cc[t6-1]
			cr2 = cc[t7-1] + float32(taur*tr2)
			ch[t8-1] = cc[t7-1] + tr2
			ti2 = cc[t5] - cc[t6]
			ci2 = cc[t7] + float32(taur*ti2)
			ch[t8] = cc[t7] + ti2
			cr3 = float32(taui * (cc[t5-1] - cc[t6-1]))
			ci3 = float32(taui * (cc[t5] + cc[t6]))
			dr2 = cr2 - ci3
			dr3 = cr2 + ci3
			di2 = ci2 + cr3
			di3 = ci2 - cr3
			ch[t9-1] = float32(wa1[i-2]*dr2) - float32(wa1[i-1]*di2)
			ch[t9] = float32(wa1[i-2]*di2) + float32(wa1[i-1]*dr2)
			ch[t10-1] = float32(wa2[i-2]*dr3) - float32(wa2[i-1]*di3)
			ch[t10] = float32(wa2[i-2]*di3) + float32(wa2[i-1]*dr3)
		}
		t1 += ido
	}
}

// dradb4 is the radix-4 backward pass (smallft.c dradb4).
func dradb4(ido, l1 int, cc, ch, wa1, wa2, wa3 []float32) {
	const sqrt2 = float32(1.414213562373095)
	var i, k, t0, t1, t2, t3, t4, t5, t6, t7, t8 int
	var ci2, ci3, ci4, cr2, cr3, cr4, ti1, ti2, ti3, ti4, tr1, tr2, tr3, tr4 float32
	t0 = l1 * ido

	t1 = 0
	t2 = ido << 2
	t3 = 0
	t6 = ido << 1
	for k = 0; k < l1; k++ {
		t4 = t3 + t6
		t5 = t1
		tr3 = cc[t4-1] + cc[t4-1]
		tr4 = cc[t4] + cc[t4]
		t4 += t6
		tr1 = cc[t3] - cc[t4-1]
		tr2 = cc[t3] + cc[t4-1]
		ch[t5] = tr2 + tr3
		t5 += t0
		ch[t5] = tr1 - tr4
		t5 += t0
		ch[t5] = tr2 - tr3
		t5 += t0
		ch[t5] = tr1 + tr4
		t1 += ido
		t3 += t2
	}

	if ido < 2 {
		return
	}
	if ido == 2 {
		goto L105
	}

	t1 = 0
	for k = 0; k < l1; k++ {
		t2 = t1 << 2
		t3 = t2 + t6
		t4 = t3
		t5 = t4 + t6
		t7 = t1
		for i = 2; i < ido; i += 2 {
			t2 += 2
			t3 += 2
			t4 -= 2
			t5 -= 2
			t7 += 2
			ti1 = cc[t2] + cc[t5]
			ti2 = cc[t2] - cc[t5]
			ti3 = cc[t3] - cc[t4]
			tr4 = cc[t3] + cc[t4]
			tr1 = cc[t2-1] - cc[t5-1]
			tr2 = cc[t2-1] + cc[t5-1]
			ti4 = cc[t3-1] - cc[t4-1]
			tr3 = cc[t3-1] + cc[t4-1]
			ch[t7-1] = tr2 + tr3
			cr3 = tr2 - tr3
			ch[t7] = ti2 + ti3
			ci3 = ti2 - ti3
			cr2 = tr1 - tr4
			cr4 = tr1 + tr4
			ci2 = ti1 + ti4
			ci4 = ti1 - ti4

			t8 = t7 + t0
			ch[t8-1] = float32(wa1[i-2]*cr2) - float32(wa1[i-1]*ci2)
			ch[t8] = float32(wa1[i-2]*ci2) + float32(wa1[i-1]*cr2)
			t8 += t0
			ch[t8-1] = float32(wa2[i-2]*cr3) - float32(wa2[i-1]*ci3)
			ch[t8] = float32(wa2[i-2]*ci3) + float32(wa2[i-1]*cr3)
			t8 += t0
			ch[t8-1] = float32(wa3[i-2]*cr4) - float32(wa3[i-1]*ci4)
			ch[t8] = float32(wa3[i-2]*ci4) + float32(wa3[i-1]*cr4)
		}
		t1 += ido
	}

	if ido%2 == 1 {
		return
	}

L105:
	t1 = ido
	t2 = ido << 2
	t3 = ido - 1
	t4 = ido + (ido << 1)
	for k = 0; k < l1; k++ {
		t5 = t3
		ti1 = cc[t1] + cc[t4]
		ti2 = cc[t4] - cc[t1]
		tr1 = cc[t1-1] - cc[t4-1]
		tr2 = cc[t1-1] + cc[t4-1]
		ch[t5] = tr2 + tr2
		t5 += t0
		ch[t5] = sqrt2 * (tr1 - ti1)
		t5 += t0
		ch[t5] = ti2 + ti2
		t5 += t0
		ch[t5] = -sqrt2 * (tr1 + ti1)

		t3 += ido
		t1 += t2
		t4 += t2
	}
}

// dradbg is the general-radix backward pass (smallft.c dradbg). Upstream
// passes the same buffer as cc, c1 and c2, and as ch and ch2.
func dradbg(ido, ip, l1, idl1 int, cc, c1, c2, ch, ch2, wa []float32) {
	const tpi = float32(6.283185307179586)
	var idij, ipph, i, j, k, l, ik, is, t0, t1, t2, t3, t4, t5, t6, t7, t8, t9, t10, t11, t12 int
	var dc2, ai1, ai2, ar1, ar2, ds2 float32
	var nbd int
	var dcp, arg, dsp, ar1h, ar2h float32
	var ipp2 int

	t10 = ip * ido
	t0 = l1 * ido
	arg = tpi / float32(ip)
	dcp = float32(math.Cos(float64(arg)))
	dsp = float32(math.Sin(float64(arg)))
	nbd = (ido - 1) >> 1
	ipp2 = ip
	ipph = (ip + 1) >> 1
	if ido < l1 {
		goto L103
	}

	t1 = 0
	t2 = 0
	for k = 0; k < l1; k++ {
		t3 = t1
		t4 = t2
		for i = 0; i < ido; i++ {
			ch[t3] = cc[t4]
			t3++
			t4++
		}
		t1 += ido
		t2 += t10
	}
	goto L106

L103:
	t1 = 0
	for i = 0; i < ido; i++ {
		t2 = t1
		t3 = t1
		for k = 0; k < l1; k++ {
			ch[t2] = cc[t3]
			t2 += ido
			t3 += t10
		}
		t1++
	}

L106:
	t1 = 0
	t2 = ipp2 * t0
	t5 = ido << 1
	t7 = t5
	for j = 1; j < ipph; j++ {
		t1 += t0
		t2 -= t0
		t3 = t1
		t4 = t2
		t6 = t5
		for k = 0; k < l1; k++ {
			ch[t3] = cc[t6-1] + cc[t6-1]
			ch[t4] = cc[t6] + cc[t6]
			t3 += ido
			t4 += ido
			t6 += t10
		}
		t5 += t7
	}

	if ido == 1 {
		goto L116
	}
	if nbd < l1 {
		goto L112
	}

	t1 = 0
	t2 = ipp2 * t0
	t7 = 0
	for j = 1; j < ipph; j++ {
		t1 += t0
		t2 -= t0
		t3 = t1
		t4 = t2

		t7 += ido << 1
		t8 = t7
		for k = 0; k < l1; k++ {
			t5 = t3
			t6 = t4
			t9 = t8
			t11 = t8
			for i = 2; i < ido; i += 2 {
				t5 += 2
				t6 += 2
				t9 += 2
				t11 -= 2
				ch[t5-1] = cc[t9-1] + cc[t11-1]
				ch[t6-1] = cc[t9-1] - cc[t11-1]
				ch[t5] = cc[t9] - cc[t11]
				ch[t6] = cc[t9] + cc[t11]
			}
			t3 += ido
			t4 += ido
			t8 += t10
		}
	}
	goto L116

L112:
	t1 = 0
	t2 = ipp2 * t0
	t7 = 0
	for j = 1; j < ipph; j++ {
		t1 += t0
		t2 -= t0
		t3 = t1
		t4 = t2
		t7 += ido << 1
		t8 = t7
		t9 = t7
		for i = 2; i < ido; i += 2 {
			t3 += 2
			t4 += 2
			t8 += 2
			t9 -= 2
			t5 = t3
			t6 = t4
			t11 = t8
			t12 = t9
			for k = 0; k < l1; k++ {
				ch[t5-1] = cc[t11-1] + cc[t12-1]
				ch[t6-1] = cc[t11-1] - cc[t12-1]
				ch[t5] = cc[t11] - cc[t12]
				ch[t6] = cc[t11] + cc[t12]
				t5 += ido
				t6 += ido
				t11 += t10
				t12 += t10
			}
		}
	}

L116:
	ar1 = 1
	ai1 = 0
	t1 = 0
	t2 = ipp2 * idl1
	t9 = t2
	t3 = (ip - 1) * idl1
	for l = 1; l < ipph; l++ {
		t1 += idl1
		t2 -= idl1

		ar1h = float32(dcp*ar1) - float32(dsp*ai1)
		ai1 = float32(dcp*ai1) + float32(dsp*ar1)
		ar1 = ar1h
		t4 = t1
		t5 = t2
		t6 = 0
		t7 = idl1
		t8 = t3
		for ik = 0; ik < idl1; ik++ {
			c2[t4] = ch2[t6] + float32(ar1*ch2[t7])
			t4++
			t6++
			t7++
			c2[t5] = ai1 * ch2[t8]
			t5++
			t8++
		}
		dc2 = ar1
		ds2 = ai1
		ar2 = ar1
		ai2 = ai1

		t6 = idl1
		t7 = t9 - idl1
		for j = 2; j < ipph; j++ {
			t6 += idl1
			t7 -= idl1
			ar2h = float32(dc2*ar2) - float32(ds2*ai2)
			ai2 = float32(dc2*ai2) + float32(ds2*ar2)
			ar2 = ar2h
			t4 = t1
			t5 = t2
			t11 = t6
			t12 = t7
			for ik = 0; ik < idl1; ik++ {
				c2[t4] += float32(ar2 * ch2[t11])
				t4++
				t11++
				c2[t5] += float32(ai2 * ch2[t12])
				t5++
				t12++
			}
		}
	}

	t1 = 0
	for j = 1; j < ipph; j++ {
		t1 += idl1
		t2 = t1
		for ik = 0; ik < idl1; ik++ {
			ch2[ik] += ch2[t2]
			t2++
		}
	}

	t1 = 0
	t2 = ipp2 * t0
	for j = 1; j < ipph; j++ {
		t1 += t0
		t2 -= t0
		t3 = t1
		t4 = t2
		for k = 0; k < l1; k++ {
			ch[t3] = c1[t3] - c1[t4]
			ch[t4] = c1[t3] + c1[t4]
			t3 += ido
			t4 += ido
		}
	}

	if ido == 1 {
		goto L132
	}
	if nbd < l1 {
		goto L128
	}

	t1 = 0
	t2 = ipp2 * t0
	for j = 1; j < ipph; j++ {
		t1 += t0
		t2 -= t0
		t3 = t1
		t4 = t2
		for k = 0; k < l1; k++ {
			t5 = t3
			t6 = t4
			for i = 2; i < ido; i += 2 {
				t5 += 2
				t6 += 2
				ch[t5-1] = c1[t5-1] - c1[t6]
				ch[t6-1] = c1[t5-1] + c1[t6]
				ch[t5] = c1[t5] + c1[t6-1]
				ch[t6] = c1[t5] - c1[t6-1]
			}
			t3 += ido
			t4 += ido
		}
	}
	goto L132

L128:
	t1 = 0
	t2 = ipp2 * t0
	for j = 1; j < ipph; j++ {
		t1 += t0
		t2 -= t0
		t3 = t1
		t4 = t2
		for i = 2; i < ido; i += 2 {
			t3 += 2
			t4 += 2
			t5 = t3
			t6 = t4
			for k = 0; k < l1; k++ {
				ch[t5-1] = c1[t5-1] - c1[t6]
				ch[t6-1] = c1[t5-1] + c1[t6]
				ch[t5] = c1[t5] + c1[t6-1]
				ch[t6] = c1[t5] - c1[t6-1]
				t5 += ido
				t6 += ido
			}
		}
	}

L132:
	if ido == 1 {
		return
	}

	for ik = 0; ik < idl1; ik++ {
		c2[ik] = ch2[ik]
	}

	t1 = 0
	for j = 1; j < ip; j++ {
		t1 += t0
		t2 = t1
		for k = 0; k < l1; k++ {
			c1[t2] = ch[t2]
			t2 += ido
		}
	}

	if nbd > l1 {
		goto L139
	}

	is = -ido - 1
	t1 = 0
	for j = 1; j < ip; j++ {
		is += ido
		t1 += t0
		idij = is
		t2 = t1
		for i = 2; i < ido; i += 2 {
			t2 += 2
			idij += 2
			t3 = t2
			for k = 0; k < l1; k++ {
				c1[t3-1] = float32(wa[idij-1]*ch[t3-1]) - float32(wa[idij]*ch[t3])
				c1[t3] = float32(wa[idij-1]*ch[t3]) + float32(wa[idij]*ch[t3-1])
				t3 += ido
			}
		}
	}
	return

L139:
	is = -ido - 1
	t1 = 0
	for j = 1; j < ip; j++ {
		is += ido
		t1 += t0
		t2 = t1
		for k = 0; k < l1; k++ {
			idij = is
			t3 = t2
			for i = 2; i < ido; i += 2 {
				idij += 2
				t3 += 2
				c1[t3-1] = float32(wa[idij-1]*ch[t3-1]) - float32(wa[idij]*ch[t3])
				c1[t3] = float32(wa[idij-1]*ch[t3]) + float32(wa[idij]*ch[t3-1])
			}
			t2 += ido
		}
	}
}

// drftb1 runs the backward passes for each factor (smallft.c drftb1).
func drftb1(n int, c, ch, wa []float32, ifac []int) {
	var i, k1, l1, l2, na, nf, ip, iw, ix2, ix3, ido, idl1 int

	nf = ifac[1]
	na = 0
	l1 = 1
	iw = 1

	for k1 = 0; k1 < nf; k1++ {
		ip = ifac[k1+2]
		l2 = ip * l1
		ido = n / l2
		idl1 = ido * l1
		if ip != 4 {
			goto L103
		}
		ix2 = iw + ido
		ix3 = ix2 + ido

		if na != 0 {
			dradb4(ido, l1, ch, c, wa[iw-1:], wa[ix2-1:], wa[ix3-1:])
		} else {
			dradb4(ido, l1, c, ch, wa[iw-1:], wa[ix2-1:], wa[ix3-1:])
		}
		na = 1 - na
		goto L115

	L103:
		if ip != 2 {
			goto L106
		}

		if na != 0 {
			dradb2(ido, l1, ch, c, wa[iw-1:])
		} else {
			dradb2(ido, l1, c, ch, wa[iw-1:])
		}
		na = 1 - na
		goto L115

	L106:
		if ip != 3 {
			goto L109
		}

		ix2 = iw + ido
		if na != 0 {
			dradb3(ido, l1, ch, c, wa[iw-1:], wa[ix2-1:])
		} else {
			dradb3(ido, l1, c, ch, wa[iw-1:], wa[ix2-1:])
		}
		na = 1 - na
		goto L115

	L109:
		// Upstream has no radix-5 backward pass; 5 goes through dradbg.
		if na != 0 {
			dradbg(ido, ip, l1, idl1, ch, ch, ch, c, c, wa[iw-1:])
		} else {
			dradbg(ido, ip, l1, idl1, c, c, c, ch, ch, wa[iw-1:])
		}
		if ido == 1 {
			na = 1 - na
		}

	L115:
		l1 = l2
		iw += (ip - 1) * ido
	}

	if na == 0 {
		return
	}

	for i = 0; i < n; i++ {
		c[i] = ch[i]
	}
}

// drft is smallft.c's struct drft_lookup.
type drft struct {
	n          int
	trigcache  []float32 // n floats of scratch, then 2n twiddles
	splitcache []int     // the factorization
}

// spx_drft_init
func newDrft(n int) *drft {
	l := &drft{
		n:          n,
		trigcache:  make([]float32, 3*n),
		splitcache: make([]int, 32),
	}
	fdrffti(n, l.trigcache, l.splitcache)
	return l
}

// spx_drft_forward
func (l *drft) forward(data []float32) {
	if l.n == 1 {
		return
	}
	drftf1(l.n, data, l.trigcache, l.trigcache[l.n:], l.splitcache)
}

// spx_drft_backward
func (l *drft) backward(data []float32) {
	if l.n == 1 {
		return
	}
	drftb1(l.n, data, l.trigcache, l.trigcache[l.n:], l.splitcache)
}
