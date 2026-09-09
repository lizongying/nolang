#!/usr/bin/env python3
# Differential test for nolang x25519 fe-* primitives.
# Faithfully port nolang fe-* + montgomery ladder (in-place mutation like nolang),
# then swap each primitive one-at-a-time with a correct ref10 implementation.
# Whichever swap fixes the final result is the collapsing operation.

P = (1 << 255) - 19
SHIFT = [0, 26, 51, 77, 102, 128, 153, 179, 204, 230]

def i64(x):
    # nolang i64: keep as unsigned 64-bit so that >> is LOGICAL (zero-fill),
    # matching nolang's semantics. Arithmetic is done mod 2^64.
    return x & ((1 << 64) - 1)

def limb_val(limbs):
    # interpret each limb as SIGNED i64 (nolang keeps limbs signed via wrap)
    s = 0
    for i in range(10):
        v = limbs[i]
        if v >= (1 << 63):
            v -= (1 << 64)
        s += v * (1 << SHIFT[i])
    return s % P

CARRY_SPEC = [
    ((1 << 25), 26), ((1 << 24), 25), ((1 << 25), 26), ((1 << 24), 25),
    ((1 << 25), 26), ((1 << 24), 25), ((1 << 25), 26), ((1 << 24), 25),
    ((1 << 25), 26), ((1 << 24), 25),
]

def carry_nolang(t):
    t = [i64(x) for x in t]
    # limbs 0..8 propagate carry into next limb (i+1)
    for i in range(9):
        rnd, sh = CARRY_SPEC[i]
        s = i64(t[i] + rnd)
        c = i64((s >> sh) - ((s >> 63) << (64 - sh)))
        t[i] = i64(t[i] - (c << sh))
        t[i + 1] = i64(t[i + 1] + c)
    # limb 9: reduce only, defer wrap to limb0 * 19 (NOT propagate c into t[0])
    rnd, sh = CARRY_SPEC[9]
    s = i64(t[9] + rnd)
    c = i64((s >> sh) - ((s >> 63) << (64 - sh)))
    t[9] = i64(t[9] - (c << sh))
    # wrap (limb9 -> limb0 * 19)
    t[0] = i64(t[0] + c * 19)
    # second pass
    s = i64(t[0] + (1 << 25)); c = i64((s >> 26) - ((s >> 63) << 38))
    t[0] = i64(t[0] - (c << 26)); t[1] = i64(t[1] + c)
    s = i64(t[1] + (1 << 24)); c = i64((s >> 25) - ((s >> 63) << 39))
    t[1] = i64(t[1] - (c << 25)); t[2] = i64(t[2] + c)
    return t

# ---------- nolang ports (faithful; in-place mutation) ----------
def fe_add_nolang(h, f, g):
    h[:] = [i64(f[i] + g[i]) for i in range(10)]
    return h

def fe_sub_nolang(h, f, g):
    h[:] = [i64(f[i] - g[i]) for i in range(10)]
    return h

