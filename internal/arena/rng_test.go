// SPDX-License-Identifier: AGPL-3.0-or-later

package arena

import "testing"

// nextVectors are the first ten Next() outputs per seed (match.md section 4).
var nextVectors = []struct {
	seed uint64
	out  [10]uint64
}{
	{0, [10]uint64{
		0xE220A8397B1DCDAF, 0x6E789E6AA1B965F4, 0x06C45D188009454F, 0xF88BB8A8724C81EC, 0x1B39896A51A8749B,
		0x53CB9F0C747EA2EA, 0x2C829ABE1F4532E1, 0xC584133AC916AB3C, 0x3EE5789041C98AC3, 0xF3B8488C368CB0A6,
	}},
	{1, [10]uint64{
		0x910A2DEC89025CC1, 0xBEEB8DA1658EEC67, 0xF893A2EEFB32555E, 0x71C18690EE42C90B, 0x71BB54D8D101B5B9,
		0xC34D0BFF90150280, 0xE099EC6CD7363CA5, 0x85E7BB0F12278575, 0x491718DE357E3DA8, 0xCB435C8E74616796,
	}},
	{1<<64 - 1, [10]uint64{
		0xE4D971771B652C20, 0xE99FF867DBF682C9, 0x382FF84CB27281E9, 0x6D1DB36CCBA982D2, 0xB4A0472E578069AE,
		0xD31DADBDA438BB33, 0xF14F2CF802083FA5, 0x405DA438A39E8064, 0xC4FEA708156E0C84, 0x031E50FE7BBD6E1C,
	}},
}

// drawVectors are the Draw rows of match.md section 4: results and the number of
// Next() outputs each call consumed.
var drawVectors = []struct {
	seed     uint64
	n        uint64
	results  []uint64
	consumed []int
}{
	{0, 10, []uint64{5, 0, 9, 4, 7, 0}, []int{1, 1, 1, 1, 1, 1}},
	{1, 8000, []uint64{6465, 4519, 2590, 4235, 761, 2048}, []int{1, 1, 1, 1, 1, 1}},
	{2, 1, []uint64{0, 0, 0}, []int{1, 1, 1}},
	{0, 9223372036854775809, []uint64{7070836379803831726, 8686239339925766635, 5009149828745571131, 8338494477124284581}, []int{1, 3, 4, 2}},
	{1, 9223372036854775809, []uint64{1227844342346046656, 4533873174211652710, 8688467253428114781, 4849545566009754239}, []int{1, 1, 1, 3}},
	{3, 9223372036854775809, []uint64{3694763184872335752, 2084015055746161920, 2512858195355979526, 7170589470788784661}, []int{2, 1, 3, 2}},
}

func TestRNGVectors(t *testing.T) {
	for _, v := range nextVectors {
		r := NewRNG(v.seed)
		for i, want := range v.out {
			if got := r.Next(); got != want {
				t.Errorf("seed %d Next() #%d = %#x, want %#x", v.seed, i+1, got, want)
			}
		}
	}
	for _, v := range drawVectors {
		r := NewRNG(v.seed)
		ref := NewRNG(v.seed) // advanced by Next() to count consumed outputs
		for i, want := range v.results {
			if got := r.Draw(v.n); got != want {
				t.Errorf("seed %d Draw(%d) call %d = %d, want %d", v.seed, v.n, i+1, got, want)
			}
			for k := 0; k < v.consumed[i]; k++ {
				ref.Next()
			}
			if r.s != ref.s {
				t.Errorf("seed %d Draw(%d) call %d did not consume exactly %d outputs", v.seed, v.n, i+1, v.consumed[i])
			}
		}
	}
}

func TestDrawRange(t *testing.T) {
	r := NewRNG(99)
	for _, n := range []uint64{1, 2, 3, 7, 100, 1<<20 + 1} {
		for i := 0; i < 200; i++ {
			if got := r.Draw(n); got >= n {
				t.Fatalf("Draw(%d) = %d, out of range", n, got)
			}
		}
	}
}
