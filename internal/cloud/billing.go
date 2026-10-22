package cloud

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"lara4ever/internal/i18n"
)

// Same loopback port used by Stripe success/cancel URLs configured server-side
// (STRIPE_SUCCESS_URL=http://127.0.0.1:33001/billing-success). Hardcoded so
// the server's .env and the desktop client agree without a discovery step.
const billingLoopbackPort = 33001

type checkoutResponse struct {
	URL string `json:"url"`
}

// StartCheckout creates a Stripe Checkout session via the server, opens the
// returned URL in the user's browser, and starts a tiny local HTTP listener
// to detect the return. Polls /me every 2s for up to 5 minutes after the
// browser is bounced back, picking up plan=pro once the webhook fires.
//
// Returns when:
//   - the user lands back on /billing-success and /me reports plan=pro, OR
//   - 10 minutes elapse, OR
//   - context is cancelled.
func (c *Client) StartCheckout(ctx context.Context) error {
	ctx, cancel, err := c.cloudContext(ctx)
	if err != nil {
		return err
	}
	defer cancel()
	if c.token() == "" {
		return ErrUnauthenticated
	}

	var resp checkoutResponse
	if err := c.post(ctx, "/api/v1/billing/checkout", nil, &resp); err != nil {
		return err
	}

	if err := c.runBillingFlow(ctx, resp.URL); err != nil {
		return err
	}
	return nil
}

// StartPayPalCheckout follows the same pattern as Stripe but with PayPal:
// server creates a Subscription, returns approve URL. PayPal redirects back
// to our 127.0.0.1:33001 loopback with ?subscription_id=... Client posts
// it to /billing/paypal/finalize so the server can verify status with PayPal
// and flip plan=pro (no webhook needed for activation).
func (c *Client) StartPayPalCheckout(ctx context.Context) error {
	ctx, cancel, err := c.cloudContext(ctx)
	if err != nil {
		return err
	}
	defer cancel()
	if c.token() == "" {
		return ErrUnauthenticated
	}

	var resp struct {
		URL            string `json:"url"`
		SubscriptionID string `json:"subscription_id"`
	}
	if err := c.post(ctx, "/api/v1/billing/paypal/checkout", nil, &resp); err != nil {
		return err
	}

	addr := fmt.Sprintf("127.0.0.1:%d", billingLoopbackPort)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		time.Sleep(500 * time.Millisecond)
		listener, err = net.Listen("tcp", addr)
		if err != nil {
			return i18n.Wrapf(err, "errors.cloud.bindAddressFailed", map[string]any{"address": addr})
		}
	}

	type result struct {
		kind string
		sub  string
	}
	doneCh := make(chan result, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/billing-success", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// PayPal puts the subscription id in ?subscription_id=
		sub := r.URL.Query().Get("subscription_id")
		if sub == "" {
			sub = resp.SubscriptionID
		}
		_, _ = w.Write(doneHTML("Subscription activated", "Welcome to LaraEnv Pro. You can close this tab and return to the app — sync will unlock automatically."))
		doneCh <- result{kind: "success", sub: sub}
	})
	mux.HandleFunc("/billing-cancel", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(doneHTML("Cancelled", "No subscription was created. You can close this tab."))
		doneCh <- result{kind: "cancel"}
	})
	server := &http.Server{Handler: mux, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second}
	go func() { _ = server.Serve(listener) }()
	defer func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(shutCtx)
	}()

	if err := openBrowser(resp.URL); err != nil {
		return i18n.Wrapf(err, "errors.cloud.openBrowserFailed", nil)
	}

	timeout := time.NewTimer(10 * time.Minute)
	defer timeout.Stop()

	var got result
	select {
	case got = <-doneCh:
	case <-timeout.C:
		return i18n.Errorf("errors.cloud.paypalTimeout", nil)
	case <-ctx.Done():
		return ctx.Err()
	}
	if got.kind == "cancel" {
		return nil
	}

	// Verify + persist subscription server-side.
	body := map[string]string{"subscription_id": got.sub}
	var fin struct {
		Plan   string `json:"plan"`
		Status string `json:"status"`
	}
	if err := c.post(ctx, "/api/v1/billing/paypal/finalize", body, &fin); err != nil {
		return err
	}
	// Refresh /me so cfg.Cloud.Plan flips to pro for the UI.
	_, _ = c.Me(ctx)
	return nil
}

// StartPortal mirrors StartCheckout but for the Customer Portal — used to
// cancel the subscription, update the card, etc. Same loopback return.
func (c *Client) StartPortal(ctx context.Context) error {
	ctx, cancel, err := c.cloudContext(ctx)
	if err != nil {
		return err
	}
	defer cancel()
	if c.token() == "" {
		return ErrUnauthenticated
	}

	var resp checkoutResponse
	if err := c.post(ctx, "/api/v1/billing/portal", nil, &resp); err != nil {
		return err
	}
	return c.runBillingFlow(ctx, resp.URL)
}

// runBillingFlow is the shared loopback dance: bind 33001, open the browser,
// wait for Stripe's redirect, then refresh /me.
func (c *Client) runBillingFlow(ctx context.Context, browserURL string) error {
	addr := fmt.Sprintf("127.0.0.1:%d", billingLoopbackPort)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		// Common cause: previous flow's listener still draining. Wait briefly
		// and try once more.
		time.Sleep(500 * time.Millisecond)
		listener, err = net.Listen("tcp", addr)
		if err != nil {
			return i18n.Wrapf(err, "errors.cloud.bindAddressFailed", map[string]any{"address": addr})
		}
	}

	doneCh := make(chan string, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/billing-success", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(doneHTML("Subscription activated", "Welcome to LaraEnv Pro. You can close this tab and return to the app — sync will unlock automatically."))
		doneCh <- "success"
	})
	mux.HandleFunc("/billing-cancel", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(doneHTML("Cancelled", "No charge was made. You can close this tab."))
		doneCh <- "cancel"
	})
	server := &http.Server{Handler: mux, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second}
	go func() { _ = server.Serve(listener) }()
	defer func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(shutCtx)
	}()

	if err := openBrowser(browserURL); err != nil {
		return i18n.Wrapf(err, "errors.cloud.openBrowserFailed", nil)
	}

	// Wait for the bounce-back, then poll /me until plan flips (the webhook
	// is async — usually within a couple seconds, but can take longer).
	timeout := time.NewTimer(10 * time.Minute)
	defer timeout.Stop()

	select {
	case kind := <-doneCh:
		if kind == "cancel" {
			return nil // user bailed; not an error
		}
	case <-timeout.C:
		return i18n.Errorf("errors.cloud.billingTimeout", nil)
	case <-ctx.Done():
		return ctx.Err()
	}

	// Poll /me up to 30s for the webhook to land. Quick path most of the time.
	pollDeadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(pollDeadline) {
		me, err := c.Me(ctx)
		if err == nil && me.Plan == "pro" {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	// We reach here if the webhook hasn't fired yet — UI can show a friendly
	// "We received your payment, syncing entitlement…" message and have the
	// user retry by clicking Refresh.
	return nil
}
