package cloud

import "context"

// Quote is a motivational phrase shown on the Dashboard.
type Quote struct {
	Text   string `json:"text"`
	Author string `json:"author"`
}

// RandomQuote returns a random active quote from the server. Public endpoint —
// works without authentication. Returns nil when no quotes are configured
// (HTTP 404) so callers can fall back gracefully to a hardcoded phrase.
func (c *Client) RandomQuote(ctx context.Context) (*Quote, error) {
	var q Quote
	if err := c.get(ctx, "/api/v1/quotes/random", &q); err != nil {
		if apiErr, ok := err.(*APIError); ok && apiErr.Status == 404 {
			return nil, nil
		}
		return nil, err
	}
	if q.Text == "" {
		return nil, nil
	}
	return &q, nil
}
