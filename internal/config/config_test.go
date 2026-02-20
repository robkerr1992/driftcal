package config

import (
	"testing"
)

func TestLoad_RequiresAPIKey(t *testing.T) {
	t.Setenv("DRIFTCAL_API_KEY", "")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error when DRIFTCAL_API_KEY is empty")
	}
}

func TestLoad_DefaultValues(t *testing.T) {
	t.Setenv("DRIFTCAL_API_KEY", "test-key")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	checks := []struct {
		field string
		got   string
		want  string
	}{
		{"Port", cfg.Port, "8080"},
		{"Host", cfg.Host, "0.0.0.0"},
		{"DBPath", cfg.DBPath, "./driftcal.db"},
		{"LogLevel", cfg.LogLevel, "info"},
		{"LogFormat", cfg.LogFormat, "console"},
	}

	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.field, c.got, c.want)
		}
	}
}

func TestLoad_CustomValues(t *testing.T) {
	envs := map[string]string{
		"DRIFTCAL_API_KEY":    "custom-key",
		"DRIFTCAL_DB_PATH":   "/tmp/test.db",
		"DRIFTCAL_HOST":      "127.0.0.1",
		"DRIFTCAL_PORT":      "3000",
		"DRIFTCAL_LOG_LEVEL": "debug",
		"DRIFTCAL_LOG_FORMAT": "json",
	}
	for k, v := range envs {
		t.Setenv(k, v)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.APIKey != "custom-key" {
		t.Errorf("APIKey = %q, want %q", cfg.APIKey, "custom-key")
	}
	if cfg.Port != "3000" {
		t.Errorf("Port = %q, want %q", cfg.Port, "3000")
	}
	if cfg.LogFormat != "json" {
		t.Errorf("LogFormat = %q, want %q", cfg.LogFormat, "json")
	}
}

func TestLoad_InvalidPort(t *testing.T) {
	t.Setenv("DRIFTCAL_API_KEY", "test-key")
	t.Setenv("DRIFTCAL_PORT", "abc")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for non-numeric port")
	}
}

func TestConfig_Addr(t *testing.T) {
	cfg := &Config{Host: "localhost", Port: "9090"}
	got := cfg.Addr()
	want := "localhost:9090"
	if got != want {
		t.Errorf("Addr() = %q, want %q", got, want)
	}
}
