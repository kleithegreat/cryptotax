package decimal

import "testing"

func TestCanon(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"0", "0"},
		{"-0", "0"},
		{"0.0", "0"},
		{"1000", "1000"},
		{"1.50", "1.5"},
		{"-1.50", "-1.5"},
		{"1e-05", "0.00001"},
		{"1E-05", "0.00001"},
		{"2.5e3", "2500"},
		{"1.234567890123456789", "1.234567890123456789"},
		{"-0.000000000000000001", "-0.000000000000000001"},
		{".5", "0.5"},
		{"00012.3400", "12.34"},
	}
	for _, c := range cases {
		got, err := Canon(c.in)
		if err != nil {
			t.Errorf("Canon(%q) error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("Canon(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCanonRejectsInvalid(t *testing.T) {
	for _, in := range []string{"", " ", "abc", "1.2.3", "1/3", "0x10", "1,000"} {
		if got, err := Canon(in); err == nil {
			t.Errorf("Canon(%q) = %q, want error", in, got)
		}
	}
}