def fe_mul_nolang(h, f, g):
    f0, f1, f2, f3, f4, f5, f6, f7, f8, f9 = f
    g0, g1, g2, g3, g4, g5, g6, g7, g8, g9 = g
    g1_19, g2_19, g3_19, g4_19, g5_19, g6_19, g7_19, g8_19, g9_19 = [19*x for x in g[1:]]
    f1_2, f3_2, f5_2, f7_2, f9_2 = [2*x for x in (f1, f3, f5, f7, f9)]
    t = [0]*10
    t[0] = f0*g0 + f1_2*g9_19 + f2*g8_19 + f3_2*g7_19 + f4*g6_19 + f5_2*g5_19 + f6*g4_19 + f7_2*g3_19 + f8*g2_19 + f9_2*g1_19
    t[1] = f0*g1 + f1*g0 + f2*g9_19 + f3*g8_19 + f4*g7_19 + f5*g6_19 + f6*g5_19 + f7*g4_19 + f8*g3_19 + f9*g2_19
    t[2] = f0*g2 + f1_2*g1 + f2*g0 + f3_2*g9_19 + f4*g8_19 + f5_2*g7_19 + f6*g6_19 + f7_2*g5_19 + f8*g4_19 + f9_2*g3_19
    t[3] = f0*g3 + f1*g2 + f2*g1 + f3*g0 + f4*g9_19 + f5*g8_19 + f6*g7_19 + f7*g6_19 + f8*g5_19 + f9*g4_19
    t[4] = f0*g4 + f1_2*g3 + f2*g2 + f3_2*g1 + f4*g0 + f5_2*g9_19 + f6*g8_19 + f7_2*g7_19 + f8*g6_19 + f9_2*g5_19
    t[5] = f0*g5 + f1*g4 + f2*g3 + f3*g2 + f4*g1 + f5*g0 + f6*g9_19 + f7*g8_19 + f8*g7_19 + f9*g6_19
    t[6] = f0*g6 + f1_2*g5 + f2*g4 + f3_2*g3 + f4*g2 + f5_2*g1 + f6*g0 + f7_2*g9_19 + f8*g8_19 + f9_2*g7_19
    t[7] = f0*g7 + f1*g6 + f2*g5 + f3*g4 + f4*g3 + f5*g2 + f6*g1 + f7*g0 + f8*g9_19 + f9*g8_19
    t[8] = f0*g8 + f1_2*g7 + f2*g6 + f3_2*g5 + f4*g4 + f5_2*g3 + f6*g2 + f7_2*g1 + f8*g0 + f9_2*g9_19
    t[9] = f0*g9 + f1*g8 + f2*g7 + f3*g6 + f4*g5 + f5*g4 + f6*g3 + f7*g2 + f8*g1 + f9*g0
    h[:] = carry_nolang(t)
    return h

def fe_sq_nolang(h, f):
    return fe_mul_nolang(h, f, f)

def fe_mul121665_nolang(h, f):
    t = [i64(121665 * f[i]) for i in range(10)]
    h[:] = carry_nolang(t)
    return h

def fe_frombytes_nolang(s):
    h = [0]*10
    h[0] = (s[0]) | (s[1] << 8) | (s[2] << 16) | (s[3] << 24)
    h[1] = ((s[4]) | (s[5] << 8) | (s[6] << 16)) << 6
    h[2] = ((s[7]) | (s[8] << 8) | (s[9] << 16)) << 5
    h[3] = ((s[10]) | (s[11] << 8) | (s[12] << 16)) << 3
    h[4] = ((s[13]) | (s[14] << 8) | (s[15] << 16)) << 2
    h[5] = (s[16]) | (s[17] << 8) | (s[18] << 16) | (s[19] << 24)
    h[6] = ((s[20]) | (s[21] << 8) | (s[22] << 16)) << 7
    h[7] = ((s[23]) | (s[24] << 8) | (s[25] << 16)) << 5
    h[8] = ((s[26]) | (s[27] << 8) | (s[28] << 16)) << 4
    h[9] = (((s[29]) | (s[30] << 8) | (s[31] << 16)) & 8388607) << 2
    return carry_nolang(h)

