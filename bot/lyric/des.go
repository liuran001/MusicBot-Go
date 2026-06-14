package lyric

// This is a faithful port of meting-api-new's QrcDecode\Decoder PHP class, which
// implements the bespoke triple-DES variant QQ Music uses for QRC lyrics. It is
// NOT standard DES — the permutation/compression tables differ — so Go's
// crypto/des does not reproduce it. Ported verbatim to guarantee identical
// output. Source: luren-dc/QQMusicApi tripledes.py.

// qrcSBox holds the 8 substitution boxes (flat 64-entry form).
var qrcSBox = [8][64]uint64{
	{
		14, 4, 13, 1, 2, 15, 11, 8, 3, 10, 6, 12, 5, 9, 0, 7,
		0, 15, 7, 4, 14, 2, 13, 1, 10, 6, 12, 11, 9, 5, 3, 8,
		4, 1, 14, 8, 13, 6, 2, 11, 15, 12, 9, 7, 3, 10, 5, 0,
		15, 12, 8, 2, 4, 9, 1, 7, 5, 11, 3, 14, 10, 0, 6, 13,
	},
	{
		15, 1, 8, 14, 6, 11, 3, 4, 9, 7, 2, 13, 12, 0, 5, 10,
		3, 13, 4, 7, 15, 2, 8, 15, 12, 0, 1, 10, 6, 9, 11, 5,
		0, 14, 7, 11, 10, 4, 13, 1, 5, 8, 12, 6, 9, 3, 2, 15,
		13, 8, 10, 1, 3, 15, 4, 2, 11, 6, 7, 12, 0, 5, 14, 9,
	},
	{
		10, 0, 9, 14, 6, 3, 15, 5, 1, 13, 12, 7, 11, 4, 2, 8,
		13, 7, 0, 9, 3, 4, 6, 10, 2, 8, 5, 14, 12, 11, 15, 1,
		13, 6, 4, 9, 8, 15, 3, 0, 11, 1, 2, 12, 5, 10, 14, 7,
		1, 10, 13, 0, 6, 9, 8, 7, 4, 15, 14, 3, 11, 5, 2, 12,
	},
	{
		7, 13, 14, 3, 0, 6, 9, 10, 1, 2, 8, 5, 11, 12, 4, 15,
		13, 8, 11, 5, 6, 15, 0, 3, 4, 7, 2, 12, 1, 10, 14, 9,
		10, 6, 9, 0, 12, 11, 7, 13, 15, 1, 3, 14, 5, 2, 8, 4,
		3, 15, 0, 6, 10, 10, 13, 8, 9, 4, 5, 11, 12, 7, 2, 14,
	},
	{
		2, 12, 4, 1, 7, 10, 11, 6, 8, 5, 3, 15, 13, 0, 14, 9,
		14, 11, 2, 12, 4, 7, 13, 1, 5, 0, 15, 10, 3, 9, 8, 6,
		4, 2, 1, 11, 10, 13, 7, 8, 15, 9, 12, 5, 6, 3, 0, 14,
		11, 8, 12, 7, 1, 14, 2, 13, 6, 15, 0, 9, 10, 4, 5, 3,
	},
	{
		12, 1, 10, 15, 9, 2, 6, 8, 0, 13, 3, 4, 14, 7, 5, 11,
		10, 15, 4, 2, 7, 12, 9, 5, 6, 1, 13, 14, 0, 11, 3, 8,
		9, 14, 15, 5, 2, 8, 12, 3, 7, 0, 4, 10, 1, 13, 11, 6,
		4, 3, 2, 12, 9, 5, 15, 10, 11, 14, 1, 7, 6, 0, 8, 13,
	},
	{
		4, 11, 2, 14, 15, 0, 8, 13, 3, 12, 9, 7, 5, 10, 6, 1,
		13, 0, 11, 7, 4, 9, 1, 10, 14, 3, 5, 12, 2, 15, 8, 6,
		1, 4, 11, 13, 12, 3, 7, 14, 10, 15, 6, 8, 0, 5, 9, 2,
		6, 11, 13, 8, 1, 4, 10, 7, 9, 5, 0, 15, 14, 2, 3, 12,
	},
	{
		13, 2, 8, 4, 6, 15, 11, 1, 10, 9, 3, 14, 5, 0, 12, 7,
		1, 15, 13, 8, 10, 3, 7, 4, 12, 5, 6, 11, 0, 14, 9, 2,
		7, 11, 4, 1, 9, 12, 14, 2, 0, 6, 10, 13, 15, 3, 5, 8,
		2, 1, 14, 7, 4, 10, 8, 13, 15, 12, 9, 0, 3, 5, 6, 11,
	},
}

