// Package slack is the shared Slack client. HTTP-only (no SDK dep), Block
// Kit JSON passthrough, message upsert via chat.update or chat.postMessage.
// Consumed by internal/notifications (PR flow) and
// internal/drift/sinks/slack.
package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client posts and edits Slack messages via the Web API.
type Client struct {
	httpClient *http.Client
	token      string
}

// New returns a Client. token is a bot user OAuth token ("xoxb-...").
func New(token string) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 15 * time.Second},
		token:      token,
	}
}

// Attachment wraps a colored sidebar attachment containing Block Kit blocks.
type Attachment struct {
	Color  string  `json:"color,omitempty"`
	Blocks []Block `json:"blocks,omitempty"`
}

// Message is the minimal post/update payload.
type Message struct {
	Channel     string       `json:"channel"`
	Text        string       `json:"text,omitempty"`        // fallback for notifications
	Blocks      []Block      `json:"blocks,omitempty"`      // top-level Block Kit (no color bar)
	Attachments []Attachment `json:"attachments,omitempty"` // colored sidebar attachments
	TS          string       `json:"ts,omitempty"`          // set on update
	ThreadTS    string       `json:"thread_ts,omitempty"`
}

// Block is opaque - users construct Block Kit JSON and stash it here.
type Block = json.RawMessage

// PostResult returns the TS of the posted message so the caller can
// persist it for later upserts.
type PostResult struct {
	TS      string `json:"ts"`
	Channel string `json:"channel"`
}

// Post publishes a new message. Returns the message TS.
func (c *Client) Post(ctx context.Context, m Message) (*PostResult, error) {
	return c.call(ctx, "chat.postMessage", m)
}

// Update edits an existing message identified by channel + ts.
func (c *Client) Update(ctx context.Context, m Message) (*PostResult, error) {
	if m.TS == "" {
		return nil, errors.New("slack update: ts required")
	}
	return c.call(ctx, "chat.update", m)
}

// Upsert posts or updates: if ts is empty, posts; otherwise updates.
func (c *Client) Upsert(ctx context.Context, channel, ts, text string, blocks []Block) (*PostResult, error) {
	m := Message{Channel: channel, Text: text, Blocks: blocks, TS: ts}
	if ts == "" {
		return c.Post(ctx, m)
	}
	return c.Update(ctx, m)
}

// PostThread publishes a reply in the thread of parentTS.
func (c *Client) PostThread(ctx context.Context, channel, parentTS, text string, blocks []Block) (*PostResult, error) {
	return c.Post(ctx, Message{Channel: channel, ThreadTS: parentTS, Text: text, Blocks: blocks})
}

func (c *Client) call(ctx context.Context, method string, m Message) (*PostResult, error) {
	body, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://slack.com/api/"+method, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("slack %s %d: %s", method, resp.StatusCode, string(data))
	}
	var out struct {
		OK      bool   `json:"ok"`
		TS      string `json:"ts"`
		Channel string `json:"channel"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	if !out.OK {
		return nil, fmt.Errorf("slack %s failed: %s", method, out.Error)
	}
	return &PostResult{TS: out.TS, Channel: out.Channel}, nil
}