def fe_tobytes_nolang(h):
    t = [i64(x) for x in h]
    for i in range(9):
        sh = 26 if i % 2 == 0 else 25
        c = i64((t[i] >> sh) - ((t[i] >> 63) << (64 - sh)))
        t[i] = i64(t[i] - (c << sh)); t[i+1] = i64(t[i+1] + c)
    c = i64((t[9] >> 25) - ((t[9] >> 63) << 39)); t[9] = i64(t[9] - (c << 25)); t[0] = i64(t[0] + c*19)
    c = i64((t[0] >> 26) - ((t[0] >> 63) << 38)); t[0] = i64(t[0] - (c << 26)); t[1] = i64(t[1] + c)
    c = i64((t[1] >> 25) - ((t[1] >> 63) << 39)); t[1] = i64(t[1] - (c << 25)); t[2] = i64(t[2] + c)
    t[0] = i64(t[0] + 19)
    for i in range(9):
        sh = 26 if i % 2 == 0 else 25
        c = i64((t[i] >> sh) - ((t[i] >> 63) << (64 - sh)))
        t[i] = i64(t[i] - (c << sh)); t[i+1] = i64(t[i+1] + c)
    c = i64((t[9] >> 25) - ((t[9] >> 63) << 39)); t[9] = i64(t[9] - (c << 25)); t[0] = i64(t[0] + c*19)
    c = i64((t[0] >> 26) - ((t[0] >> 63) << 38)); t[0] = i64(t[0] - (c << 26)); t[1] = i64(t[1] + c)
    c = i64((t[1] >> 25) - ((t[1] >> 63) << 39)); t[1] = i64(t[1] - (c << 25)); t[2] = i64(t[2] + c)
    mask = i64(0 - (1 - c))
    t[0] = i64(t[0] - (19 & mask))
    out = [0]*32
    out[0] = t[0] & 255; out[1] = (t[0] >> 8) & 255; out[2] = (t[0] >> 16) & 255
    out[3] = ((t[0] >> 24) | (t[1] << 2)) & 255; out[4] = (t[1] >> 6) & 255; out[5] = (t[1] >> 14) & 255
    out[6] = ((t[1] >> 22) | (t[2] << 3)) & 255; out[7] = (t[2] >> 5) & 255; out[8] = (t[2] >> 13) & 255
    out[9] = ((t[2] >> 21) | (t[3] << 5)) & 255; out[10] = (t[3] >> 3) & 255; out[11] = (t[3] >> 11) & 255
    out[12] = ((t[3] >> 19) | (t[4] << 6)) & 255; out[13] = (t[4] >> 2) & 255; out[14] = (t[4] >> 10) & 255
    out[15] = (t[4] >> 18) & 255; out[16] = ((t[4] >> 26) | (t[5] << 0)) & 255; out[17] = (t[5] >> 8) & 255
    out[18] = (t[5] >> 16) & 255; out[19] = ((t[5] >> 24) | (t[6] << 1)) & 255; out[20] = (t[6] >> 7) & 255
    out[21] = (t[6] >> 15) & 255; out[22] = ((t[6] >> 23) | (t[7] << 3)) & 255; out[23] = (t[7] >> 5) & 255
    out[24] = (t[7] >> 13) & 255; out[25] = ((t[7] >> 21) | (t[8] << 4)) & 255; out[26] = (t[8] >> 4) & 255
    out[27] = (t[8] >> 12) & 255; out[28] = ((t[8] >> 20) | (t[9] << 6)) & 255; out[29] = (t[9] >> 2) & 255
    out[30] = (t[9] >> 10) & 255; out[31] = (t[9] >> 18) & 255
    return out

# ---------- correct ref10 implementations (in-place) ----------
def fe_add_c(h, f, g):
    h[:] = [f[i] + g[i] for i in range(10)]
    return h

def fe_sub_c(h, f, g):
    C = 66664320
    t = [f[i] - g[i] + C for i in range(10)]
    c0 = t[0] >> 26; t[1] += c0; t[0] -= c0 << 26
    c1 = t[1] >> 25; t[2] += c1; t[1] -= c1 << 25
    c2 = t[2] >> 26; t[3] += c2; t[2] -= c2 << 26
    c3 = t[3] >> 25; t[4] += c3; t[3] -= c3 << 25
    c4 = t[4] >> 26; t[5] += c4; t[4] -= c4 << 26
    c5 = t[5] >> 25; t[6] += c5; t[5] -= c5 << 25
    c6 = t[6] >> 26; t[7] += c6; t[6] -= c6 << 26
    c7 = t[7] >> 25; t[8] += c7; t[7] -= c7 << 25
    c8 = t[8] >> 26; t[9] += c8; t[8] -= c8 << 26
    c9 = t[9] >> 25; t[0] += c9 * 19; t[9] -= c9 << 25
    c10 = t[0] >> 26; t[1] += c10; t[0] -= c10 << 26
    c11 = t[1] >> 25; t[2] += c11; t[1] -= c11 << 25
    h[:] = [int(x) for x in t]
    return h

