package parser

import (
	"strings"
	"testing"
)

// TestParseHTML_DropsDisclosureChrome pins that collapsible UI wrappers are
// treated as page chrome, not documentation.
//
// Found in real-world validation against pi.dev: every chunk of a 39-page crawl
// carried a "[Copied](…#anchor)" line, and pages whose body sat inside a
// <details> disclosure also leaked "Navigation / On this page / Documentation".
// 89.7% of chunks and 7.6% of all indexed characters were UI noise, which both
// dilutes embeddings and puts stray link text in front of an agent.
func TestParseHTML_DropsDisclosureChrome(t *testing.T) {
	const page = `<html><head><title>T</title></head><body>
<main>
  <details class="mobile-nav">
    <summary>Navigation</summary>
    <div>
      <p class="nav-heading">On this page</p>
      <nav><a href="#a">A</a></nav>
    </div>
  </details>
  <section>
    <h2>Real section</h2>
    <p>The actual documentation body.</p>
    <h2 id="a">Anchored section</h2>
    <p><a class="anchor" href="#a" aria-label="Copy link">Copied</a>More body text.</p>
    <details class="example">
      <summary>Example</summary>
      <p>Code inside a disclosure.</p>
    </details>
  </section>
</main>
</body></html>`

	doc, err := ParseHTML(Page{URL: "https://example.test/docs/page", HTML: page})
	if err != nil {
		t.Fatalf("ParseHTML: %v", err)
	}

	md := doc.Markdown

	for _, unwanted := range []string{
		"Navigation",
		"On this page",
		"Copied",
	} {
		if strings.Contains(md, unwanted) {
			t.Errorf("markdown should not contain the chrome string %q; got:\n%s", unwanted, md)
		}
	}

	for _, wanted := range []string{
		"The actual documentation body.",
		"Anchored section",
		"Code inside a disclosure.",
	} {
		if !strings.Contains(md, wanted) {
			t.Errorf("markdown should keep the documentation text %q; got:\n%s", wanted, md)
		}
	}
}

// TestParseHTML_KeepsStandaloneSummary guards the fix from over-reaching: a
// <details> is chrome when it is navigation, but a <summary> is real content
// when it introduces documentation that only exists inside the disclosure.
func TestParseHTML_KeepsStandaloneSummary(t *testing.T) {
	const page = `<html><head><title>T</title></head><body>
<main>
  <h1>Reference</h1>
  <details>
    <summary>Legacy alias</summary>
    <p>The old name for this option.</p>
  </details>
</main>
</body></html>`

	doc, err := ParseHTML(Page{URL: "https://example.test/docs/ref", HTML: page})
	if err != nil {
		t.Fatalf("ParseHTML: %v", err)
	}

	if !strings.Contains(doc.Markdown, "Legacy alias") {
		t.Errorf("a summary that labels documentation should be kept; got:\n%s", doc.Markdown)
	}
	if !strings.Contains(doc.Markdown, "The old name for this option.") {
		t.Errorf("content inside a disclosure should be kept; got:\n%s", doc.Markdown)
	}
}
