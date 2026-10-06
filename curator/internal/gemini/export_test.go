package gemini

import "time"

// SetRetryDelay shortens the wait between retries for tests.
func (c *Client) SetRetryDelay(d time.Duration) { c.retryDelay = d }