def fe_mul_c(h, f, g):
    f0, f1, f2, f3, f4, f5, f6, f7, f8, f9 = f
    g0, g1, g2, g3, g4, g5, g6, g7, g8, g9 = g
    g1_19, g2_19, g3_19, g4_19, g5_19, g6_19, g7_19, g8_19, g9_19 = [19*x for x in g[1:]]
    f1_2, f3_2, f5_2, f7_2, f9_2 = [2*x for x in (f1, f3, f5, f7, f9)]
    t = [0]*10
    t[0] = f0*g0 + f1_2*g9_19 + f2*g8_19 + f3_2*g7_19 + f4*g6_19 + f5_2*g5_19 + f6*g4_19 + f7_2*g3_19 + f8*g2_19 + f9_2*g1_19
    t[1] = f0*g1 + f1*g0 + f2*g9_19 + f3*g8_19 + f4*g7_19 + f5*g6_19 + f6*g5_19 + f7*g4_19 + f8*g3_19 + f9*g2_19
    t[2] = f0*g2 + f1_2*g1 + f2*g0 + f3_2*g9_19 + f4*g8_19 + f5_2*g7_19 + f6*g6_19 + f7_2*g5_19 + f8*g4_19 + f9_2*g3_19
    t[3] = f0*g3 + f1*g2 + f2*g1 + f3*g0 + f4*g9_19 + f5*g8_19 + f6*g7_19 + f7*g6_19 + f8*g5_19 + f9*g4_19
    t[4] = f0*g4 + f1_2*g3 + f2*g2 + f3_2*g1 + f4*g0 + f5_2*g9_19 + f6*g8_19 + f7_2*g7_19 + f8*g6_19 + f9_2*g5_19
    t[5] = f0*g5 + f1*g4 + f2*g3 + f3*g2 + f4*g1 + f5*g0 + f6*g9_19 + f7*g8_19 + f8*g7_19 + f9*g6_19
    t[6] = f0*g6 + f1_2*g5 + f2*g4 + f3_2*g3 + f4*g2 + f5_2*g1 + f6*g0 + f7_2*g9_19 + f8*g8_19 + f9_2*g7_19
    t[7] = f0*g7 + f1*g6 + f2*g5 + f3*g4 + f4*g3 + f5*g2 + f6*g1 + f7*g0 + f8*g9_19 + f9*g8_19
    t[8] = f0*g8 + f1_2*g7 + f2*g6 + f3_2*g5 + f4*g4 + f5_2*g3 + f6*g2 + f7_2*g1 + f8*g0 + f9_2*g9_19
    t[9] = f0*g9 + f1*g8 + f2*g7 + f3*g6 + f4*g5 + f5*g4 + f6*g3 + f7*g2 + f8*g1 + f9*g0
    h0 = t[0] + (1 << 25); c0 = h0 >> 26; t[1] += c0; t[0] = h0 - c0*(1<<26)
    h1 = t[1] + (1 << 24); c1 = h1 >> 25; t[2] += c1; t[1] = h1 - c1*(1<<25)
    h2 = t[2] + (1 << 25); c2 = h2 >> 26; t[3] += c2; t[2] = h2 - c2*(1<<26)
    h3 = t[3] + (1 << 24); c3 = h3 >> 25; t[4] += c3; t[3] = h3 - c3*(1<<25)
    h4 = t[4] + (1 << 25); c4 = h4 >> 26; t[5] += c4; t[4] = h4 - c4*(1<<26)
    h5 = t[5] + (1 << 24); c5 = h5 >> 25; t[6] += c5; t[5] = h5 - c5*(1<<25)
    h6 = t[6] + (1 << 25); c6 = h6 >> 26; t[7] += c6; t[6] = h6 - c6*(1<<26)
    h7 = t[7] + (1 << 24); c7 = h7 >> 25; t[8] += c7; t[7] = h7 - c7*(1<<25)
    h8 = t[8] + (1 << 25); c8 = h8 >> 26; t[9] += c8; t[8] = h8 - c8*(1<<26)
    h9 = t[9] + (1 << 24); c9 = h9 >> 25; t[0] += c9*19; t[9] = h9 - c9*(1<<25)
    h0 = t[0] + (1 << 25); c10 = h0 >> 26; t[1] += c10; t[0] = h0 - c10*(1<<26)
    h1 = t[1] + (1 << 24); c11 = h1 >> 25; t[2] += c11; t[1] = h1 - c11*(1<<25)
    h[:] = [int(x) for x in t]
    return h

def fe_sq_c(h, f):
    return fe_mul_c(h, f, f)

