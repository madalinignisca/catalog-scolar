// This file implements Web Push (VAPID) notification sending for CatalogRO.
//
// Web Push uses the VAPID (Voluntary Application Server Identification)
// protocol to authenticate push messages. The server holds a VAPID key pair;
// the public key is shared with the browser during subscription, and the
// private key is used to sign push messages.
//
// Configuration via environment variables:
//
//	VAPID_PUBLIC_KEY=<base64url-encoded public key>
//	VAPID_PRIVATE_KEY=<base64url-encoded private key>
//	VAPID_CONTACT=mailto:admin@catalogro.ro
//
// Generate keys with: go run github.com/nicholasgasior/gowpgen/cmd/gowpgen
// Or: npx web-push generate-vapid-keys
//
// If VAPID keys are not configured, push sending is disabled (logged only).
package notification

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	webpush "github.com/SherClockHolmes/webpush-go"
)

// VAPIDConfig holds the VAPID key pair for Web Push authentication.
type VAPIDConfig struct {
	PublicKey  string // base64url-encoded ECDSA P-256 public key
	PrivateKey string // base64url-encoded ECDSA P-256 private key
	Contact   string // mailto: or https: URL for push service to contact
}

// PushSender sends Web Push notifications using VAPID.
type PushSender struct {
	config VAPIDConfig
	logger *slog.Logger
}

// LoadVAPIDConfig reads VAPID settings from environment variables.
// Returns nil if VAPID_PUBLIC_KEY is not configured (push disabled).
func LoadVAPIDConfig() *VAPIDConfig {
	pubKey := os.Getenv("VAPID_PUBLIC_KEY")
	if pubKey == "" {
		return nil
	}

	privKey := os.Getenv("VAPID_PRIVATE_KEY")
	if privKey == "" {
		panic("FATAL: VAPID_PRIVATE_KEY must be set when VAPID_PUBLIC_KEY is configured")
	}

	contact := os.Getenv("VAPID_CONTACT")
	if contact == "" {
		contact = "mailto:admin@catalogro.ro"
	}

	return &VAPIDConfig{
		PublicKey:  pubKey,
		PrivateKey: privKey,
		Contact:   contact,
	}
}

// NewPushSender creates a Web Push sender with the given VAPID config.
// If config is nil, returns a no-op sender that only logs.
func NewPushSender(config *VAPIDConfig, logger *slog.Logger) *PushSender {
	sender := &PushSender{logger: logger}
	if config != nil {
		sender.config = *config
	}
	return sender
}

// IsEnabled returns true if VAPID keys are configured.
func (p *PushSender) IsEnabled() bool {
	return p.config.PublicKey != ""
}

// ErrSubscriptionExpired is returned when the push service indicates the
// subscription is no longer valid (HTTP 410 Gone or 404 Not Found).
// The caller should delete this subscription from the database.
var ErrSubscriptionExpired = errors.New("push subscription expired or invalid")

// pushTTL is how long (seconds) the push service holds the message if the
// device is offline. 24 hours is standard for school notifications — parents
// may not check their phone immediately.
const pushTTL = 86400

// PushPayload is the JSON payload sent to the browser's service worker.
// The service worker uses these fields to display the notification.
type PushPayload struct {
	Title string            `json:"title"`
	Body  string            `json:"body"`
	Icon  string            `json:"icon,omitempty"`
	URL   string            `json:"url,omitempty"`   // click action URL
	Data  map[string]string `json:"data,omitempty"` // extra data for the service worker
}

// SendPush sends a Web Push notification to a single subscription.
// The subscription fields (endpoint, p256dh key, auth key) come from the
// push_subscriptions table, stored when the user called /subscribe.
func (p *PushSender) SendPush(endpoint, p256dhKey, authKey string, payload PushPayload) error {
	if !p.IsEnabled() {
		p.logger.Debug("push sending disabled (VAPID not configured)",
			"endpoint", truncateEndpoint(endpoint))
		return nil
	}

	// Serialize the payload to JSON for the service worker.
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal push payload: %w", err)
	}

	// Build the Web Push subscription object.
	sub := &webpush.Subscription{
		Endpoint: endpoint,
		Keys: webpush.Keys{
			P256dh: p256dhKey,
			Auth:   authKey,
		},
	}

	// Send the push message using VAPID authentication.
	resp, err := webpush.SendNotification(payloadJSON, sub, &webpush.Options{
		Subscriber:      p.config.Contact,
		VAPIDPublicKey:  p.config.PublicKey,
		VAPIDPrivateKey: p.config.PrivateKey,
		TTL:             pushTTL,
	})
	if err != nil {
		return fmt.Errorf("send push to %s: %w", truncateEndpoint(endpoint), err)
	}
	defer resp.Body.Close()

	// Check for subscription expiry or invalidity.
	if resp.StatusCode == http.StatusGone || resp.StatusCode == http.StatusNotFound {
		// Subscription is no longer valid — the browser unsubscribed or
		// the push service removed it. Caller should delete from DB.
		return ErrSubscriptionExpired
	}

	if resp.StatusCode >= 400 {
		return fmt.Errorf("push service error (HTTP %d): %s", resp.StatusCode, truncateEndpoint(endpoint))
	}

	p.logger.Debug("push sent successfully", "endpoint", truncateEndpoint(endpoint))
	return nil
}

// truncateEndpoint shortens a push endpoint URL for logging.
func truncateEndpoint(endpoint string) string {
	if len(endpoint) > 60 {
		return endpoint[:60] + "..."
	}
	return endpoint
}
