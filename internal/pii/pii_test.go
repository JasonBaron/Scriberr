package pii

import (
	"reflect"
	"strings"
	"testing"
)

func TestDetect(t *testing.T) {
	cases := []struct {
		text string
		want []string
	}{
		{"Can you confirm your date of birth? Sure, it's March 4, 1977.", []string{"dob"}},
		{"My DOB is 03/04/1977", []string{"dob"}},
		{"Call me at (555) 123-4567 or 555.987.6543", []string{"phone"}},
		{"email jason@example.com", []string{"email"}},
		{"SSN 123-45-6789", []string{"ssn"}},
		{"card 4111 1111 1111 1111", []string{"card"}},
		{"We live at 42 Maple Ridge Road now", []string{"address"}},
		{"What's your account number?", []string{"id"}},
		{"We talked for 45 minutes about 3 things in 2026.", []string{}},
		{"It takes 5 minutes on the way there, 2 more blocks to the place", []string{}},
		{"Order 1234 5678 9012 3456 shipped", []string{}}, // fails Luhn
	}
	for _, c := range cases {
		got := Detect(c.text)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("Detect(%q) = %v, want %v", c.text, got, c.want)
		}
	}
}

func TestRedact(t *testing.T) {
	in := "Patient date of birth: 03/04/1977. Born on March 4, 1977. Reach her at 555-123-4567 or a@b.co. SSN 123-45-6789. Lives at 12 Oak St. Card 4111-1111-1111-1111. Next visit May 5, 2027."
	out := Redact(in)
	for _, leak := range []string{"03/04/1977", "March 4, 1977", "555-123-4567", "a@b.co", "123-45-6789", "12 Oak St", "4111"} {
		if strings.Contains(out, leak) {
			t.Errorf("leaked %q in %q", leak, out)
		}
	}
	if !strings.Contains(out, "date of birth: [redacted]") || !strings.Contains(out, "May 5, 2027") {
		t.Errorf("unexpected redaction: %q", out)
	}
}
