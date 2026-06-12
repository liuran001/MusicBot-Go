package applemusic

import "testing"

// TestALACBitReaderRead verifies that read() pulls big-endian, MSB-first bit
// runs of arbitrary width correctly, including reads that straddle byte
// boundaries, and that it advances the position as it goes.
func TestALACBitReaderRead(t *testing.T) {
	// 0xB4 = 1011 0100, 0x2D = 0010 1101 -> bit stream 1011010000101101
	br := newAlacBitReader([]byte{0xB4, 0x2D})

	// read(0) must be a no-op returning 0.
	if v, err := br.read(0); err != nil || v != 0 {
		t.Fatalf("read(0) = (%d, %v), want (0, nil)", v, err)
	}

	steps := []struct {
		n    int
		want uint32
	}{
		{1, 0b1},        // 1
		{3, 0b011},      // 011
		{4, 0b0100},     // 0100  (crosses into nothing yet, still byte 0)
		{8, 0b00101101}, // 0010 1101 (straddles byte 0 -> byte 1)
	}
	for i, s := range steps {
		got, err := br.read(s.n)
		if err != nil {
			t.Fatalf("step %d: read(%d) error: %v", i, s.n, err)
		}
		if got != s.want {
			t.Fatalf("step %d: read(%d) = %b, want %b", i, s.n, got, s.want)
		}
	}

	// All 16 bits consumed; nothing left.
	if br.left() != 0 {
		t.Fatalf("left() = %d after consuming all bits, want 0", br.left())
	}
	// Reading past the end must error and not advance.
	if _, err := br.read(1); err == nil {
		t.Fatalf("read(1) past EOF: want error, got nil")
	}
}

// TestALACBitReaderShow verifies that show() returns the same value read()
// would but leaves the position untouched.
func TestALACBitReaderShow(t *testing.T) {
	br := newAlacBitReader([]byte{0xF0, 0x0F}) // 11110000 00001111

	v1, err := br.show(4)
	if err != nil {
		t.Fatalf("show(4) error: %v", err)
	}
	if v1 != 0b1111 {
		t.Fatalf("show(4) = %b, want 1111", v1)
	}
	if br.pos != 0 {
		t.Fatalf("show(4) advanced pos to %d, want 0", br.pos)
	}

	// A second show must return the same value (position not moved).
	v2, err := br.show(4)
	if err != nil {
		t.Fatalf("second show(4) error: %v", err)
	}
	if v2 != v1 {
		t.Fatalf("second show(4) = %b, want %b", v2, v1)
	}

	// Now read should agree with show, then advance.
	r, err := br.read(4)
	if err != nil {
		t.Fatalf("read(4) error: %v", err)
	}
	if r != v1 {
		t.Fatalf("read(4) = %b, want %b (same as show)", r, v1)
	}
	if br.pos != 4 {
		t.Fatalf("read(4) pos = %d, want 4", br.pos)
	}

	// show beyond the available bits must return an error.
	if _, err := br.show(64); err == nil {
		t.Fatalf("show(64) past EOF: want error, got nil")
	}
}

// TestALACBitReaderSkip verifies that skip() advances the position, rejects
// overruns without moving, and leaves subsequent reads correctly aligned.
func TestALACBitReaderSkip(t *testing.T) {
	br := newAlacBitReader([]byte{0xAB, 0xCD}) // 10101011 11001101

	if err := br.skip(4); err != nil {
		t.Fatalf("skip(4) error: %v", err)
	}
	if br.pos != 4 {
		t.Fatalf("pos = %d after skip(4), want 4", br.pos)
	}
	// Next nibble of 0xAB is 0b1011.
	if v, err := br.read(4); err != nil || v != 0b1011 {
		t.Fatalf("read(4) after skip = (%b, %v), want (1011, nil)", v, err)
	}

	// skip(0) is a no-op.
	if err := br.skip(0); err != nil || br.pos != 8 {
		t.Fatalf("skip(0): pos=%d err=%v, want pos=8 nil", br.pos, err)
	}

	// Overrun skip must error and must NOT advance the position.
	before := br.pos
	if err := br.skip(100); err == nil {
		t.Fatalf("skip(100) past EOF: want error, got nil")
	}
	if br.pos != before {
		t.Fatalf("failed skip advanced pos from %d to %d", before, br.pos)
	}

	// skip exactly to the end is allowed.
	if err := br.skip(br.left()); err != nil {
		t.Fatalf("skip to exact end error: %v", err)
	}
	if br.left() != 0 {
		t.Fatalf("left() = %d after skipping to end, want 0", br.left())
	}
}

