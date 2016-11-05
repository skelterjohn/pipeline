/*
Copyright 2016 Google Inc. All rights reserved.
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
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
		{
			"\tone tab\nk\ttwo tab\n",
			[]string{
				"    one tab",
				"k   two tab",
				"",
			},
		},
	} {
		rs, _ := getBufferLinesToShow(3, 20, 0, tc.in)
		if err := compareRunePage(rs, tc.outs); err != nil {
			t.Errorf("Case %d: %v", i, err)
		}
	}
}

func TestGetBufferLinesToShowScroll(t *testing.T) {
	type c struct {
		in   string
		outs []string
	}
	for i, tc := range []c{
		{
			"too\nmany\nlines\nto\nshow",
			[]string{
				"too",
				"many",
				"lines",
			},
		},
	} {
		rs, _ := getBufferLinesToShow(3, 20, 2, tc.in)
		if err := compareRunePage(rs, tc.outs); err != nil {
			t.Errorf("Case %d: %v", i, err)
		}
	}
}
