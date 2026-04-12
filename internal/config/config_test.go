package config

import (
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func setEnv(t *testing.T, kvs map[string]string) {
	t.Helper()
	for k, v := range kvs {
		t.Setenv(k, v)
	}
}

func validEnv() map[string]string {
	return map[string]string{
		"GITHUB_APP_ID":          "123",
		"GITHUB_INSTALLATION_ID": "456",
		"GITHUB_PRIVATE_KEY":     "fake-pem-content",
		"GITHUB_WEBHOOK_SECRET":  "secret",
	}
}

func TestLoad_MissingRequired(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	required := []string{
		"GITHUB_APP_ID",
		"GITHUB_INSTALLATION_ID",
		"GITHUB_PRIVATE_KEY",
		"GITHUB_WEBHOOK_SECRET",
	}
	for _, key := range required { //nolint:paralleltest // uses t.Setenv
		t.Run("missing_"+key, func(t *testing.T) {
			env := validEnv()
			delete(env, key)
			setEnv(t, env)
			os.Unsetenv(key)

			_, err := Load()
			if err == nil {
				t.Errorf("expected error when %s is missing, got nil", key)
			}
		})
	}
}

func TestLoad_WhitespaceOnlyRequired(t *testing.T) {
	required := []string{
		"GITHUB_APP_ID",
		"GITHUB_INSTALLATION_ID",
		"GITHUB_PRIVATE_KEY",
		"GITHUB_WEBHOOK_SECRET",
	}
	for _, key := range required {
		t.Run("whitespace_"+key, func(t *testing.T) {
			setEnv(t, validEnv())
			t.Setenv(key, "   ")

			_, err := Load()
			if err == nil {
				t.Errorf("expected error when %s is whitespace-only, got nil", key)
			}
		})
	}
}

func TestLoad_InvalidAppID(t *testing.T) {
	setEnv(t, validEnv())
	t.Setenv("GITHUB_APP_ID", "not-a-number")

	_, err := Load()
	if err == nil {
		t.Error("expected error for non-numeric GITHUB_APP_ID")
	}
}

func TestLoad_InvalidInstallID(t *testing.T) {
	setEnv(t, validEnv())
	t.Setenv("GITHUB_INSTALLATION_ID", "not-a-number")

	_, err := Load()
	if err == nil {
		t.Error("expected error for non-numeric GITHUB_INSTALLATION_ID")
	}
}

func TestLoad_Defaults(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	setEnv(t, validEnv())
	os.Unsetenv("PORT")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != "8080" {
		t.Errorf("expected default PORT=8080, got %s", cfg.Port)
	}
}

func TestLoad_AllFields(t *testing.T) {
	setEnv(t, validEnv())
	t.Setenv("PORT", "9090")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != "9090" {
		t.Errorf("expected Port=9090, got %s", cfg.Port)
	}
	if cfg.GitHubAppID != 123 {
		t.Errorf("expected GitHubAppID=123, got %d", cfg.GitHubAppID)
	}
	if cfg.GitHubInstallID != 456 {
		t.Errorf("expected GitHubInstallID=456, got %d", cfg.GitHubInstallID)
	}
	if len(cfg.GitHubPrivateKey) == 0 {
		t.Error("expected GitHubPrivateKey to be populated")
	}
}

func TestLoad_LogLevel_Default(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	setEnv(t, validEnv())
	os.Unsetenv("LOG_LEVEL")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.LogLevel != slog.LevelInfo {
		t.Errorf("expected default LogLevel=INFO, got %v", cfg.LogLevel)
	}
}

func TestLoad_LogLevel_Valid(t *testing.T) {
	tests := []struct {
		input string
		want  slog.Level
	}{
		{"DEBUG", slog.LevelDebug},
		{"debug", slog.LevelDebug},
		{"INFO", slog.LevelInfo},
		{"WARN", slog.LevelWarn},
		{"warn", slog.LevelWarn},
		{"ERROR", slog.LevelError},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			setEnv(t, validEnv())
			t.Setenv("LOG_LEVEL", tc.input)
			cfg, err := Load()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cfg.LogLevel != tc.want {
				t.Errorf("want %v, got %v", tc.want, cfg.LogLevel)
			}
		})
	}
}

func TestLoad_LogLevel_Invalid(t *testing.T) {
	setEnv(t, validEnv())
	t.Setenv("LOG_LEVEL", "VERBOSE")

	_, err := Load()
	if err == nil {
		t.Error("expected error for invalid LOG_LEVEL")
	}
}

func TestLoad_PrivateKeyTrimmed(t *testing.T) {
	setEnv(t, validEnv())
	t.Setenv("GITHUB_PRIVATE_KEY", "test\n")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.GitHubPrivateKey) == 0 {
		t.Fatal("expected non-empty private key")
	}
	if cfg.GitHubPrivateKey[0] == ' ' || cfg.GitHubPrivateKey[0] == '\n' {
		t.Error("expected private key to be trimmed")
	}
}

func TestLoad_WebhookSecretTrimmed(t *testing.T) {
	setEnv(t, validEnv())
	t.Setenv("GITHUB_WEBHOOK_SECRET", "  secret123  ")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.GitHubWebhookSecret != "secret123" {
		t.Errorf("expected trimmed secret 'secret123', got %q", cfg.GitHubWebhookSecret)
	}
}

func TestValidationErrors_Error(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		expect string
		errs   ValidationErrors
	}{
		{
			name:   "single error",
			errs:   ValidationErrors{"missing FOO"},
			expect: "invalid configuration: missing FOO",
		},
		{
			name:   "multiple errors",
			errs:   ValidationErrors{"missing FOO", "missing BAR"},
			expect: "invalid configuration: missing FOO; missing BAR",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.EqualError(t, tc.errs, tc.expect)
		})
	}
}
