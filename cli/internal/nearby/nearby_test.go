package nearby

import (
	"testing"

	"github.com/BioHazard786/Warpdrop/cli/internal/lan"
)

func TestMergeKeepsOneEntryPerNameAndPrefersBoth(t *testing.T) {
	lobby := []Device{{ID: "p1", Name: "Pixel"}, {ID: "p2", Name: "Web Chrome"}}
	peers := []lan.Peer{{ID: "x", Name: "pixel", Addrs: []string{"10.0.0.2:1"}}, {ID: "y", Name: "Laptop", Addrs: []string{"10.0.0.3:1"}}}
	got := Merge(lobby, peers)
	if len(got) != 3 {
		t.Fatalf("want 3 devices, got %+v", got)
	}
	by := map[string]Device{}
	for _, d := range got {
		by[d.Name] = d
	}
	if d := by["Pixel"]; d.ID != "p1" || d.LAN == nil || !d.InLobby() {
		t.Fatalf("pixel should be in both lists: %+v", d)
	}
	if d := by["Laptop"]; d.InLobby() || d.LAN == nil {
		t.Fatalf("laptop is LAN only: %+v", d)
	}
	if d := by["Web Chrome"]; d.LAN != nil {
		t.Fatalf("browser has no direct route: %+v", d)
	}
}
