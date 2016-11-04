package main

import (
	"fmt"
	logpkg "log"
	"os"
	"testing"
)

func init() {
	log = logpkg.New(os.Stderr, "", logpkg.LstdFlags)
}

func compareRunePage(rs [][]rune, ss []string) error {
	if len(rs) != len(ss) {
		max := len(rs)
		if len(ss) > max {
			max = len(ss)
		}
		for i := 0; i < max; i++ {
			if i < len(rs) {
				fmt.Printf(">%s\n", string(rs[i]))
			}
			if i < len(ss) {
				fmt.Printf("<%s\n", ss[i])
			}
		}

		return fmt.Errorf("wrong number of lines: got %d, want %d", len(rs), len(ss))
	}
	for i := range rs {
		if string(rs[i]) != ss[i] {
			return fmt.Errorf("line %d: got %q, want %q", i, string(rs[i]), ss[i])
		}
	}
	return nil
}

func TestGetBufferLinesToShow(t *testing.T) {
	type c struct {
		in   string
		outs []string
	}
	for i, tc := range []c{
		{
			"one short line",
			[]string{
				"one short line",
			},
		},
		{
			"two short\nlines",
			[]string{
				"two short",
				"lines",
			},
		},
		{
			"one long line that is past the 20 char limit",
			[]string{
				"one long line that i",
				"s past the 20 char l",
				"imit",
			},
		},
		{
			"a string\n\nwith a blank line",
			[]string{
				"a string",
				"",
				"with a blank line",
			},
		},
		{
			"\n\n",
			[]string{
				"",
				"",
			},
		},
		{
			"too\nmany\nlines\nto\nshow",
			[]string{
				"lines",
				"to",
				"show",
			},
		},
	} {
		if err := compareRunePage(getBufferLinesToShow(3, 20, 0, tc.in),
			tc.outs); err != nil {
			t.Errorf("Case %d: %v", i, err)
		}
	}
}
