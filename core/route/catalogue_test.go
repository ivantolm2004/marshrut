package route

import "testing"

func TestDestinationParsing(t *testing.T) {
	for _, tt := range []struct{ in, domain, prefix string }{
		{"https://EXAMPLE.com:8443/a?b#c", "example.com", ""},
		{"https://пример.рф/path", "xn--e1afmkfd.xn--p1ai", ""},
		{"203.0.113.9", "", "203.0.113.9/32"},
		{"2001:db8::1", "", "2001:db8::1/128"},
		{"203.0.113.19/24", "", "203.0.113.0/24"},
	} {
		d, p, err := ParseDestination(tt.in)
		if err != nil || d != tt.domain || p != tt.prefix {
			t.Fatal(tt.in, d, p, err)
		}
	}
	for _, in := range []string{"https://u:p@example.com", "file://example.com", "javascript:alert(1)", "https://", "example.com/path", "fe80::1%eth0"} {
		if _, _, err := ParseDestination(in); err == nil {
			t.Fatal("invalid destination", in)
		}
	}
}
