package collector

import "testing"

func TestPercentInt(t *testing.T) {
	tests := []struct {
		in   string
		want int
	}{
		{in: "89", want: 89},
		{in: "89.90", want: 89},
		{in: "0.06", want: 0},
		{in: "-", want: 0},
		{in: "-1", want: 0},
	}
	for _, test := range tests {
		if got := percentInt(test.in); got != test.want {
			t.Errorf("percentInt(%q)=%d, want %d", test.in, got, test.want)
		}
	}
}
