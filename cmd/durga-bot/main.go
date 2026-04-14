package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/containifyci/durga-bot/internal/config"
	githubinternal "github.com/containifyci/durga-bot/internal/github"
	"github.com/containifyci/durga-bot/internal/server"
	"github.com/containifyci/durga-bot/internal/token"

	gh "github.com/google/go-github/v67/github"
)

type app struct {
	newTokenCli func(ghClient *gh.Client, secretOperatorHost, variableName string, logger *slog.Logger) token.Client
}

// @title           Son of Anton GitHub App
// @version         1.0
// @description     GitHub App webhook server skeleton. Receives and validates GitHub webhook events.
// @contact.name    Flink Platform Team
// @host            localhost:8080
// @schemes         http
// @BasePath        /
// @tag.name        webhooks
// @tag.description GitHub webhook event handlers
func main() {
	a := app{
		newTokenCli: func(ghClient *gh.Client, secretOperatorHost, variableName string, logger *slog.Logger) token.Client {
			return token.NewSecretOperatorClient(ghClient, secretOperatorHost, variableName, logger)
		},
	}
	os.Exit(a.appMain())
}

func (a *app) appMain() int {
	runErrCh := make(chan error, 1)
	go func() { runErrCh <- a.run() }()

	err := <-runErrCh

	if err != nil {
		return 1
	}
	return 0
}

func (a *app) run() error {
	bootstrap := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	cfg, err := config.Load()
	if err != nil {
		bootstrap.Error("failed to load configuration", slog.String("error", err.Error()))
		return fmt.Errorf("loading config: %w", err)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: cfg.LogLevel,
	}))

	logger.Info("configuration loaded",
		slog.String("log_level", cfg.LogLevel.String()),
	)

	if pk := string(cfg.GitHubPrivateKey); len(pk) >= 10 {
		logger.Debug("private key loaded",
			slog.Int("size_bytes", len(pk)),
			slog.String("snippet", pk[:5]+"..."+pk[len(pk)-5:]),
		)
	}

	ghClient, err := githubinternal.NewInstallationClient(
		cfg.GitHubAppID,
		cfg.GitHubInstallID,
		cfg.GitHubPrivateKey,
	)
	if err != nil {
		logger.Error("failed to create GitHub client", slog.String("error", err.Error()))
		return fmt.Errorf("creating GitHub client: %w", err)
	}

	tokenCli := a.newTokenCli(ghClient, cfg.SecretOperatorHost, cfg.GitHubVariableName, logger)

	webhookHandler := githubinternal.NewHandler(
		cfg.GitHubWebhookSecret,
		logger,
		tokenCli,
		ghClient,
	)

	mux := server.NewMux(webhookHandler)
	srv := server.New(mux, cfg.Port, logger)

	if err := srv.Run(); err != nil {
		logger.Error("server exited with error", slog.String("error", err.Error()))
		return fmt.Errorf("running server: %w", err)
	}
	return nil
}
