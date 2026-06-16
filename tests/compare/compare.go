// compare — Nolang 標準庫 vs Go 標準庫對照測試
//
// 輸出 KEY=VALUE 格式，與 tests/test_std_hash.no 的輸出一致。
// 用法：go run tests/compare/compare.go > tests/compare/go-output.txt

package main

import (
	"crypto/aes"
	"crypto/des"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"fmt"
	"hash/crc32"
	"hash/fnv"
)

func main() {
	// ===== 雜湊函數 =====
	testCRC32("",          `crc32("")`)
	testCRC32("hello",     `crc32("hello")`)
	testCRC32("The quick brown fox jumps over the lazy dog", `crc32("fox")`)

	testFNVa("",           `fnv1a32("")`)
	testFNVa("hello",      `fnv1a32("hello")`)
	testFNVa("The quick brown fox jumps over the lazy dog", `fnv1a32("fox")`)

	testMD5("",            `md5("")`)
	testMD5("hello",       `md5("hello")`)
	testMD5("The quick brown fox jumps over the lazy dog", `md5("fox")`)

	testSHA1("",           `sha1("")`)
	testSHA1("hello",      `sha1("hello")`)
	testSHA1("The quick brown fox jumps over the lazy dog", `sha1("fox")`)

	testSHA256("",         `sha256("")`)
	testSHA256("hello",    `sha256("hello")`)
	testSHA256("The quick brown fox jumps over the lazy dog", `sha256("fox")`)

	testSHA512("",         `sha512("")`)
	testSHA512("hello",    `sha512("hello")`)
	testSHA512("The quick brown fox jumps over the lazy dog", `sha512("fox")`)

	// ===== SHA-256 壓縮函數（zero block）=====
	iv256 := []uint32{
		0x6A09E667, 0xBB67AE85, 0x3C6EF372, 0xA54FF53A,
		0x510E527F, 0x9B05688C, 0x1F83D9AB, 0x5BE0CD19,
	}
	w256 := make([]uint32, 16)
	h256 := sha256Compress(iv256, w256)
	fmt.Printf("sha256-zero-block=%08x%08x%08x%08x%08x%08x%08x%08x\n",
		h256[0], h256[1], h256[2], h256[3], h256[4], h256[5], h256[6], h256[7])

	// ===== SHA-256 壓縮函數（空字串填充區塊）=====
	w256empty := []uint32{0x80000000, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	h256empty := sha256Compress(iv256, w256empty)
	fmt.Printf("sha256-empty=%08x%08x%08x%08x%08x%08x%08x%08x\n",
		h256empty[0], h256empty[1], h256empty[2], h256empty[3],
		h256empty[4], h256empty[5], h256empty[6], h256empty[7])

	// ===== SHA-256 壓縮函數（"abc" 填充區塊）=====
	w256abc := []uint32{0x61626380, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x18}
	h256abc := sha256Compress(iv256, w256abc)
	fmt.Printf("sha256-abc=%08x%08x%08x%08x%08x%08x%08x%08x\n",
		h256abc[0], h256abc[1], h256abc[2], h256abc[3],
		h256abc[4], h256abc[5], h256abc[6], h256abc[7])

	// ===== SHA-512 壓縮函數（zero block）=====
	iv512 := []uint64{
		0x6A09E667F3BCC908, 0xBB67AE8584CAA73B, 0x3C6EF372FE94F82B, 0xA54FF53A5F1D36F1,
		0x510E527FADE682D1, 0x9B05688C2B3E6C1F, 0x1F83D9ABFB41BD6B, 0x5BE0CD19137E2179,
	}
	w512 := make([]uint64, 16)
	h512 := sha512Compress(iv512, w512)
	fmt.Printf("sha512-zero-block=%016x%016x%016x%016x%016x%016x%016x%016x\n",
		h512[0], h512[1], h512[2], h512[3], h512[4], h512[5], h512[6], h512[7])

	// ===== SHA-512 壓縮函數（空字串填充區塊）=====
	w512empty := []uint64{0x8000000000000000, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	h512empty := sha512Compress(iv512, w512empty)
	fmt.Printf("sha512-empty=%016x%016x%016x%016x%016x%016x%016x%016x\n",
		h512empty[0], h512empty[1], h512empty[2], h512empty[3],
		h512empty[4], h512empty[5], h512empty[6], h512empty[7])

	// ===== SHA-512 壓縮函數（"abc" 填充區塊）=====
	w512abc := []uint64{0x6162638000000000, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 24}
	h512abc := sha512Compress(iv512, w512abc)
	fmt.Printf("sha512-abc=%016x%016x%016x%016x%016x%016x%016x%016x\n",
		h512abc[0], h512abc[1], h512abc[2], h512abc[3],
		h512abc[4], h512abc[5], h512abc[6], h512abc[7])

	// ===== SHA-1 壓縮函數（zero block）=====
	iv1 := []uint32{
		0x67452301, 0xEFCDAB89, 0x98BADCFE, 0x10325476, 0xC3D2E1F0,
	}
	w1 := make([]uint32, 16)
	h1 := sha1Compress(iv1, w1)
	fmt.Printf("sha1-zero-block=%08x%08x%08x%08x%08x\n",
		h1[0], h1[1], h1[2], h1[3], h1[4])

	// ===== SHA-1 壓縮函數（空字串填充區塊）=====
	w1empty := []uint32{0x80000000, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	h1empty := sha1Compress(iv1, w1empty)
	fmt.Printf("sha1-empty=%08x%08x%08x%08x%08x\n",
		h1empty[0], h1empty[1], h1empty[2], h1empty[3], h1empty[4])

	// ===== SHA-1 壓縮函數（"abc" 填充區塊）=====
	w1abc := []uint32{0x61626380, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x18}
	h1abc := sha1Compress(iv1, w1abc)
	fmt.Printf("sha1-abc=%08x%08x%08x%08x%08x\n",
		h1abc[0], h1abc[1], h1abc[2], h1abc[3], h1abc[4])

	// ===== AES-128 =====
	aesKey := []byte{0x2b, 0x7e, 0x15, 0x16, 0x28, 0xae, 0xd2, 0xa6, 0xab, 0xf7, 0x15, 0x88, 0x09, 0xcf, 0x4f, 0x3c}
	aesPt := []byte{0x6b, 0xc1, 0xbe, 0xe2, 0x2e, 0x40, 0x9f, 0x96, 0xe9, 0x3d, 0x7e, 0x11, 0x73, 0x93, 0x17, 0x2a}
	c, _ := aes.NewCipher(aesKey)
	ct := make([]byte, 16)
	c.Encrypt(ct, aesPt)
	fmt.Printf("aes-enc=%x\n", ct)
	dec := make([]byte, 16)
	c.Decrypt(dec, ct)
	fmt.Printf("aes-dec=%x\n", dec)

	// ===== DES =====
	desKey := []byte{0x13, 0x34, 0x57, 0x79, 0x9B, 0xBC, 0xDF, 0xF1}
	desPt := []byte{0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF}
	dc, _ := des.NewCipher(desKey)
	dct := make([]byte, 8)
	dc.Encrypt(dct, desPt)
	fmt.Printf("des-enc=%x\n", dct)
	ddec := make([]byte, 8)
	dc.Decrypt(ddec, dct)
	fmt.Printf("des-dec=%x\n", ddec)
}

// ─── 輔助函數 ─────────────────────────────

func printHash(name, key, valHex string) {
	fmt.Printf("%s=%s\n", key, valHex)
}

func testCRC32(s, label string) {
	h := crc32.NewIEEE()
	h.Write([]byte(s))
	printHash("crc32", label, fmt.Sprintf("%08x", h.Sum32()))
}

func testFNVa(s, label string) {
	h := fnv.New32a()
	h.Write([]byte(s))
	printHash("fnv1a32", label, fmt.Sprintf("%08x", h.Sum32()))
}

func testMD5(s, label string) {
	h := md5.New()
	h.Write([]byte(s))
	printHash("md5", label, fmt.Sprintf("%x", h.Sum(nil)))
}

func testSHA1(s, label string) {
	h := sha1.New()
	h.Write([]byte(s))
	printHash("sha1", label, fmt.Sprintf("%x", h.Sum(nil)))
}

func testSHA256(s, label string) {
	h := sha256.New()
	h.Write([]byte(s))
	printHash("sha256", label, fmt.Sprintf("%x", h.Sum(nil)))
}

func testSHA512(s, label string) {
	h := sha512.New()
	h.Write([]byte(s))
	printHash("sha512", label, fmt.Sprintf("%x", h.Sum(nil)))
}

// ─── SHA-256/SHA-512 壓縮函數（與 Nolang 實作一致）───

func sha256Compress(iv []uint32, w []uint32) []uint32 {
	K := [64]uint32{
		0x428A2F98, 0x71374491, 0xB5C0FBCF, 0xE9B5DBA5,
		0x3956C25B, 0x59F111F1, 0x923F82A4, 0xAB1C5ED5,
		0xD807AA98, 0x12835B01, 0x243185BE, 0x550C7DC3,
		0x72BE5D74, 0x80DEB1FE, 0x9BDC06A7, 0xC19BF174,
		0xE49B69C1, 0xEFBE4786, 0x0FC19DC6, 0x240CA1CC,
		0x2DE92C6F, 0x4A7484AA, 0x5CB0A9DC, 0x76F988DA,
		0x983E5152, 0xA831C66D, 0xB00327C8, 0xBF597FC7,
		0xC6E00BF3, 0xD5A79147, 0x06CA6351, 0x14292967,
		0x27B70A85, 0x2E1B2138, 0x4D2C6DFC, 0x53380D13,
		0x650A7354, 0x766A0ABB, 0x81C2C92E, 0x92722C85,
		0xA2BFE8A1, 0xA81A664B, 0xC24B8B70, 0xC76C51A3,
		0xD192E819, 0xD6990624, 0xF40E3585, 0x106AA070,
		0x19A4C116, 0x1E376C08, 0x2748774C, 0x34B0BCB5,
		0x391C0CB3, 0x4ED8AA4A, 0x5B9CCA4F, 0x682E6FF3,
		0x748F82EE, 0x78A5636F, 0x84C87814, 0x8CC70208,
		0x90BEFFFA, 0xA4506CEB, 0xBEF9A3F7, 0xC67178F2,
	}
	a, b, c, d, e, f, g, h := iv[0], iv[1], iv[2], iv[3], iv[4], iv[5], iv[6], iv[7]
	for i := 16; i < 64; i++ {
		s0 := (w[i-15]>>7 | w[i-15]<<25) ^ (w[i-15]>>18 | w[i-15]<<14) ^ (w[i-15] >> 3)
		s1 := (w[i-2]>>17 | w[i-2]<<15) ^ (w[i-2]>>19 | w[i-2]<<13) ^ (w[i-2] >> 10)
		w = append(w, w[i-16]+s0+w[i-7]+s1)
	}
	for i := 0; i < 64; i++ {
		S1 := ((e >> 6) | (e << 26)) ^ ((e >> 11) | (e << 21)) ^ ((e >> 25) | (e << 7))
		ch := (e & f) ^ ((^e) & g)
		S0 := ((a >> 2) | (a << 30)) ^ ((a >> 13) | (a << 19)) ^ ((a >> 22) | (a << 10))
		maj := (a & b) ^ (a & c) ^ (b & c)
		T1 := h + S1 + ch + K[i] + w[i]
		T2 := S0 + maj
		h, g, f, e, d, c, b, a = g, f, e, d+T1, c, b, a, T1+T2
	}
	return []uint32{iv[0] + a, iv[1] + b, iv[2] + c, iv[3] + d, iv[4] + e, iv[5] + f, iv[6] + g, iv[7] + h}
}

func sha512Compress(iv []uint64, w []uint64) []uint64 {
	K := [80]uint64{
		0x428A2F98D728AE22, 0x7137449123EF65CD, 0xB5C0FBCFEC4D3B2F, 0xE9B5DBA58189DBBC,
		0x3956C25BF348B538, 0x59F111F1B605D019, 0x923F82A4AF194F9B, 0xAB1C5ED5DA6D8118,
		0xD807AA98A3030242, 0x12835B0145706FBE, 0x243185BE4EE4B28C, 0x550C7DC3D5FFB4E2,
		0x72BE5D74F27B896F, 0x80DEB1FE3B1696B1, 0x9BDC06A725C71235, 0xC19BF174CF692694,
		0xE49B69C19EF14AD2, 0xEFBE4786384F25E3, 0x0FC19DC68B8CD5B5, 0x240CA1CC77AC9C65,
		0x2DE92C6F592B0275, 0x4A7484AA6EA6E483, 0x5CB0A9DCBD41FBD4, 0x76F988DA831153B5,
		0x983E5152EE66DFAB, 0xA831C66D2DB43210, 0xB00327C898FB213F, 0xBF597FC7BEEF0EE4,
		0xC6E00BF33DA88FC2, 0xD5A79147930AA725, 0x06CA6351E003826F, 0x142929670A0E6E70,
		0x27B70A8546D22FFC, 0x2E1B21385C26C926, 0x4D2C6DFC5AC42AED, 0x53380D139D95B3DF,
		0x650A73548BAF63DE, 0x766A0ABB3C77B2A8, 0x81C2C92E47EDAEE6, 0x92722C851482353B,
		0xA2BFE8A14CF10364, 0xA81A664BBC423001, 0xC24B8B70D0F89791, 0xC76C51A30654BE30,
		0xD192E819D6EF5218, 0xD69906245565A910, 0xF40E35855771202A, 0x106AA07032BBD1B8,
		0x19A4C116B8D2D0C8, 0x1E376C085141AB53, 0x2748774CDF8EEB99, 0x34B0BCB5E19B48A8,
		0x391C0CB3C5C95A63, 0x4ED8AA4AE3418ACB, 0x5B9CCA4F7763E373, 0x682E6FF3D6B2B8A3,
		0x748F82EE5DEFB2FC, 0x78A5636F43172F60, 0x84C87814A1F0AB72, 0x8CC702081A6439EC,
		0x90BEFFFA23631E28, 0xA4506CEBDE82BDE9, 0xBEF9A3F7B2C67915, 0xC67178F2E372532B,
		0xCA273ECEEA26619C, 0xD186B8C721C0C207, 0xEADA7DD6CDE0EB1E, 0xF57D4F7FEE6ED178,
		0x06F067AA72176FBA, 0x0A637DC5A2C898A6, 0x113F9804BEF90DAE, 0x1B710B35131C471B,
		0x28DB77F523047D84, 0x32CAAB7B40C72493, 0x3C9EBE0A15C9BEBC, 0x431D67C49C100D4C,
		0x4CC5D4BECB3E42B6, 0x597F299CFC657E2A, 0x5FCB6FAB3AD6FAEC, 0x6C44198C4A475817,
	}
	a, b, c, d, e, f, g, h := iv[0], iv[1], iv[2], iv[3], iv[4], iv[5], iv[6], iv[7]
	for i := 16; i < 80; i++ {
		s0 := (w[i-15]>>1 | w[i-15]<<63) ^ (w[i-15]>>8 | w[i-15]<<56) ^ (w[i-15] >> 7)
		s1 := (w[i-2]>>19 | w[i-2]<<45) ^ (w[i-2]>>61 | w[i-2]<<3) ^ (w[i-2] >> 6)
		w = append(w, w[i-16]+s0+w[i-7]+s1)
	}
	for i := 0; i < 80; i++ {
		S1 := ((e >> 14) | (e << 50)) ^ ((e >> 18) | (e << 46)) ^ ((e >> 41) | (e << 23))
		ch := (e & f) ^ ((^e) & g)
		S0 := ((a >> 28) | (a << 36)) ^ ((a >> 34) | (a << 30)) ^ ((a >> 39) | (a << 25))
		maj := (a & b) ^ (a & c) ^ (b & c)
		T1 := h + S1 + ch + K[i] + w[i]
		T2 := S0 + maj
		h, g, f, e, d, c, b, a = g, f, e, d+T1, c, b, a, T1+T2
	}
	return []uint64{iv[0] + a, iv[1] + b, iv[2] + c, iv[3] + d, iv[4] + e, iv[5] + f, iv[6] + g, iv[7] + h}
}

func sha1Compress(iv []uint32, w []uint32) []uint32 {
	a, b, c, d, e := iv[0], iv[1], iv[2], iv[3], iv[4]
	for t := 16; t < 80; t++ {
		wt := w[t-3] ^ w[t-8] ^ w[t-14] ^ w[t-16]
		wt = (wt << 1) | (wt >> 31)
		w = append(w, wt)
	}
	for t := 0; t < 80; t++ {
		var f, k uint32
		switch {
		case t < 20:
			f = (b & c) | (^b & d)
			k = 0x5A827999
		case t < 40:
			f = b ^ c ^ d
			k = 0x6ED9EBA1
		case t < 60:
			f = (b & c) | (b & d) | (c & d)
			k = 0x8F1BBCDC
		default:
			f = b ^ c ^ d
			k = 0xCA62C1D6
		}
		temp := ((a << 5) | (a >> 27)) + f + e + k + w[t]
		e = d
		d = c
		c = (b << 30) | (b >> 2)
		b = a
		a = temp
	}
	return []uint32{iv[0] + a, iv[1] + b, iv[2] + c, iv[3] + d, iv[4] + e}
}
