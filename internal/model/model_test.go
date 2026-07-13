package model

import "testing"

func TestParseValue(t *testing.T) {
	cases := []struct {
		raw   string
		isNum bool
		num   float64
		str   string
	}{
		{"42", true, 42, ""},
		{"42.5", true, 42.5, ""},
		{"-3", true, -3, ""},
		{"failed", false, 0, "failed"},
		{"", false, 0, ""},
		{"1.2.3.4", false, 0, "1.2.3.4"}, // an IP is a string, not a number
	}
	for _, c := range cases {
		v := ParseValue(c.raw)
		if v.IsNum != c.isNum || v.Num != c.num || v.Str != c.str {
			t.Errorf("ParseValue(%q) = %+v", c.raw, v)
		}
	}
}

func TestValueEqualAndString(t *testing.T) {
	if !ParseValue("42").Equal(NumValue(42)) {
		t.Error("42 == 42")
	}
	if ParseValue("42").Equal(StrValue("42")) {
		t.Error("number 42 must not equal string \"42\" — a type change is a change")
	}
	if NumValue(42.5).String() != "42.5" || StrValue("x").String() != "x" {
		t.Error("String() roundtrip broken")
	}
	if BoolValue(true).Num != 1 || BoolValue(false).Num != 0 {
		t.Error("BoolValue mapping broken")
	}
}
