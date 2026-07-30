package main

import (
	"testing"

	"commander/internal/panelstate"
)

func TestTabLabelUsesPathByDefault(t *testing.T) {
	state := panelstate.New("/Users/allan/Downloads")
	if got, want := tabLabel(state), "Downloads"; got != want {
		t.Fatalf("tabLabel = %q, want %q", got, want)
	}
}

func TestTabLabelPrefersTabTitleOverride(t *testing.T) {
	state := panelstate.New("sftp://allan@192.168.1.104:2211/")
	state.TabTitle = "solaris114lab2"
	if got, want := tabLabel(state), "solaris114lab2"; got != want {
		t.Fatalf("tabLabel = %q, want %q (the connection's friendly name, not the presented path's last component)", got, want)
	}
}

func TestTabLabelLockIconStillAppliesWithTabTitleOverride(t *testing.T) {
	state := panelstate.New("sftp://allan@192.168.1.104:2211/")
	state.TabTitle = "solaris114lab2"
	state.Lock(true)
	if got, want := tabLabel(state), "🔒 solaris114lab2"; got != want {
		t.Fatalf("tabLabel = %q, want %q", got, want)
	}
}
