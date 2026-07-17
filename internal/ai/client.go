// Package ai talks to an OpenAI-compatible chat-completions endpoint to
// draft a node from a natural-language prompt. It is deliberately
// provider-agnostic: the same wire format is spoken by Ollama, vLLM,
// llama.cpp, LM Studio, OpenRouter, and the hosted providers, so switching
// backends is a matter of changing the base URL (and maybe an API key).
//
// The client never writes to the graph. It returns a NodeDraft that the
// caller pre-fills into the normal post form, where a human reviews and
// submits it through the usual create path.
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client is a thin OpenAI-compatible chat client. Zero value is not usable;
// construct with NewClient.
type Client struct {
	baseURL string
	model   string
	apiKey  string
	http    *http.Client
}

// NewClient returns a client for the given endpoint. baseURL is the API root
// (e.g. "http://127.0.0.1:11434/v1"); a trailing slash is tolerated. apiKey
// may be empty for local runtimes that don't authenticate.
func NewClient(baseURL, model, apiKey string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		apiKey:  apiKey,
		http: &http.Client{
			// Local generation on CPU/Metal can take many seconds for the
			// first token; give it comfortable headroom like the TTS client.
			Timeout: 2 * time.Minute,
		},
	}
}

// NodeDraft is the AI's proposal for a single node. It intentionally carries
// only the fields the model can sensibly author on its own — the parent,
// visibility, pin, and tags are left for the human to complete in the form.
type NodeDraft struct {
	Type  string // "view" | "topic" | "finding"
	Title string
	Body  string
}

// allowedTypes are the node types the model may pick, matching db.NodeType
// values. Kept as a local set so this package stays free of a db dependency.
var allowedTypes = map[string]bool{"view": true, "topic": true, "finding": true}

const maxTitleLen = 200

// systemPrompt teaches the model this app's vocabulary and pins the output
// to a strict JSON shape. The three types mirror the post form's toggle.
const systemPrompt = `You help draft a single node for a discussion platform that organises ideas as a graph.

There are exactly three node types:
- "view": an opinion — a single clear stance someone could support or oppose.
- "topic": a subject that groups opinions together (usually phrased as a question or theme).
- "finding": evidence — a concrete observation, fact, or citation that attaches to another node.

Given the user's request, produce ONE node. Reply with ONLY a JSON object, no prose, no markdown fences, in exactly this shape:
{"type": "view" | "topic" | "finding", "title": "<short title, max 200 characters>", "body": "<optional markdown elaboration, may be empty>"}

Pick the single type that best fits. Keep the title concise and self-contained. Do not invent sources or links.`

// GenerateNode asks the model for a node draft from a prompt alone (no web
// grounding).
func (c *Client) GenerateNode(ctx context.Context, prompt string) (*NodeDraft, error) {
	return c.GenerateNodeGrounded(ctx, prompt, nil)
}

// GenerateNodeGrounded drafts a node, optionally grounded in fetched web
// sources. When sources are present the model is told to rely on them and not
// invent facts. Transport and HTTP errors are returned immediately (and
// verbatim, for diagnostics); a response that fails to parse or validate is
// retried once, since local models occasionally emit stray tokens around the
// JSON.
func (c *Client) GenerateNodeGrounded(ctx context.Context, prompt string, sources []Source) (*NodeDraft, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return nil, errors.New("prompt is empty")
	}
	userContent := buildUserContent(prompt, sources)
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		content, err := c.complete(ctx, userContent)
		if err != nil {
			// Transport / HTTP failure — retrying won't help and the caller
			// wants the real reason.
			return nil, err
		}
		draft, err := parseDraft(content)
		if err != nil {
			lastErr = err
			continue
		}
		return draft, nil
	}
	return nil, fmt.Errorf("model did not return a valid node draft: %w", lastErr)
}

// buildUserContent assembles the user message: the prompt, plus the fetched
// source text with a grounding instruction when any sources are present.
func buildUserContent(prompt string, sources []Source) string {
	if len(sources) == 0 {
		return prompt
	}
	var b strings.Builder
	b.WriteString(prompt)
	b.WriteString("\n\nGround the node in the following web sources. Use only claims supported by them and do not invent facts; if they conflict or are thin, say so plainly in the body.\n")
	for i, s := range sources {
		fmt.Fprintf(&b, "\n[Source %d] %s (%s)\n%s\n", i+1, s.Title, s.URL, s.Text)
	}
	return b.String()
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type responseFormat struct {
	Type string `json:"type"`
}

type chatRequest struct {
	Model          string          `json:"model"`
	Messages       []chatMessage   `json:"messages"`
	ResponseFormat *responseFormat `json:"response_format,omitempty"`
	Temperature    float64         `json:"temperature"`
	Stream         bool            `json:"stream"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
}

// complete POSTs one chat request and returns the assistant message content.
func (c *Client) complete(ctx context.Context, userContent string) (string, error) {
	reqBody, err := json.Marshal(chatRequest{
		Model: c.model,
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userContent},
		},
		ResponseFormat: &responseFormat{Type: "json_object"},
		Temperature:    0.4,
		Stream:         false,
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/chat/completions", bytes.NewReader(reqBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("ai endpoint %s: %s", resp.Status, bytes.TrimSpace(msg))
	}

	var parsed chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", fmt.Errorf("decode ai response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return "", errors.New("ai endpoint returned no choices")
	}
	return parsed.Choices[0].Message.Content, nil
}

// parseDraft turns the model's message content into a validated NodeDraft.
// Returns an error (retriable by the caller) when the content isn't the JSON
// object we asked for or names an unknown type.
func parseDraft(content string) (*NodeDraft, error) {
	raw := stripJSONFence(strings.TrimSpace(content))
	var out struct {
		Type  string `json:"type"`
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, fmt.Errorf("content is not JSON: %w", err)
	}
	typ := strings.ToLower(strings.TrimSpace(out.Type))
	if !allowedTypes[typ] {
		return nil, fmt.Errorf("unknown node type %q", out.Type)
	}
	title := strings.TrimSpace(out.Title)
	if title == "" {
		return nil, errors.New("missing title")
	}
	if r := []rune(title); len(r) > maxTitleLen {
		// The form caps titles at 200 too; truncate rather than fail so a
		// slightly-long title doesn't cost the user a whole round-trip.
		title = strings.TrimSpace(string(r[:maxTitleLen]))
	}
	return &NodeDraft{
		Type:  typ,
		Title: title,
		Body:  strings.TrimSpace(out.Body),
	}, nil
}

// stripJSONFence removes a ```json ... ``` (or bare ``` ... ```) wrapper that
// some models emit even when asked for raw JSON. Leaves untouched anything
// that isn't fenced.
func stripJSONFence(s string) string {
	if !strings.HasPrefix(s, "```") {
		return s
	}
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimPrefix(s, "json")
	s = strings.TrimPrefix(s, "JSON")
	if i := strings.LastIndex(s, "```"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}
