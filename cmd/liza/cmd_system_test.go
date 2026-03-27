package main

import (
	"testing"
)

func TestTuiCmd_HeadlessFlag(t *testing.T) {
	flag := tuiCmd.Flags().Lookup("headless")
	if flag == nil {
		t.Fatal("tuiCmd missing --headless flag")
	}
	if flag.DefValue != "false" {
		t.Errorf("--headless default = %q, want %q", flag.DefValue, "false")
	}
	if flag.Usage == "" {
		t.Error("--headless flag has no usage text")
	}
}

func TestTuiCmd_IntervalFlag(t *testing.T) {
	flag := tuiCmd.Flags().Lookup("interval")
	if flag == nil {
		t.Fatal("tuiCmd missing --interval flag (needed for headless backward compatibility)")
	}
}

func TestTuiCmd_ShortDescription(t *testing.T) {
	want := "Interactive TUI dashboard for monitoring Liza"
	if tuiCmd.Short != want {
		t.Errorf("tuiCmd.Short = %q, want %q", tuiCmd.Short, want)
	}
}
