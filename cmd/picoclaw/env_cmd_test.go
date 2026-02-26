package main

import "testing"

func TestTuiCmdRunsLocalAppTUI(t *testing.T) {
	orig := runAppTUI
	t.Cleanup(func() { runAppTUI = orig })

	called := false
	runAppTUI = func() { called = true }

	tuiCmd()

	if !called {
		t.Fatal("expected tuiCmd to invoke local app launcher")
	}
}

