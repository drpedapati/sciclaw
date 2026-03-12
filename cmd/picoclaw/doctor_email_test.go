package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sipeed/picoclaw/pkg/config"
)

func TestRunDoctorReportsEmailConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg := config.DefaultConfig()
	cfg.Channels.Email.Enabled = true
	cfg.Channels.Email.Provider = "resend"
	cfg.Channels.Email.APIKey = "test"
	cfg.Channels.Email.Address = "support@example.com"
	cfg.Channels.Email.BaseURL = "not a url"
	cfg.Channels.Email.ReceiveEnabled = true

	configPath := filepath.Join(home, ".picoclaw", "config.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := config.SaveConfig(configPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	rep := runDoctor(doctorOptions{})

	emailCheck, ok := doctorCheckByName(rep.Checks, "email")
	if !ok || emailCheck.Status != doctorOK {
		t.Fatalf("email check=%#v", emailCheck)
	}
	baseURLCheck, ok := doctorCheckByName(rep.Checks, "email.base_url")
	if !ok || baseURLCheck.Status != doctorWarn {
		t.Fatalf("email.base_url check=%#v", baseURLCheck)
	}
	receiveCheck, ok := doctorCheckByName(rep.Checks, "email.receive")
	if !ok || receiveCheck.Status != doctorWarn {
		t.Fatalf("email.receive check=%#v", receiveCheck)
	}
}