const (
	qrcEnMode = 1
	qrcDeMode = 0
)

// qrcBitnum extracts bit b from the 8-byte block a and shifts it left by c.
func qrcBitnum(a []byte, b, c int) uint64 {
	index := (b/32)*4 + 3 - (b%32)/8
	by := uint64(a[index])
	shift := 7 - (b % 8)
	return ((by >> uint(shift)) & 1) << uint(c)
}

// qrcBitnumIntr extracts bit b from the 32-bit int a (MSB-first) and shifts by c.
func qrcBitnumIntr(a uint64, b, c int) uint64 {
	return ((a >> uint(31-b)) & 1) << uint(c)
}

// qrcBitnumIntl extracts bit b from a (after a left shift) and aligns it at c.
func qrcBitnumIntl(a uint64, b, c int) uint64 {
	return (((a << uint(b)) & 0x80000000) >> uint(c))
}

func qrcSBoxBit(a uint64) uint64 {
	return (a & 32) | ((a & 31) >> 1) | ((a & 1) << 4)
}

func qrcInitialPermutation(in []byte) (uint64, uint64) {
	s0 := qrcBitnum(in, 57, 31) | qrcBitnum(in, 49, 30) | qrcBitnum(in, 41, 29) | qrcBitnum(in, 33, 28) |
		qrcBitnum(in, 25, 27) | qrcBitnum(in, 17, 26) | qrcBitnum(in, 9, 25) | qrcBitnum(in, 1, 24) |
		qrcBitnum(in, 59, 23) | qrcBitnum(in, 51, 22) | qrcBitnum(in, 43, 21) | qrcBitnum(in, 35, 20) |
		qrcBitnum(in, 27, 19) | qrcBitnum(in, 19, 18) | qrcBitnum(in, 11, 17) | qrcBitnum(in, 3, 16) |
		qrcBitnum(in, 61, 15) | qrcBitnum(in, 53, 14) | qrcBitnum(in, 45, 13) | qrcBitnum(in, 37, 12) |
		qrcBitnum(in, 29, 11) | qrcBitnum(in, 21, 10) | qrcBitnum(in, 13, 9) | qrcBitnum(in, 5, 8) |
		qrcBitnum(in, 63, 7) | qrcBitnum(in, 55, 6) | qrcBitnum(in, 47, 5) | qrcBitnum(in, 39, 4) |
		qrcBitnum(in, 31, 3) | qrcBitnum(in, 23, 2) | qrcBitnum(in, 15, 1) | qrcBitnum(in, 7, 0)

	s1 := qrcBitnum(in, 56, 31) | qrcBitnum(in, 48, 30) | qrcBitnum(in, 40, 29) | qrcBitnum(in, 32, 28) |
		qrcBitnum(in, 24, 27) | qrcBitnum(in, 16, 26) | qrcBitnum(in, 8, 25) | qrcBitnum(in, 0, 24) |
		qrcBitnum(in, 58, 23) | qrcBitnum(in, 50, 22) | qrcBitnum(in, 42, 21) | qrcBitnum(in, 34, 20) |
		qrcBitnum(in, 26, 19) | qrcBitnum(in, 18, 18) | qrcBitnum(in, 10, 17) | qrcBitnum(in, 2, 16) |
		qrcBitnum(in, 60, 15) | qrcBitnum(in, 52, 14) | qrcBitnum(in, 44, 13) | qrcBitnum(in, 36, 12) |
		qrcBitnum(in, 28, 11) | qrcBitnum(in, 20, 10) | qrcBitnum(in, 12, 9) | qrcBitnum(in, 4, 8) |
		qrcBitnum(in, 62, 7) | qrcBitnum(in, 54, 6) | qrcBitnum(in, 46, 5) | qrcBitnum(in, 38, 4) |
		qrcBitnum(in, 30, 3) | qrcBitnum(in, 22, 2) | qrcBitnum(in, 14, 1) | qrcBitnum(in, 6, 0)

	return s0, s1
}

