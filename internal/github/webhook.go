package github

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/containifyci/durga-bot/internal/token"
	gh "github.com/google/go-github/v67/github"
)

// Handler receives GitHub webhook events, validates the HMAC signature,
// and acknowledges them with HTTP 200.
type Handler struct {
	token         token.Client
	ghClient      *gh.Client
	logger        *slog.Logger
	webhookSecret []byte
}

// NewHandler creates a webhook Handler.
// tokenClient may be nil; in that case token creation is skipped.
func NewHandler(webhookSecret string, logger *slog.Logger, tokenClient token.Client, ghClient *gh.Client) *Handler {
	return &Handler{
		webhookSecret: []byte(webhookSecret),
		logger:        logger,
		token:         tokenClient,
		ghClient:      ghClient,
	}
}

// ServeHTTP handles incoming GitHub webhook requests.
//
//		@Summary       Receive GitHub webhook event
//		@Description   Validates HMAC-SHA256 signature and acknowledges the event.
//		@Tags          webhooks
//	 @x-criticality "low"
//		@Accept        json
//		@Produce       plain
//		@Success       200  {string}  string  "event received"
//		@Failure       401  {string}  string  "invalid signature"
//		@Router        /webhooks/github [post]
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MB limit

	payload, err := gh.ValidatePayload(r, h.webhookSecret)
	if err != nil {
		h.logger.Warn("invalid webhook signature",
			slog.String("error", err.Error()),
		)
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}

	eventType := gh.WebHookType(r)
	deliveryID := gh.DeliveryID(r)

	h.logger.Info("webhook event received",
		slog.String("event_type", eventType),
		slog.String("delivery_id", deliveryID),
		slog.Int("payload_size", len(payload)),
	)

	if h.token != nil {
		wp := extractWebhookPayload(payload)
		owner, repoName, ok := strings.Cut(wp.Repository.FullName, "/")
		if !ok {
			h.logger.Warn("skipping token creation: could not parse repo name",
				slog.String("full_name", wp.Repository.FullName),
			)
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, "event received")
			return
		}

		go func() { //nolint:contextcheck // intentionally detached from request context for fire-and-forget
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()

			serviceName, err := ResolveServiceName(ctx, h.ghClient, owner, repoName)
			if err != nil {
				h.logger.Error("failed to resolve service name",
					slog.String("repo", wp.Repository.FullName),
					slog.String("error", err.Error()),
				)
				return
			}

			req := token.TokenRequest{
				ServiceName: serviceName,
				RepoOwner:   owner,
				RepoName:    repoName,
				PRNumber:    wp.Number,
			}
			if err := h.token.CreateToken(ctx, req); err != nil {
				h.logger.Error("failed to create token",
					slog.String("service", serviceName),
					slog.String("repo", wp.Repository.FullName),
					slog.String("error", err.Error()),
				)
			}
		}()
	}

	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, "event received")
}

type webhookPayload struct {
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
	Number int `json:"number"`
}

func extractWebhookPayload(payload []byte) webhookPayload {
	var wp webhookPayload
	if err := json.Unmarshal(payload, &wp); err != nil {
		return webhookPayload{}
	}
	if wp.Repository.FullName == "" {
		wp.Repository.FullName = "unknown"
	}
	return wp
}
