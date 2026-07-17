package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"

	"golang.org/x/net/html"
)

// Tuning for the retrieval step. Kept as constants (not config) for v1: a
// local model has limited context, and unbounded fetching is a DoS/SSRF
// surface. Lower = safer and faster.
const (
	maxSources       = 4               // total pages fed to the model
	maxFetchBytes    = 2 << 20         // 2 MiB per page, enforced while reading
	fetchTimeout     = 15 * time.Second
	maxRedirects     = 3
	perSourceCharCap = 4000  // trim each page's extracted text to this
	totalCharBudget  = 12000 // stop gathering once the injected text hits this
)

// Source is one web document the draft will be grounded in.
type Source struct {
	URL   string
	Title string
	Text  string
}

// Gatherer collects web sources for a draft: user-supplied URLs plus, when
// enabled, SearXNG search results. Fetching is SSRF-guarded (see Fetcher).
type Gatherer struct {
	search  *SearchClient // nil when SEARXNG_URL is unset
	fetcher *Fetcher
	logger  *slog.Logger
}

// NewGatherer wires a gatherer. searxngURL may be empty, which disables the
// search step (URL-grounding still works).
func NewGatherer(searxngURL string, logger *slog.Logger) *Gatherer {
	if logger == nil {
		logger = slog.Default()
	}
	g := &Gatherer{fetcher: NewFetcher(), logger: logger}
	if searxngURL != "" {
		g.search = NewSearchClient(searxngURL)
	}
	return g
}

// SearchAvailable reports whether web search is configured.
func (g *Gatherer) SearchAvailable() bool { return g.search != nil }

// Gather fetches the user's URLs (strict — a bad user URL fails the whole
// request so the person gets a clear error) and, when useSearch is set and
// search is configured, appends SearXNG results (best-effort — a blocked or
// dead result is skipped). Returns the sources within the char budget; the
// slice may be empty, in which case the caller generates ungrounded.
func (g *Gatherer) Gather(ctx context.Context, prompt string, urls []string, useSearch bool) ([]Source, error) {
	var sources []Source
	budget := totalCharBudget

	add := func(s Source) {
		if budget <= 0 || len(sources) >= maxSources {
			return
		}
		if len(s.Text) > budget {
			s.Text = s.Text[:budget]
		}
		budget -= len(s.Text)
		sources = append(sources, s)
	}

	seen := map[string]bool{}
	for _, raw := range urls {
		raw = strings.TrimSpace(raw)
		if raw == "" || seen[raw] {
			continue
		}
		seen[raw] = true
		src, err := g.fetcher.Fetch(ctx, raw)
		if err != nil {
			// User explicitly asked for this URL — surface the failure.
			return nil, fmt.Errorf("could not read %s: %w", raw, err)
		}
		add(src)
	}

	if useSearch && g.search != nil && budget > 0 && len(sources) < maxSources {
		hits, err := g.search.Search(ctx, prompt, maxSources)
		if err != nil {
			// Search backend down shouldn't discard user-URL grounding.
			g.logger.Warn("ai research: search failed", "err", err)
		}
		for _, h := range hits {
			if budget <= 0 || len(sources) >= maxSources {
				break
			}
			if seen[h.URL] {
				continue
			}
			seen[h.URL] = true
			src, err := g.fetcher.Fetch(ctx, h.URL)
			if err != nil {
				g.logger.Warn("ai research: skip search result", "url", h.URL, "err", err)
				continue
			}
			if src.Title == "" {
				src.Title = h.Title
			}
			add(src)
		}
	}
	return sources, nil
}

// Fetcher retrieves and text-extracts a single web page behind an SSRF guard.
type Fetcher struct {
	http *http.Client
}

// NewFetcher builds a client whose dialer rejects any connection to a private,
// loopback, link-local (incl. 169.254.169.254 cloud metadata), unspecified, or
// multicast address. The check runs at connect time on the *resolved* IP, so a
// hostname that resolves to an internal address — including via DNS-rebinding —
// is blocked too.
func NewFetcher() *Fetcher {
	dialer := &net.Dialer{
		Timeout: 10 * time.Second,
		Control: func(network, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip := net.ParseIP(host)
			if ip == nil {
				return fmt.Errorf("unresolvable address %q", host)
			}
			if isBlockedIP(ip) {
				return fmt.Errorf("refusing to connect to non-public address %s", ip)
			}
			return nil
		},
	}
	transport := &http.Transport{
		DialContext:         dialer.DialContext,
		TLSHandshakeTimeout: 10 * time.Second,
		DisableKeepAlives:   true,
	}
	return &Fetcher{
		http: &http.Client{
			Transport: transport,
			Timeout:   fetchTimeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= maxRedirects {
					return errors.New("too many redirects")
				}
				// The dialer re-checks the IP on each hop; here we just keep
				// the scheme sane so we never follow into file:// etc.
				if s := strings.ToLower(req.URL.Scheme); s != "http" && s != "https" {
					return fmt.Errorf("refusing redirect to %q scheme", req.URL.Scheme)
				}
				return nil
			},
		},
	}
}