def fe_mul121665_c(h, f):
    t = [121665 * f[i] for i in range(10)]
    h0 = t[0] + (1 << 25); c0 = h0 >> 26; t[1] += c0; t[0] = h0 - c0*(1<<26)
    h1 = t[1] + (1 << 24); c1 = h1 >> 25; t[2] += c1; t[1] = h1 - c1*(1<<25)
    h2 = t[2] + (1 << 25); c2 = h2 >> 26; t[3] += c2; t[2] = h2 - c2*(1<<26)
    h3 = t[3] + (1 << 24); c3 = h3 >> 25; t[4] += c3; t[3] = h3 - c3*(1<<25)
    h4 = t[4] + (1 << 25); c4 = h4 >> 26; t[5] += c4; t[4] = h4 - c4*(1<<26)
    h5 = t[5] + (1 << 24); c5 = h5 >> 25; t[6] += c5; t[5] = h5 - c5*(1<<25)
    h6 = t[6] + (1 << 25); c6 = h6 >> 26; t[7] += c6; t[6] = h6 - c6*(1<<26)
    h7 = t[7] + (1 << 24); c7 = h7 >> 25; t[8] += c7; t[7] = h7 - c7*(1<<25)
    h8 = t[8] + (1 << 25); c8 = h8 >> 26; t[9] += c8; t[8] = h8 - c8*(1<<26)
    h9 = t[9] + (1 << 24); c9 = h9 >> 25; t[0] += c9*19; t[9] = h9 - c9*(1<<25)
    h0 = t[0] + (1 << 25); c10 = h0 >> 26; t[1] += c10; t[0] = h0 - c10*(1<<26)
    h1 = t[1] + (1 << 24); c11 = h1 >> 25; t[2] += c11; t[1] = h1 - c11*(1<<25)
    h[:] = [int(x) for x in t]
    return h

def fe_frombytes_c(s):
    h = [0]*10
    h[0] = s[0] | (s[1] << 8) | (s[2] << 16) | (s[3] << 24)
    h[1] = ((s[4] | (s[5] << 8) | (s[6] << 16)) << 6)
    h[2] = ((s[7] | (s[8] << 8) | (s[9] << 16)) << 5)
    h[3] = ((s[10] | (s[11] << 8) | (s[12] << 16)) << 3)
    h[4] = ((s[13] | (s[14] << 8) | (s[15] << 16)) << 2)
    h[5] = s[16] | (s[17] << 8) | (s[18] << 16) | (s[19] << 24)
    h[6] = ((s[20] | (s[21] << 8) | (s[22] << 16)) << 7)
    h[7] = ((s[23] | (s[24] << 8) | (s[25] << 16)) << 5)
    h[8] = ((s[26] | (s[27] << 8) | (s[28] << 16)) << 4)
    h[9] = (((s[29] | (s[30] << 8) | (s[31] << 16)) & 8388607) << 2)
    h0 = h[0] + (1 << 25); c0 = h0 >> 26; h[1] += c0; h[0] = h0 - c0*(1<<26)
    h1 = h[1] + (1 << 24); c1 = h1 >> 25; h[2] += c1; h[1] = h1 - c1*(1<<25)
    h2 = h[2] + (1 << 25); c2 = h2 >> 26; h[3] += c2; h[2] = h2 - c2*(1<<26)
    h3 = h[3] + (1 << 24); c3 = h3 >> 25; h[4] += c3; h[3] = h3 - c3*(1<<25)
    h4 = h[4] + (1 << 25); c4 = h4 >> 26; h[5] += c4; h[4] = h4 - c4*(1<<26)
    h5 = h[5] + (1 << 24); c5 = h5 >> 25; h[6] += c5; h[5] = h5 - c5*(1<<25)
    h6 = h[6] + (1 << 25); c6 = h6 >> 26; h[7] += c6; h[6] = h6 - c6*(1<<26)
    h7 = h[7] + (1 << 24); c7 = h7 >> 25; h[8] += c7; h[7] = h7 - c7*(1<<25)
    h8 = h[8] + (1 << 25); c8 = h8 >> 26; h[9] += c8; h[8] = h8 - c8*(1<<26)
    h9 = h[9] + (1 << 24); c9 = h9 >> 25; h[0] += c9*19; h[9] = h9 - c9*(1<<25)
    h0 = h[0] + (1 << 25); c10 = h0 >> 26; h[1] += c10; h[0] = h0 - c10*(1<<26)
    h1 = h[1] + (1 << 24); c11 = h1 >> 25; h[2] += c11; h[1] = h1 - c11*(1<<25)
    return [int(x) for x in h]

