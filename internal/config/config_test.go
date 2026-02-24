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

	checks := []struct {
		field string
		got   string
		want  string
	}{
		{"APIKey", cfg.APIKey, "custom-key"},
		{"DBPath", cfg.DBPath, "/tmp/test.db"},
		{"Host", cfg.Host, "127.0.0.1"},
		{"Port", cfg.Port, "3000"},
		{"LogLevel", cfg.LogLevel, "debug"},
		{"LogFormat", cfg.LogFormat, "json"},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.field, c.got, c.want)
		}
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

func TestLoad_InvalidPortRange(t *testing.T) {
	cases := []struct {
		name string
		port string
	}{
		{"zero", "0"},
		{"too high", "99999"},
		{"negative", "-1"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("DRIFTCAL_API_KEY", "test-key")
			t.Setenv("DRIFTCAL_PORT", tc.port)

			_, err := Load()
			if err == nil {
				t.Fatalf("expected error for port %q", tc.port)
			}
		})
	}
}

func TestLoad_InvalidTelegramUserID(t *testing.T) {
	t.Setenv("DRIFTCAL_API_KEY", "test-key")
	t.Setenv("TELEGRAM_AUTHORIZED_USER_ID", "not-a-number")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for non-numeric Telegram user ID")
	}
}

func TestLoad_ValidTelegramUserID(t *testing.T) {
	t.Setenv("DRIFTCAL_API_KEY", "test-key")
	t.Setenv("TELEGRAM_AUTHORIZED_USER_ID", "123456789")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.TelegramAuthorizedUser != 123456789 {
		t.Errorf("TelegramAuthorizedUser = %d, want %d", cfg.TelegramAuthorizedUser, 123456789)
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

func TestLoad_BaseURLValidation(t *testing.T) {
	cases := []struct {
		name    string
		url     string
		wantErr bool
		wantURL string
	}{
		{"valid https", "https://driftcal.example.com", false, "https://driftcal.example.com"},
		{"strips trailing slash", "https://driftcal.example.com/", false, "https://driftcal.example.com"},
		{"http rejected", "http://driftcal.example.com", true, ""},
		{"http localhost allowed", "http://localhost:8080", false, "http://localhost:8080"},
		{"http 127.0.0.1 allowed", "http://127.0.0.1:8080", false, "http://127.0.0.1:8080"},
		{"empty is ok", "", false, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("DRIFTCAL_API_KEY", "test-key")
			t.Setenv("DRIFTCAL_BASE_URL", tc.url)

			cfg, err := Load()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for URL %q", tc.url)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for URL %q: %v", tc.url, err)
			}
			if cfg.BaseURL != tc.wantURL {
				t.Errorf("BaseURL = %q, want %q", cfg.BaseURL, tc.wantURL)
			}
		})
	}
}
