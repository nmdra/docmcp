package parser

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"net/url"
	"strings"

	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/base"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/commonmark"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/table"
	"golang.org/x/net/html"
)

// ErrEmptyDocument reports a page with no convertible body content.
var ErrEmptyDocument = errors.New("empty document")

// ErrNotHTML reports a page whose content type is not a document DocMCP indexes.
var ErrNotHTML = errors.New("not an HTML document")

// ErrInvalidPage reports input the parser cannot key a document by.
var ErrInvalidPage = errors.New("invalid page")

// Page is the parser's input: a fetched page plus the URL it came from.
type Page struct {
	URL         string
	HTML        string
	ContentType string
}

// Document is a parsed page, ready to be chunked.
type Document struct {
	ID           string
	URL          string
	CanonicalURL string
	Title        string
	Markdown     string
	ContentHash  string
}

// droppedTags are page chrome, not documentation: indexing them adds noise that
// every chunk would carry and that retrieval would rank against real content.
var droppedTags = map[string]bool{
	"script":   true,
	"style":    true,
	"nav":      true,
	"footer":   true,
	"aside":    true,
	"header":   true,
	"noscript": true,
	"iframe":   true,
	"svg":      true,
	"form":     true,
	"button":   true,
}

// contentSelectors are tried in order; the first one present wins.
var contentSelectors = []string{"main", "article", "[role=main]", "body"}

// ParseHTML converts a fetched page into clean Markdown.
func ParseHTML(page Page) (Document, error) {
	if strings.TrimSpace(page.URL) == "" {
		// Without a URL a document has no stable identity, so it could never be
		// re-checked on sync or attributed in a citation.
		return Document{}, fmt.Errorf("%w: page has no URL", ErrInvalidPage)
	}
	if strings.TrimSpace(page.HTML) == "" {
		return Document{}, fmt.Errorf("%w: %s has no HTML", ErrEmptyDocument, page.URL)
	}
	if err := checkContentType(page.ContentType); err != nil {
		return Document{}, err
	}

	doc, err := html.Parse(strings.NewReader(page.HTML))
	if err != nil {
		return Document{}, fmt.Errorf("parse %s: %w", page.URL, err)
	}

	// Read metadata before stripping: both the canonical link and the <title>
	// live in <head>, which stripChrome removes.
	canonical := canonicalURL(doc, page.URL)
	title := documentTitle(doc)

	stripChrome(doc)
	root := extractContent(doc)

	markdown, err := convert(root, page.URL)
	if err != nil {
		return Document{}, fmt.Errorf("convert %s: %w", page.URL, err)
	}
	markdown = strings.TrimRight(markdown, "\n") + "\n"

	if strings.TrimSpace(stripCodeFences(markdown)) == "" {
		return Document{}, fmt.Errorf("%w: %s has no body content", ErrEmptyDocument, page.URL)
	}

	return Document{
		ID:           documentID(page.URL),
		URL:          page.URL,
		CanonicalURL: canonical,
		Title:        title,
		Markdown:     markdown,
		ContentHash:  hashNormalizedContent(markdown),
	}, nil
}

func checkContentType(contentType string) error {
	if contentType == "" {
		return nil
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil
	}
	switch mediaType {
	case "text/html", "application/xhtml+xml":
		return nil
	default:
		return fmt.Errorf("%w: content type %q", ErrNotHTML, mediaType)
	}
}

// stripChrome removes chrome nodes and reports, "skip past this node" children.
func stripChrome(n *html.Node) {
	var kept []*html.Node

	for child := n.FirstChild; child != nil; {
		next := child.NextSibling

		if child.Type == html.CommentNode {
			child = next
			continue
		}
		if child.Type == html.ElementNode && droppedTags[child.Data] {
			child = next
			continue
		}
		if child.Type == html.ElementNode && isHidden(child) {
			child = next
			continue
		}

		stripChrome(child)
		kept = append(kept, child)
		child = next
	}

	rebuildChildren(n, kept)
}

func rebuildChildren(n *html.Node, kept []*html.Node) {
	for child := n.FirstChild; child != nil; {
		next := child.NextSibling
		n.RemoveChild(child)
		child = next
	}
	for _, c := range kept {
		n.AppendChild(c)
	}
}

// isHidden catches the common ways docs sites mark chrome as not content:
// aria-hidden, role=presentation, or an explicit hidden attribute.
func isHidden(n *html.Node) bool {
	for _, attr := range n.Attr {
		switch {
		case attr.Key == "hidden":
			return true
		case attr.Key == "aria-hidden" && attr.Val == "true":
			return true
		case attr.Key == "role" && attr.Val == "presentation":
			return true
		}
	}
	return false
}

