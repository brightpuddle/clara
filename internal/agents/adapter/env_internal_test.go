package adapter

import "testing"

func TestManagedEnvStripsTmuxAndMarksManaged(t *testing.T) {
	got := managedEnv([]string{"PATH=/bin", "TMUX=/tmp/x,1,0", "TMUX_PANE=%1", "HOME=/h"})
	want := []string{"PATH=/bin", "HOME=/h", "CLARA_MANAGED_AGENT=1"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}
