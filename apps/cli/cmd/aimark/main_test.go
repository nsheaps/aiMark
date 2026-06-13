package main

import "testing"

func TestDefaultsToBench(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{nil, true},                          // bare `aimark`
		{[]string{"--yes"}, true},            // flags only → bench
		{[]string{"--no-upload"}, true},      //
		{[]string{"--json", "--yes"}, true},  //
		{[]string{"-h"}, false},              // root help
		{[]string{"--help"}, false},          //
		{[]string{"-v"}, false},              // root version
		{[]string{"--version"}, false},       //
		{[]string{"bench"}, false},           // explicit subcommand
		{[]string{"run", "sprint-1"}, false}, //
		{[]string{"results", "list"}, false}, //
		{[]string{"typo-cmd"}, false},        // cobra reports unknown command
	}
	for _, tc := range cases {
		if got := defaultsToBench(tc.args); got != tc.want {
			t.Errorf("defaultsToBench(%v) = %v, want %v", tc.args, got, tc.want)
		}
	}
}
