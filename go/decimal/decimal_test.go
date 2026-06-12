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

func TestMul(t *testing.T) {
	got, err := Mul("0.1", "0.2")
	if err != nil {
		t.Fatalf("Mul error: %v", err)
	}
	if got != "0.02" {
		t.Errorf("Mul(0.1, 0.2) = %q, want 0.02", got)
	}

	got, err = Mul("123456789.123456789", "1000000000")
	if err != nil {
		t.Fatalf("Mul error: %v", err)
	}
	if got != "123456789123456789" {
		t.Errorf("large Mul = %q, want 123456789123456789", got)
	}

	if _, err := Mul("x", "1"); err == nil {
		t.Error("Mul with invalid input should error")
	}
}

func TestSignAbs(t *testing.T) {
	if s, _ := Sign("-3.2"); s != -1 {
		t.Errorf("Sign(-3.2) = %d", s)
	}
	if s, _ := Sign("0.000"); s != 0 {
		t.Errorf("Sign(0.000) = %d", s)
	}
	if a, _ := Abs("-3.20"); a != "3.2" {
		t.Errorf("Abs(-3.20) = %q", a)
	}
}