// TestALACBitReaderReadSigned verifies two's-complement sign extension over a
// range of widths, for both positive and negative values.
func TestALACBitReaderReadSigned(t *testing.T) {
	tests := []struct {
		name string
		buf  []byte
		n    int
		want int32
	}{
		// 0x80 = 1000_0000: top bit set in a 4-bit read -> 1000 = -8.
		{"neg4bit", []byte{0x80}, 4, -8},
		// 0x70 = 0111_0000: 4-bit read -> 0111 = +7.
		{"pos4bit", []byte{0x70}, 4, 7},
		// 0xFF: 8-bit read -> 1111_1111 = -1.
		{"negFF", []byte{0xFF}, 8, -1},
		// 0x7F: 8-bit read -> 0111_1111 = +127.
		{"pos7F", []byte{0x7F}, 8, 127},
		// 16-bit 0x8000 = -32768.
		{"neg16bit", []byte{0x80, 0x00}, 16, -32768},
		// 16-bit 0x0001 = +1.
		{"pos16bit", []byte{0x00, 0x01}, 16, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			br := newAlacBitReader(tc.buf)
			got, err := br.readSigned(tc.n)
			if err != nil {
				t.Fatalf("readSigned(%d) error: %v", tc.n, err)
			}
			if got != tc.want {
				t.Fatalf("readSigned(%d) = %d, want %d", tc.n, got, tc.want)
			}
		})
	}

	// readSigned past EOF must propagate the error.
	br := newAlacBitReader([]byte{0xFF})
	if _, err := br.readSigned(16); err == nil {
		t.Fatalf("readSigned(16) on 1 byte: want EOF error, got nil")
	}
}

// TestALACBitReaderUnary09 verifies the capped unary decoder used by the rice
// scalar decoder: it counts leading 1s up to a maximum of 9.
func TestALACBitReaderUnary09(t *testing.T) {
	// 0x00 -> first bit is 0 -> count 0.
	if v, err := newAlacBitReader([]byte{0x00}).unary09(); err != nil || v != 0 {
		t.Fatalf("unary09(0x00) = (%d, %v), want (0, nil)", v, err)
	}
	// 0xE0 = 1110_0000 -> three leading 1s then a 0 -> count 3.
	if v, err := newAlacBitReader([]byte{0xE0}).unary09(); err != nil || v != 3 {
		t.Fatalf("unary09(0xE0) = (%d, %v), want (3, nil)", v, err)
	}
	// 0xFF,0xFF = sixteen 1s -> capped at 9.
	if v, err := newAlacBitReader([]byte{0xFF, 0xFF}).unary09(); err != nil || v != 9 {
		t.Fatalf("unary09(all ones) = (%d, %v), want (9, nil)", v, err)
	}
}

// TestALACAvLog2 spot-checks the integer log2 helper.
func TestALACAvLog2(t *testing.T) {
	cases := map[uint32]int{0: 0, 1: 0, 2: 1, 3: 1, 4: 2, 7: 2, 8: 3, 255: 7, 256: 8}
	for in, want := range cases {
		if got := alacAvLog2(in); got != want {
			t.Fatalf("alacAvLog2(%d) = %d, want %d", in, got, want)
		}
	}
}
