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
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
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
		// No fixed client Timeout: local grounded generation is slow and
		// varies by hardware, so the deadline is governed entirely by the
		// context the caller passes (the worker's per-job timeout). A fixed
		// client timeout here would just race that and fire early.
		http: &http.Client{},
	}
}

// NodeDraft is the AI's proposal for a single node. It intentionally carries
// only the fields the model can sensibly author on its own — the parent,
// visibility, pin, and tags are left for the human to complete in the form.
type NodeDraft struct {
	Type  string // "view" | "topic" | "finding"
	Title string
	Body  string
	// Evidence are proposed finding nodes derived from the grounding sources.
	// Each becomes a reusable finding linked to the main node if the user keeps
	// it in the confirm modal. Empty for ungrounded drafts.
	Evidence []EvidenceDraft
}

// EvidenceDraft is one proposed piece of evidence: a finding backed by a source
// the draft was grounded in.
type EvidenceDraft struct {
	Title     string
	Body      string
	SourceURL string
	Relation  string // "supports" | "opposes" | "related"
}

// allowedRelations are the edge kinds evidence may take toward the main node.
var allowedRelations = map[string]bool{"supports": true, "opposes": true, "related": true}

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
{"type": "view" | "topic" | "finding", "title": "<short title, max 200 characters>", "body": "<optional markdown elaboration, may be empty>", "evidence": [{"title": "<short title>", "body": "<1-2 sentences on what this source contributes>", "source_url": "<one of the provided source URLs>", "relation": "supports" | "opposes" | "related"}]}

Pick the single type that best fits. Keep the title concise and self-contained. Do not invent sources or links.

The "evidence" array is optional supporting material that will become separate, reusable evidence nodes. Only include an evidence item when web sources are provided to you, and each item's "source_url" MUST be exactly one of those provided source URLs — never invent a URL. Set "relation" to how that source relates to the node: "supports" if it backs the node, "opposes" if it argues against it, otherwise "related". If no sources are provided, use an empty array [].`

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
	return c.GenerateNodeGroundedStream(ctx, prompt, sources, nil)
}

// GenerateNodeGroundedStream is GenerateNodeGrounded with live progress: onToken
// is invoked with each content delta as it streams from the model (it may be
// called many times, and may be nil to ignore). The worker uses it to reset its
// idle watchdog and to write the draft-in-progress to the job row so the UI can
// show generation happening live. The returned draft is still produced by
// strictly parsing the full accumulated output, exactly as the non-streaming
// path — the streamed deltas are for display and liveness only.
func (c *Client) GenerateNodeGroundedStream(ctx context.Context, prompt string, sources []Source, onToken func(delta string)) (*NodeDraft, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return nil, errors.New("prompt is empty")
	}
	userContent := buildUserContent(prompt, sources)
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		content, err := c.completeStream(ctx, userContent, onToken)
		if err != nil {
			// Transport / HTTP / cancellation failure — retrying won't help and
			// the caller wants the real reason.
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


// newChatRequest builds the POST for one completion. stream selects SSE vs. a
// single JSON body; both share the same messages and options.
func (c *Client) newChatRequest(ctx context.Context, userContent string, stream bool) (*http.Request, error) {
	reqBody, err := json.Marshal(chatRequest{
		Model: c.model,
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userContent},
		},
		ResponseFormat: &responseFormat{Type: "json_object"},
		Temperature:    0.4,
		Stream:         stream,
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/chat/completions", bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	return req, nil
}

// streamChunk is one SSE frame from the chat-completions stream: the delta
// carries incremental content.
type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
}

// completeStream POSTs a streaming chat request, forwards each content delta to
// onToken (when non-nil), and returns the full accumulated assistant message.
// Reading the body is what advances the deadline: a stalled stream leaves the
// caller's context to fire, and cancellation surfaces here as a read error.
func (c *Client) completeStream(ctx context.Context, userContent string, onToken func(delta string)) (string, error) {
	req, err := c.newChatRequest(ctx, userContent, true)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("ai endpoint %s: %s", resp.Status, bytes.TrimSpace(msg))
	}

	var full strings.Builder
	sc := bufio.NewScanner(resp.Body)
	// SSE frames are line-delimited; a single delta is small, but raise the
	// buffer ceiling so an unusually large frame can't abort the scan.
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		var chunk streamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			// Tolerate a stray keep-alive / comment frame rather than failing
			// the whole generation on one unparsable line.
			continue
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		delta := chunk.Choices[0].Delta.Content
		if delta == "" {
			continue
		}
		full.WriteString(delta)
		if onToken != nil {
			onToken(delta)
		}
	}
	if err := sc.Err(); err != nil {
		// Distinguish a cancelled/expired context (idle watchdog or hard cap)
		// from a raw I/O error so the worker can report it meaningfully.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		return "", fmt.Errorf("read ai stream: %w", err)
	}
	if full.Len() == 0 {
		return "", errors.New("ai endpoint returned no content")
	}
	return full.String(), nil
}

// parseDraft turns the model's message content into a validated NodeDraft.
// Returns an error (retriable by the caller) when the content isn't the JSON
// object we asked for or names an unknown type.
func parseDraft(content string) (*NodeDraft, error) {
	raw := stripJSONFence(strings.TrimSpace(content))
	var out struct {
		Type     string `json:"type"`
		Title    string `json:"title"`
		Body     string `json:"body"`
		Evidence []struct {
			Title     string `json:"title"`
			Body      string `json:"body"`
			SourceURL string `json:"source_url"`
			Relation  string `json:"relation"`
		} `json:"evidence"`
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

	// Evidence is best-effort: skip malformed items rather than failing the
	// whole draft. The worker further restricts URLs to the sources actually
	// fetched, so a hallucinated link can't slip through as a citation.
	var evidence []EvidenceDraft
	for _, e := range out.Evidence {
		etitle := strings.TrimSpace(e.Title)
		eurl := strings.TrimSpace(e.SourceURL)
		if etitle == "" || eurl == "" {
			continue
		}
		if s := strings.ToLower(eurl); !strings.HasPrefix(s, "http://") && !strings.HasPrefix(s, "https://") {
			continue
		}
		if r := []rune(etitle); len(r) > maxTitleLen {
			etitle = strings.TrimSpace(string(r[:maxTitleLen]))
		}
		relation := strings.ToLower(strings.TrimSpace(e.Relation))
		if !allowedRelations[relation] {
			relation = "related"
		}
		evidence = append(evidence, EvidenceDraft{
			Title:     etitle,
			Body:      strings.TrimSpace(e.Body),
			SourceURL: eurl,
			Relation:  relation,
		})
	}

	return &NodeDraft{
		Type:     typ,
		Title:    title,
		Body:     strings.TrimSpace(out.Body),
		Evidence: evidence,
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