// isBlockedIP reports whether an address must not be fetched server-side.
func isBlockedIP(ip net.IP) bool {
	return ip.IsLoopback() ||
		ip.IsPrivate() || // RFC1918 + IPv6 unique-local fc00::/7
		ip.IsLinkLocalUnicast() || // 169.254.0.0/16 (incl. metadata) + fe80::/10
		ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() ||
		ip.IsUnspecified()
}

// Fetch validates the URL, downloads it (guarded, size-capped), and returns
// the extracted readable text.
func (f *Fetcher) Fetch(ctx context.Context, rawURL string) (Source, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return Source{}, fmt.Errorf("invalid URL: %w", err)
	}
	if s := strings.ToLower(u.Scheme); s != "http" && s != "https" {
		return Source{}, errors.New("only http and https URLs are supported")
	}
	if u.Host == "" {
		return Source{}, errors.New("URL is missing a host")
	}

	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Source{}, err
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain;q=0.9")
	req.Header.Set("User-Agent", "mindful-social/1.0 (+node drafting)")

	resp, err := f.http.Do(req)
	if err != nil {
		return Source{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Source{}, fmt.Errorf("fetch returned %s", resp.Status)
	}
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	if ct != "" && !strings.Contains(ct, "text/html") &&
		!strings.Contains(ct, "text/plain") && !strings.Contains(ct, "xhtml") {
		return Source{}, fmt.Errorf("unsupported content type %q", ct)
	}

	title, text, err := extractText(io.LimitReader(resp.Body, maxFetchBytes))
	if err != nil {
		return Source{}, err
	}
	if text == "" {
		return Source{}, errors.New("no readable text found")
	}
	if len(text) > perSourceCharCap {
		text = text[:perSourceCharCap]
	}
	return Source{URL: u.String(), Title: title, Text: text}, nil
}

// skipTags are page regions that carry chrome, not content. Their subtrees are
// dropped entirely during extraction.
var skipTags = map[string]bool{
	"script": true, "style": true, "noscript": true, "template": true,
	"nav": true, "footer": true, "header": true, "aside": true,
	"form": true, "svg": true, "button": true, "iframe": true,
}

// extractText walks parsed HTML, drops chrome subtrees, and returns the page
// title plus its whitespace-collapsed body text.
func extractText(r io.Reader) (title, text string, err error) {
	doc, err := html.Parse(r)
	if err != nil {
		return "", "", err
	}
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			if n.Data == "title" && title == "" {
				title = strings.TrimSpace(nodeText(n))
				return
			}
			if skipTags[n.Data] {
				return
			}
		}
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
			b.WriteByte(' ')
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return title, collapseWS(b.String()), nil
}

// nodeText concatenates the text nodes under n (used for <title>).
func nodeText(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}

// collapseWS reduces every run of whitespace to a single space.
func collapseWS(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// SearchClient queries a SearXNG instance for result links. The instance URL
// is admin-configured and trusted, so this client is not SSRF-guarded — only
// the fetching of the result URLs (via Fetcher) is.
type SearchClient struct {
	baseURL string
	http    *http.Client
}

func NewSearchClient(baseURL string) *SearchClient {
	return &SearchClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: 15 * time.Second},
	}
}

// SearchHit is one result link from SearXNG.
type SearchHit struct {
	URL   string
	Title string
}

type searxResult struct {
	URL   string `json:"url"`
	Title string `json:"title"`
}

type searxResponse struct {
	Results []searxResult `json:"results"`
}

// Search returns up to limit result links for the query. Requires the SearXNG
// instance to have the JSON format enabled.
func (s *SearchClient) Search(ctx context.Context, query string, limit int) ([]SearchHit, error) {
	q := url.Values{}
	q.Set("q", query)
	q.Set("format", "json")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL+"/search?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("searxng %s: %s", resp.Status, strings.TrimSpace(string(msg)))
	}
	var parsed searxResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decode searxng response: %w", err)
	}
	hits := make([]SearchHit, 0, limit)
	for _, r := range parsed.Results {
		if r.URL == "" {
			continue
		}
		hits = append(hits, SearchHit{URL: r.URL, Title: r.Title})
		if len(hits) >= limit {
			break
		}
	}
	return hits, nil
}
