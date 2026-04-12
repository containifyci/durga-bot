# durga-bot

A GitHub App webhook server skeleton. Receives, validates, and acknowledges GitHub webhook events.

## Architecture

The service runs two HTTP servers:

- **Webhook server** (`:8080`) — receives `POST /webhooks/github`, validates HMAC signature, logs the event, returns 200.
- **Platform server** (`:8090`) — exposes `/health` (liveness probe) and `/metrics`.

## Getting Started

### Prerequisites

- Go 1.26+
- A registered [GitHub App](https://docs.github.com/en/apps/creating-github-apps)