func qrcInversePermutation(s0, s1 uint64) []byte {
	data := make([]byte, 8)
	data[3] = byte(qrcBitnumIntr(s1, 7, 7) | qrcBitnumIntr(s0, 7, 6) | qrcBitnumIntr(s1, 15, 5) | qrcBitnumIntr(s0, 15, 4) |
		qrcBitnumIntr(s1, 23, 3) | qrcBitnumIntr(s0, 23, 2) | qrcBitnumIntr(s1, 31, 1) | qrcBitnumIntr(s0, 31, 0))
	data[2] = byte(qrcBitnumIntr(s1, 6, 7) | qrcBitnumIntr(s0, 6, 6) | qrcBitnumIntr(s1, 14, 5) | qrcBitnumIntr(s0, 14, 4) |
		qrcBitnumIntr(s1, 22, 3) | qrcBitnumIntr(s0, 22, 2) | qrcBitnumIntr(s1, 30, 1) | qrcBitnumIntr(s0, 30, 0))
	data[1] = byte(qrcBitnumIntr(s1, 5, 7) | qrcBitnumIntr(s0, 5, 6) | qrcBitnumIntr(s1, 13, 5) | qrcBitnumIntr(s0, 13, 4) |
		qrcBitnumIntr(s1, 21, 3) | qrcBitnumIntr(s0, 21, 2) | qrcBitnumIntr(s1, 29, 1) | qrcBitnumIntr(s0, 29, 0))
	data[0] = byte(qrcBitnumIntr(s1, 4, 7) | qrcBitnumIntr(s0, 4, 6) | qrcBitnumIntr(s1, 12, 5) | qrcBitnumIntr(s0, 12, 4) |
		qrcBitnumIntr(s1, 20, 3) | qrcBitnumIntr(s0, 20, 2) | qrcBitnumIntr(s1, 28, 1) | qrcBitnumIntr(s0, 28, 0))
	data[7] = byte(qrcBitnumIntr(s1, 3, 7) | qrcBitnumIntr(s0, 3, 6) | qrcBitnumIntr(s1, 11, 5) | qrcBitnumIntr(s0, 11, 4) |
		qrcBitnumIntr(s1, 19, 3) | qrcBitnumIntr(s0, 19, 2) | qrcBitnumIntr(s1, 27, 1) | qrcBitnumIntr(s0, 27, 0))
	data[6] = byte(qrcBitnumIntr(s1, 2, 7) | qrcBitnumIntr(s0, 2, 6) | qrcBitnumIntr(s1, 10, 5) | qrcBitnumIntr(s0, 10, 4) |
		qrcBitnumIntr(s1, 18, 3) | qrcBitnumIntr(s0, 18, 2) | qrcBitnumIntr(s1, 26, 1) | qrcBitnumIntr(s0, 26, 0))
	data[5] = byte(qrcBitnumIntr(s1, 1, 7) | qrcBitnumIntr(s0, 1, 6) | qrcBitnumIntr(s1, 9, 5) | qrcBitnumIntr(s0, 9, 4) |
		qrcBitnumIntr(s1, 17, 3) | qrcBitnumIntr(s0, 17, 2) | qrcBitnumIntr(s1, 25, 1) | qrcBitnumIntr(s0, 25, 0))
	data[4] = byte(qrcBitnumIntr(s1, 0, 7) | qrcBitnumIntr(s0, 0, 6) | qrcBitnumIntr(s1, 8, 5) | qrcBitnumIntr(s0, 8, 4) |
		qrcBitnumIntr(s1, 16, 3) | qrcBitnumIntr(s0, 16, 2) | qrcBitnumIntr(s1, 24, 1) | qrcBitnumIntr(s0, 24, 0))
	return data
}