def fe_tobytes_c(h):
    t = [int(x) for x in h]
    for _ in range(2):
        c0 = t[0] >> 26; t[1] += c0; t[0] -= c0 << 26
        c1 = t[1] >> 25; t[2] += c1; t[1] -= c1 << 25
        c2 = t[2] >> 26; t[3] += c2; t[2] -= c2 << 26
        c3 = t[3] >> 25; t[4] += c3; t[3] -= c3 << 25
        c4 = t[4] >> 26; t[5] += c4; t[4] -= c4 << 26
        c5 = t[5] >> 25; t[6] += c5; t[5] -= c5 << 25
        c6 = t[6] >> 26; t[7] += c6; t[6] -= c6 << 26
        c7 = t[7] >> 25; t[8] += c7; t[7] -= c7 << 25
        c8 = t[8] >> 26; t[9] += c8; t[8] -= c8 << 26
        c9 = t[9] >> 25; t[0] += c9*19; t[9] -= c9 << 25
    val = limb_val(t)
    out = [0]*32
    for i in range(32):
        out[i] = (val >> (8*i)) & 255
    return out

# ---------- montgomery ladder (nolang structure, in-place) ----------
def _invert_with(out, a, ops):
    m = ops['fe_mul']; sq = ops['fe_sq']
    t0 = [0]*10; t1 = [0]*10; t2 = [0]*10; t3 = [0]*10
    sq(t0, a)
    sq(t1, t0)
    sq(t1, t1)
    m(t1, a, t1)
    m(t0, t0, t1)
    sq(t2, t0)
    m(t1, t1, t2)
    sq(t2, t1)
    for _ in range(4): sq(t2, t2)
    m(t1, t2, t1)
    sq(t2, t1)
    for _ in range(9): sq(t2, t2)
    m(t2, t2, t1)
    sq(t3, t2)
    for _ in range(19): sq(t3, t3)
    m(t2, t3, t2)
    for _ in range(10): sq(t2, t2)
    m(t1, t2, t1)
    sq(t2, t1)
    for _ in range(49): sq(t2, t2)
    m(t2, t2, t1)
    sq(t3, t2)
    for _ in range(99): sq(t3, t3)
    m(t2, t3, t2)
    for _ in range(50): sq(t2, t2)
    m(t1, t2, t1)
    for _ in range(5): sq(t1, t1)
    m(out, t1, t0)
    return out

def ladder(scalar_bytes, point_bytes, ops):
    add = ops['fe_add']; sub = ops['fe_sub']; mul = ops['fe_mul']
    sq = ops['fe_sq']; m121665 = ops['fe_mul121665']; inv = ops['fe_invert']
    fb = ops['fe_frombytes']; tb = ops['fe_tobytes']
    k = list(scalar_bytes)
    k[0] &= 248; k[31] &= 127; k[31] |= 64
    u = fb(point_bytes)
    x1 = list(u)
    x2 = [0]*10; x2[0] = 1
    z2 = [0]*10
    x3 = list(u)
    z3 = [0]*10; z3[0] = 1
    a = [0]*10; aa = [0]*10; b = [0]*10; bb = [0]*10
    e = [0]*10; c = [0]*10; d = [0]*10; da = [0]*10; cb = [0]*10
    swap = 0
    t = 254
    while t >= 0:
        byte_idx = t // 8; bit_idx = t % 8
        bv = (k[byte_idx] >> bit_idx) & 1
        swap ^= bv
        if swap:
            x2, x3 = x3, x2; z2, z3 = z3, z2
        swap = bv
        add(a, x2, z2)
        sq(aa, a)
        sub(b, x2, z2)
        sq(bb, b)
        sub(e, aa, bb)
        add(c, x3, z3)
        sub(d, x3, z3)
        mul(da, d, a)
        mul(cb, c, b)
        add(x3, da, cb)
        sq(x3, x3)
        sub(z3, da, cb)
        sq(z3, z3)
        mul(z3, z3, x1)
        mul(x2, aa, bb)
        sq(a, e)
        m121665(a, a)
        mul(z2, e, aa)
        add(z2, z2, a)
        t -= 1
    if swap:
        x2, x3 = x3, x2; z2, z3 = z3, z2
    z_inv = [0]*10
    inv(z_inv, z2)
    result = [0]*10
    mul(result, x2, z_inv)
    return tb(result)