// extractContent finds the main content root, falling back to body so a page
// without landmarks still converts.
func extractContent(doc *html.Node) *html.Node {
	for _, selector := range contentSelectors {
		if found := findFirst(doc, selector); found != nil {
			return found
		}
	}
	if body := findFirst(doc, "body"); body != nil {
		return body
	}
	return doc
}

func findFirst(n *html.Node, name string) *html.Node {
	if n.Type == html.ElementNode && n.Data == name {
		return n
	}
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if found := findFirst(child, name); found != nil {
			return found
		}
	}
	return nil
}

// convert renders the extracted node to Markdown. In-body links are resolved
// against the page URL first, so the Markdown carries absolute sources — the
// converter's own domain option only knows the host, not the page's directory,
// which would flatten "/latest/api" to "/api".
func convert(root *html.Node, pageURL string) (string, error) {
	parsed, err := url.Parse(pageURL)
	if err != nil {
		return "", fmt.Errorf("parse page URL %q: %w", pageURL, err)
	}

	absoluteLinks(root, parsed)

	// WithPlugins replaces the converter's entire rule set, so the base and
	// CommonMark plugins must be listed alongside the table plugin or the
	// output degrades to plain text.
	conv := converter.NewConverter(converter.WithPlugins(
		base.NewBasePlugin(),
		commonmark.NewCommonmarkPlugin(),
		table.NewTablePlugin(
			table.WithCellPaddingBehavior(table.CellPaddingBehaviorMinimal),
		),
	))
	out, err := conv.ConvertNode(root)
	if err != nil {
		return "", fmt.Errorf("convert node: %w", err)
	}
	return string(out), nil
}

// absoluteLinks rewrites every href and src in place to its absolute form.
func absoluteLinks(n *html.Node, base *url.URL) {
	if n.Type == html.ElementNode && (n.Data == "a" || n.Data == "img") {
		for i := range n.Attr {
			key := n.Attr[i].Key
			if key != "href" && key != "src" {
				continue
			}
			ref, err := url.Parse(n.Attr[i].Val)
			if err != nil || ref.Scheme == "" && strings.HasPrefix(ref.Path, "mailto:") {
				continue
			}
			n.Attr[i].Val = base.ResolveReference(ref).String()
		}
	}
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		absoluteLinks(child, base)
	}
}

// documentTitle prefers the article's own heading over the site-branded page
// title, which usually reads "Authentication — Acme Docs".
func documentTitle(doc *html.Node) string {
	if root := findFirst(doc, "main"); root != nil {
		if h := findFirst(root, "h1"); h != nil {
			if text := strings.TrimSpace(textContent(h)); text != "" {
				return text
			}
		}
	}
	if root := findFirst(doc, "article"); root != nil {
		if h := findFirst(root, "h1"); h != nil {
			if text := strings.TrimSpace(textContent(h)); text != "" {
				return text
			}
		}
	}
	if title := findFirst(doc, "title"); title != nil {
		if text := strings.TrimSpace(textContent(title)); text != "" {
			return text
		}
	}
	return ""
}

func textContent(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(n)
	return b.String()
}

func canonicalURL(doc *html.Node, fallback string) string {
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "link" {
			var rel, href string
			for _, attr := range n.Attr {
				switch strings.ToLower(attr.Key) {
				case "rel":
					rel = strings.ToLower(attr.Val)
				case "href":
					href = attr.Val
				}
			}
			if strings.Contains(rel, "canonical") && href != "" {
				if abs, err := url.Parse(href); err == nil {
					fallback = abs.String()
				}
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	return fallback
}

// documentID identifies a page by its URL, so the same page keeps its identity
// across syncs regardless of content edits.
func documentID(rawURL string) string {
	sum := sha256.Sum256([]byte(rawURL))
	return hex.EncodeToString(sum[:])[:16]
}

// hashNormalizedContent hashes the Markdown a chunk will actually embed:
// whitespace-collapsed and trimmed, so reformatting alone is not a change.
func hashNormalizedContent(markdown string) string {
	sum := sha256.Sum256([]byte(normalizeContent(markdown)))
	return hex.EncodeToString(sum[:])
}

func normalizeContent(s string) string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, strings.TrimRight(line, " \t"))
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func stripCodeFences(markdown string) string {
	var b strings.Builder
	for _, line := range strings.Split(markdown, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			continue
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}
