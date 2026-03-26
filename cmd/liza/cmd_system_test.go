package main

import (
	"testing"
)

func TestWatchCmd_HeadlessFlag(t *testing.T) {
	flag := watchCmd.Flags().Lookup("headless")
	if flag == nil {
		t.Fatal("watchCmd missing --headless flag")
	}
	if flag.DefValue != "false" {
		t.Errorf("--headless default = %q, want %q", flag.DefValue, "false")
	}
	if flag.Usage == "" {
		t.Error("--headless flag has no usage text")
	}
}

func TestWatchCmd_IntervalFlag(t *testing.T) {
	flag := watchCmd.Flags().Lookup("interval")
	if flag == nil {
		t.Fatal("watchCmd missing --interval flag (needed for headless backward compatibility)")
	}
}

func TestWatchCmd_ShortDescription(t *testing.T) {
	want := "Interactive TUI dashboard for monitoring Liza"
	if watchCmd.Short != want {
		t.Errorf("watchCmd.Short = %q, want %q", watchCmd.Short, want)
	}
}