# ---------- ground truth ----------
ALICE_PRIV = bytes.fromhex('77076d0a7318a57d3c16c17251b26645df4c2f87ebc0992ab177fba51db92c2a')
ALICE_PUB  = bytes.fromhex('8520f0098930a754748b7ddcb43ef75a0dbf3a0d26381af4eba4a98eaa9b4e6a')
B = [9] + [0]*31
ALICE_PUB_INT = int.from_bytes(ALICE_PUB, 'little') & ((1 << 255) - 1)

OPS = {
    'fe_add': fe_add_nolang, 'fe_sub': fe_sub_nolang, 'fe_mul': fe_mul_nolang,
    'fe_sq': fe_sq_nolang, 'fe_mul121665': fe_mul121665_nolang,
    'fe_invert': lambda o, a: _invert_with(o, a, OPS),
    'fe_frombytes': fe_frombytes_nolang, 'fe_tobytes': fe_tobytes_nolang,
}

# ---------- MATHEMATICALLY CORRECT reference (big-int ground truth) ----------
MASK26 = (1 << 26) - 1
MASK25 = (1 << 25) - 1
def to_limbs(v):
    v %= P
    t = [0]*10
    t[0] = v & MASK26
    t[1] = (v >> 26) & MASK25
    t[2] = (v >> 51) & MASK26
    t[3] = (v >> 77) & MASK25
    t[4] = (v >> 102) & MASK26
    t[5] = (v >> 128) & MASK25
    t[6] = (v >> 153) & MASK26
    t[7] = (v >> 179) & MASK25
    t[8] = (v >> 204) & MASK26
    t[9] = (v >> 230) & MASK25
    # carry reduce
    for _ in range(3):
        c0 = t[0] >> 26; t[1] += c0; t[0] -= c0 << 26
        c1 = t[1] >> 25; t[2] += c1; t[1] -= c1 << 25
        c2 = t[2] >> 26; t[3] += c2; t[2] -= c2 << 26
        c3 = t[3] >> 25; t[4] += c3; t[3] -= c3 << 25
        c4 = t[4] >> 26; t[5] += c4; t[4] -= c4 << 26
        c5 = t[5] >> 25; t[6] += c5; t[5] -= c5 << 25
        c6 = t[6] >> 26; t[7] += c6; t[6] -= c6 << 26
        c7 = t[7] >> 25; t[8] += c7; t[7] -= c7 << 25
        c8 = t[8] >> 26; t[9] += c8; t[8] -= c8 << 26
        c9 = t[9] >> 25; t[0] += c9 * 19; t[9] -= c9 << 25
    return [int(x) for x in t]

def fe_add_c(h, f, g):
    h[:] = to_limbs((limb_val(f) + limb_val(g)) % P); return h
def fe_sub_c(h, f, g):
    h[:] = to_limbs((limb_val(f) - limb_val(g)) % P); return h
def fe_mul_c(h, f, g):
    h[:] = to_limbs((limb_val(f) * limb_val(g)) % P); return h
def fe_sq_c(h, f):
    h[:] = to_limbs((limb_val(f) * limb_val(f)) % P); return h
def fe_mul121665_c(h, f):
    h[:] = to_limbs((limb_val(f) * 121665) % P); return h
def fe_invert_c(h, a):
    h[:] = to_limbs(pow(limb_val(a), P - 2, P)); return h
def fe_frombytes_c(s):
    v = int.from_bytes(bytes(s), 'little') & ((1 << 255) - 1)
    return to_limbs(v % P)
def fe_tobytes_c(h):
    v = limb_val(h) % P
    return [(v >> (8*i)) & 255 for i in range(32)]

CORRECT = {
    'fe_add': fe_add_c, 'fe_sub': fe_sub_c, 'fe_mul': fe_mul_c,
    'fe_sq': fe_sq_c, 'fe_mul121665': fe_mul121665_c,
    'fe_invert': fe_invert_c, 'fe_frombytes': fe_frombytes_c, 'fe_tobytes': fe_tobytes_c,
}

