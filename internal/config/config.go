package config

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	env "github.com/Netflix/go-env"
)

// ValidationErrors holds one or more configuration validation errors.
// It is returned by Load when one or more environment variables are missing or invalid.
type ValidationErrors []string

func (e ValidationErrors) Error() string {
	return "invalid configuration: " + strings.Join(e, "; ")
}

// logLevel wraps slog.Level with env.Unmarshaler support.
type logLevel struct{ slog.Level }

func (l *logLevel) UnmarshalEnvironmentValue(data string) error {
	switch strings.ToUpper(data) {
	case "", "INFO":
		l.Level = slog.LevelInfo
	case "DEBUG":
		l.Level = slog.LevelDebug
	case "WARN":
		l.Level = slog.LevelWarn
	case "ERROR":
		l.Level = slog.LevelError
	default:
		return fmt.Errorf("LOG_LEVEL must be DEBUG, INFO, WARN, or ERROR; got %q", data)
	}
	return nil
}

// pemBytes is a []byte that strips leading/trailing whitespace on unmarshaling.
type pemBytes []byte

func (p *pemBytes) UnmarshalEnvironmentValue(data string) error {
	*p = pemBytes(strings.TrimSpace(data))
	return nil
}

// rawConfig is the go-env unmarshaling target.
type rawConfig struct {
	Port                string   `env:"PORT,default=8080"`
	GitHubWebhookSecret string   `env:"GITHUB_WEBHOOK_SECRET"`
	SecretOperatorHost  string   `env:"SECRET_OPERATOR_HOST,default=http://localhost:9999"`
	GitHubVariableName  string   `env:"GITHUB_VARIABLE_NAME,default=SECRET_OPERATOR_TOKENS"`
	GitHubPrivateKey    pemBytes `env:"GITHUB_PRIVATE_KEY"`
	LogLevel            logLevel `env:"LOG_LEVEL,default=INFO"`
	GitHubAppID         int64    `env:"GITHUB_APP_ID"`
	GitHubInstallID     int64    `env:"GITHUB_INSTALLATION_ID"`
}

// Config holds all service configuration loaded from environment variables.
type Config struct {
	Port                string
	GitHubWebhookSecret string
	SecretOperatorHost  string
	GitHubVariableName  string
	GitHubPrivateKey    []byte
	GitHubInstallID     int64
	GitHubAppID         int64
	LogLevel            slog.Level
}

// Load reads environment variables and returns a validated Config.
func Load() (*Config, error) {
	// Pre-validate required fields, collecting all missing-variable errors at once.
	required := []string{
		"GITHUB_APP_ID",
		"GITHUB_INSTALLATION_ID",
		"GITHUB_PRIVATE_KEY",
		"GITHUB_WEBHOOK_SECRET",
	}
	var errs ValidationErrors
	for _, key := range required {
		if strings.TrimSpace(os.Getenv(key)) == "" {
			errs = append(errs, fmt.Sprintf("required environment variable %s is not set", key))
		}
	}
	if len(errs) > 0 {
		return &Config{}, errs
	}

	var raw rawConfig
	if _, err := env.UnmarshalFromEnviron(&raw); err != nil {
		return &Config{}, ValidationErrors{err.Error()}
	}

	cfg := &Config{
		Port:                raw.Port,
		GitHubAppID:         raw.GitHubAppID,
		GitHubInstallID:     raw.GitHubInstallID,
		GitHubPrivateKey:    []byte(raw.GitHubPrivateKey),
		GitHubWebhookSecret: strings.TrimSpace(raw.GitHubWebhookSecret),
		LogLevel:            raw.LogLevel.Level,
		SecretOperatorHost:  raw.SecretOperatorHost,
		GitHubVariableName: raw.GitHubVariableName,
	}

	return cfg, nil
}
