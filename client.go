// SPDX-License-Identifier: BUSL-1.1

package zeusclient

// Options constructs a Client. Profile, secrets, and ports land in later tickets.
type Options struct{}

// Client is the public handle (Python ZeusRuntime).
// Agent, Zeus, and Config facades are ZCG-11.
type Client struct {
	rt *runtime
}

// New constructs a Client. Close the result. No process-global HTTP or auth.
func New(opts Options) (*Client, error) {
	return &Client{rt: newRuntime()}, nil
}

// Close releases resources. Idempotent and nil-safe.
func (c *Client) Close() error {
	if c == nil {
		return nil
	}
	return c.rt.close()
}