func qrcF(state uint64, key [6]byte) uint64 {
	t1 := qrcBitnumIntl(state, 31, 0) | ((state & 0xf0000000) >> 1) | qrcBitnumIntl(state, 4, 5) | qrcBitnumIntl(state, 3, 6) |
		((state & 0x0f000000) >> 3) | qrcBitnumIntl(state, 8, 11) | qrcBitnumIntl(state, 7, 12) | ((state & 0x00f00000) >> 5) |
		qrcBitnumIntl(state, 12, 17) | qrcBitnumIntl(state, 11, 18) | ((state & 0x000f0000) >> 7) | qrcBitnumIntl(state, 16, 23)

	t2 := qrcBitnumIntl(state, 15, 0) | ((state & 0x0000f000) << 15) | qrcBitnumIntl(state, 20, 5) | qrcBitnumIntl(state, 19, 6) |
		((state & 0x00000f00) << 13) | qrcBitnumIntl(state, 24, 11) | qrcBitnumIntl(state, 23, 12) | ((state & 0x000000f0) << 11) |
		qrcBitnumIntl(state, 28, 17) | qrcBitnumIntl(state, 27, 18) | ((state & 0x0000000f) << 9) | qrcBitnumIntl(state, 0, 23)

	lrgstate := [6]uint64{
		(t1 >> 24) & 0xff,
		(t1 >> 16) & 0xff,
		(t1 >> 8) & 0xff,
		(t2 >> 24) & 0xff,
		(t2 >> 16) & 0xff,
		(t2 >> 8) & 0xff,
	}
	for i := 0; i < 6; i++ {
		lrgstate[i] ^= uint64(key[i])
	}

	state = (qrcSBox[0][qrcSBoxBit(lrgstate[0]>>2)] << 28) |
		(qrcSBox[1][qrcSBoxBit(((lrgstate[0]&0x03)<<4)|(lrgstate[1]>>4))] << 24) |
		(qrcSBox[2][qrcSBoxBit(((lrgstate[1]&0x0f)<<2)|(lrgstate[2]>>6))] << 20) |
		(qrcSBox[3][qrcSBoxBit(lrgstate[2]&0x3f)] << 16) |
		(qrcSBox[4][qrcSBoxBit(lrgstate[3]>>2)] << 12) |
		(qrcSBox[5][qrcSBoxBit(((lrgstate[3]&0x03)<<4)|(lrgstate[4]>>4))] << 8) |
		(qrcSBox[6][qrcSBoxBit(((lrgstate[4]&0x0f)<<2)|(lrgstate[5]>>6))] << 4) |
		qrcSBox[7][qrcSBoxBit(lrgstate[5]&0x3f)]

	return qrcBitnumIntl(state, 15, 0) | qrcBitnumIntl(state, 6, 1) | qrcBitnumIntl(state, 19, 2) | qrcBitnumIntl(state, 20, 3) |
		qrcBitnumIntl(state, 28, 4) | qrcBitnumIntl(state, 11, 5) | qrcBitnumIntl(state, 27, 6) | qrcBitnumIntl(state, 16, 7) |
		qrcBitnumIntl(state, 0, 8) | qrcBitnumIntl(state, 14, 9) | qrcBitnumIntl(state, 22, 10) | qrcBitnumIntl(state, 25, 11) |
		qrcBitnumIntl(state, 4, 12) | qrcBitnumIntl(state, 17, 13) | qrcBitnumIntl(state, 30, 14) | qrcBitnumIntl(state, 9, 15) |
		qrcBitnumIntl(state, 1, 16) | qrcBitnumIntl(state, 7, 17) | qrcBitnumIntl(state, 23, 18) | qrcBitnumIntl(state, 13, 19) |
		qrcBitnumIntl(state, 31, 20) | qrcBitnumIntl(state, 26, 21) | qrcBitnumIntl(state, 2, 22) | qrcBitnumIntl(state, 8, 23) |
		qrcBitnumIntl(state, 18, 24) | qrcBitnumIntl(state, 12, 25) | qrcBitnumIntl(state, 29, 26) | qrcBitnumIntl(state, 5, 27) |
		qrcBitnumIntl(state, 21, 28) | qrcBitnumIntl(state, 10, 29) | qrcBitnumIntl(state, 3, 30) | qrcBitnumIntl(state, 24, 31)
}

