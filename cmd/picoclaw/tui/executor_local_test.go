package tui

import "testing"

func TestParseServiceStatusFlag(t *testing.T) {
	out := `
Gateway service status:
  Backend:   launchd
  Installed: yes
  Running:   yes
  Enabled:   yes
`
	if v, ok := parseServiceStatusFlag(out, "installed"); !ok || !v {
		t.Fatalf("installed parse = (%v,%v), want (true,true)", v, ok)
	}
	if v, ok := parseServiceStatusFlag(out, "running"); !ok || !v {
		t.Fatalf("running parse = (%v,%v), want (true,true)", v, ok)
	}
}

func TestParseServiceStatusFlag_No(t *testing.T) {
	out := `
Gateway service status:
  Installed: no
  Running:   no
`
	if v, ok := parseServiceStatusFlag(out, "installed"); !ok || v {
		t.Fatalf("installed parse = (%v,%v), want (false,true)", v, ok)
	}
	if v, ok := parseServiceStatusFlag(out, "running"); !ok || v {
		t.Fatalf("running parse = (%v,%v), want (false,true)", v, ok)
	}
}

