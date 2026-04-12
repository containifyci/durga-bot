package github

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"time"

	gh "github.com/google/go-github/v67/github"
)

// TokenClient creates tokens for services.
type TokenClient interface {
	CreateToken(ctx context.Context, service string) error
}

// Handler receives GitHub webhook events, validates the HMAC signature,
// and acknowledges them with HTTP 200.
type Handler struct {
	token         TokenClient
	logger        *slog.Logger
	webhookSecret []byte
}

// NewHandler creates a webhook Handler.
// tokenClient may be nil; in that case token creation is skipped.
func NewHandler(webhookSecret string, logger *slog.Logger, tokenClient TokenClient) *Handler {
	return &Handler{
		webhookSecret: []byte(webhookSecret),
		logger:        logger,
		token:         tokenClient,
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
		repoName := extractRepoName(payload)
		name := eventType + ":" + repoName
		
		go func() { //nolint:contextcheck // intentionally detached from request context for fire-and-forget
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := h.token.CreateToken(ctx, name); err != nil {
				h.logger.Error("failed to create token",
					slog.String("service", name),
					slog.String("error", err.Error()),
				)
			}
		}()
	}

	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, "event received")
}

type repoPayload struct {
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
}

func extractRepoName(payload []byte) string {
	var rp repoPayload
	if err := json.Unmarshal(payload, &rp); err != nil || rp.Repository.FullName == "" {
		return "unknown"
	}
	return rp.Repository.FullName
}