def check(ops):
    out = ladder(ALICE_PRIV, B, ops)
    out_int = int.from_bytes(bytes(out), 'little') & ((1 << 255) - 1)
    return out_int == ALICE_PUB_INT, out_int, out

print("=== Base: all nolang ports ===")
ok, val, out = check(OPS)
print("OK=%s  val==expected:%s  val=%d" % (ok, val == ALICE_PUB_INT, val))
print("expected=", ALICE_PUB_INT)

print("\n=== Swap each primitive to mathematically-correct one at a time ===")
for name in ['fe_sub','fe_mul','fe_sq','fe_mul121665','fe_add','fe_invert','fe_frombytes','fe_tobytes']:
    ops = dict(OPS)
    ops[name] = CORRECT[name]
    ok, val, out = check(ops)
    print("swap %-14s -> OK=%s  val==expected:%s" % (name, ok, val == ALICE_PUB_INT))

print("\n=== All correct (big-int ground truth) ===")
ops = dict(OPS)
for n in CORRECT: ops[n] = CORRECT[n]
ok, val, out = check(ops)
print("OK=%s  val==expected:%s" % (ok, val == ALICE_PUB_INT))
print("out bytes hex:", bytes(out).hex())

# Sanity: isolated fe_invert * fe_mul == 1 (big-int correct)
a = [9] + [0]*9
inv = [0]*10
fe_invert_c(inv, a)
prod = [0]*10
fe_mul_c(prod, a, inv)
print("\nisolated invert(9)*9 == 1 (correct):", limb_val(prod) == 1)

# ---------- ISOLATED correctness of each nolang port ----------
import random
random.seed(1)
def rand_limbs():
    return [random.randint(0, (1<<26)-1) if i%2==0 else random.randint(0,(1<<25)-1) for i in range(10)]

print("\n=== Isolated: does each nolang port match big-int correct? ===")
# fe_add
bad=0
for _ in range(200):
    f=rand_limbs(); g=rand_limbs()
    r=[0]*10; fe_add_nolang(r,f,g)
    if limb_val(r)!=(limb_val(f)+limb_val(g))%P: bad+=1
print("fe_add_nolang mismatch:", bad)
bad=0
for _ in range(200):
    f=rand_limbs(); g=rand_limbs()
    r=[0]*10; fe_sub_nolang(r,f,g)
    if limb_val(r)!=(limb_val(f)-limb_val(g))%P: bad+=1
print("fe_sub_nolang mismatch:", bad)
bad=0
for _ in range(200):
    f=rand_limbs(); g=rand_limbs()
    r=[0]*10; fe_mul_nolang(r,f,g)
    if limb_val(r)!=(limb_val(f)*limb_val(g))%P: bad+=1
print("fe_mul_nolang mismatch:", bad)
bad=0
for _ in range(200):
    f=rand_limbs()
    r=[0]*10; fe_sq_nolang(r,f)
    if limb_val(r)!=(limb_val(f)*limb_val(f))%P: bad+=1
print("fe_sq_nolang mismatch:", bad)
bad=0
for _ in range(200):
    f=rand_limbs()
    r=[0]*10; fe_mul121665_nolang(r,f)
    if limb_val(r)!=(limb_val(f)*121665)%P: bad+=1
print("fe_mul121665_nolang mismatch:", bad)
bad=0
for _ in range(200):
    # bytes representing a value
    v=random.randrange(P)
    b=[(v>>(8*i))&255 for i in range(32)]
    r=fe_frombytes_nolang(b)
    if limb_val(r)!=v%P: bad+=1
print("fe_frombytes_nolang mismatch:", bad)
bad=0
for _ in range(200):
    f=rand_limbs()
    # tobytes should encode limb_val(f)%P in canonical form
    b=fe_tobytes_nolang(f)
    if int.from_bytes(bytes(b),'little')&((1<<255)-1)!=limb_val(f)%P: bad+=1
print("fe_tobytes_nolang mismatch:", bad)
# invert: a * invert(a) == 1
bad=0
for _ in range(50):
    f=rand_limbs()
    if limb_val(f)%P==0: continue
    inv=[0]*10; OPS['fe_invert'](inv,f)
    prod=[0]*10; fe_mul_nolang(prod, f, inv)
    if limb_val(prod)!=1: bad+=1
print("fe_invert_nolang (a*inv!=1):", bad)

