package main

import "testing"

func TestParseCorsOrigins(t *testing.T) {
	if got := parseCorsOrigins(""); len(got) != 0 {
		t.Fatalf("parseCorsOrigins() = %v, want no browser origins", got)
	}
	got := parseCorsOrigins(" http://a , http://b ")
	if len(got) != 2 || got[0] != "http://a" || got[1] != "http://b" {
		t.Fatalf("parseCorsOrigins = %v", got)
	}
}

func TestMatchOrigin(t *testing.T) {
	cases := []struct {
		allow  []string
		origin string
		want   string
	}{
		{[]string{"*"}, "http://anything", "*"},
		{[]string{"*"}, "", "*"}, // non-browser
		{[]string{"http://192.0.2.*"}, "http://192.0.2.10:8080", "http://192.0.2.10:8080"},
		{[]string{"http://192.0.2.*"}, "http://10.0.0.5", ""},
		{[]string{"http://a"}, "http://a", "http://a"},
		{[]string{"http://a"}, "http://b", ""},
		{[]string{"http://a"}, "", "*"},
	}
	for i, c := range cases {
		if got := matchOrigin(c.allow, c.origin); got != c.want {
			t.Fatalf("case %d: matchOrigin(%v,%q) = %q, want %q", i, c.allow, c.origin, got, c.want)
		}
	}
}
