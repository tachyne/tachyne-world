package main

import (
	"strings"
	"testing"
)

func TestParseWith(t *testing.T) {
	cases := []struct {
		in   string
		want pluginSpec
		err  bool
	}{
		{"github.com/x/p", pluginSpec{module: "github.com/x/p"}, false},
		{"github.com/x/p@v1.2.0", pluginSpec{module: "github.com/x/p", version: "v1.2.0"}, false},
		{"github.com/x/p=../local", pluginSpec{module: "github.com/x/p", replace: "../local"}, false},
		{"github.com/x/p@abc123=../local", pluginSpec{module: "github.com/x/p", version: "abc123", replace: "../local"}, false},
		{"=../local", pluginSpec{}, true},
	}
	for _, c := range cases {
		got, err := parseWith(c.in)
		if c.err {
			if err == nil {
				t.Errorf("%q: expected error", c.in)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("%q → %+v (err %v), want %+v", c.in, got, err, c.want)
		}
	}
}

func TestGenMain(t *testing.T) {
	src := genMain([]pluginSpec{
		{module: "github.com/x/p"},
		{module: "example.com/q", replace: "../q"},
	})
	for _, want := range []string{
		`"github.com/tachyne/tachyne-world/servercmd"`,
		`_ "github.com/x/p"`,
		`_ "example.com/q"`,
		"func main() { servercmd.Main() }",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("generated main missing %q:\n%s", want, src)
		}
	}
	// No plugins: still a valid runner.
	src = genMain(nil)
	if !strings.Contains(src, "servercmd.Main()") {
		t.Error("empty plugin set broke the runner")
	}
}