func qrcACrypt(in []byte, key [16][6]byte) []byte {
	s0, s1 := qrcInitialPermutation(in)
	for idx := 0; idx < 15; idx++ {
		prev := s1
		s1 = qrcF(s1, key[idx]) ^ s0
		s0 = prev
	}
	s0 = qrcF(s1, key[15]) ^ s0
	return qrcInversePermutation(s0, s1)
}

func qrcKeySchedule(key []byte, mode int) [16][6]byte {
	var schedule [16][6]byte
	keyRndShift := [16]int{1, 1, 2, 2, 2, 2, 2, 2, 1, 2, 2, 2, 2, 2, 2, 1}
	keyPermC := [28]int{56, 48, 40, 32, 24, 16, 8, 0, 57, 49, 41, 33, 25, 17, 9, 1, 58, 50, 42, 34, 26, 18, 10, 2, 59, 51, 43, 35}
	keyPermD := [28]int{62, 54, 46, 38, 30, 22, 14, 6, 61, 53, 45, 37, 29, 21, 13, 5, 60, 52, 44, 36, 28, 20, 12, 4, 27, 19, 11, 3}
	keyCompression := [48]int{
		13, 16, 10, 23, 0, 4, 2, 27, 14, 5, 20, 9, 22, 18, 11, 3, 25, 7, 15, 6, 26, 19, 12, 1,
		40, 51, 30, 36, 46, 54, 29, 39, 50, 44, 32, 47, 43, 48, 38, 55, 33, 52, 45, 41, 49, 35, 28, 31,
	}

	var c, d uint64
	for i := 0; i < 28; i++ {
		c += qrcBitnum(key, keyPermC[i], 31-i)
	}
	for i := 0; i < 28; i++ {
		d += qrcBitnum(key, keyPermD[i], 31-i)
	}

	for i := 0; i < 16; i++ {
		shift := keyRndShift[i]
		c = ((c << uint(shift)) | (c >> uint(28-shift))) & 0xfffffff0
		d = ((d << uint(shift)) | (d >> uint(28-shift))) & 0xfffffff0

		togen := i
		if mode == 0 {
			togen = 15 - i
		}
		for j := 0; j < 6; j++ {
			schedule[togen][j] = 0
		}
		for j := 0; j < 24; j++ {
			schedule[togen][j/8] |= byte(qrcBitnumIntr(c, keyCompression[j], 7-(j%8)))
		}
		for j := 24; j < 48; j++ {
			schedule[togen][j/8] |= byte(qrcBitnumIntr(d, keyCompression[j]-27, 7-(j%8)))
		}
	}
	return schedule
}

func qrcTripleDESKeySetup(key []byte, mode int) [3][16][6]byte {
	var out [3][16][6]byte
	if mode == qrcEnMode {
		out[0] = qrcKeySchedule(key[0:], qrcEnMode)
		out[1] = qrcKeySchedule(key[8:], qrcDeMode)
		out[2] = qrcKeySchedule(key[16:], qrcEnMode)
		return out
	}
	out[0] = qrcKeySchedule(key[16:], qrcDeMode)
	out[1] = qrcKeySchedule(key[8:], qrcEnMode)
	out[2] = qrcKeySchedule(key[0:], qrcDeMode)
	return out
}

func qrcTripleDESCrypt(data []byte, keys [3][16][6]byte) []byte {
	for i := 0; i < 3; i++ {
		data = qrcACrypt(data, keys[i])
	}
	return data
}

// qrcDESDecrypt decrypts an ECB-mode QRC blob with the bespoke triple-DES,
// mirroring Decoder::decode (minus the trailing zlib step).
func qrcDESDecrypt(bin []byte) []byte {
	schedule := qrcTripleDESKeySetup(qrcDESKey, qrcDeMode)
	out := make([]byte, 0, len(bin))
	for i := 0; i+8 <= len(bin); i += 8 {
		out = append(out, qrcTripleDESCrypt(bin[i:i+8], schedule)...)
	}
	return out
}
