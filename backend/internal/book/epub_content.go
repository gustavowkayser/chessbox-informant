package book

import (
	"cmp"
	"errors"
	"io"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/net/html"
)

var (
	blockTags = tagSet("address", "article", "aside", "blockquote", "body", "caption", "dd", "details", "div",
		"dl", "dt", "fieldset", "figcaption", "figure", "footer", "form", "header", "hgroup", "hr", "li", "main",
		"nav", "ol", "p", "pre", "section", "summary", "table", "tbody", "td", "tfoot", "th", "thead", "tr", "ul",
		"h1", "h2", "h3", "h4", "h5", "h6")
	headingTags = tagSet("h1", "h2", "h3", "h4", "h5", "h6")
	skippedTags = tagSet("head", "script", "style", "template", "noscript")
	voidTags    = tagSet("area", "base", "br", "col", "embed", "hr", "img", "input", "link", "meta", "source",
		"track", "wbr")
)

func tagSet(tags ...string) map[string]bool {
	set := make(map[string]bool, len(tags))
	for _, t := range tags {
		set[t] = true
	}
	return set
}

// tocTargets are the table of contents entries that point into one file.
type tocTargets struct {
	start    string
	hasStart bool
	ids      map[string]string
}

// contentWalker turns XHTML content documents into blocks. It is fed the
// spine documents in order and keeps the current chapter and page across them.
type contentWalker struct {
	targets map[string]tocTargets
	chapter string
	page    int
	blocks  []Block

	text           strings.Builder
	textPage       int
	skipDepth      int
	headingDepth   int
	pageBreakDepth int
	pageText       strings.Builder
}

type openElement struct {
	tag       string
	block     bool
	heading   bool
	skip      bool
	pageBreak bool
	pageLabel string
	id        string
}

func newContentWalker(toc []tocEntry) *contentWalker {
	targets := make(map[string]tocTargets)
	for _, e := range toc {
		t := targets[e.path]
		// Later entries are deeper or more specific, so they win when several
		// point to the same place.
		if e.fragment == "" {
			t.start, t.hasStart = e.label, true
		} else {
			if t.ids == nil {
				t.ids = make(map[string]string)
			}
			t.ids[e.fragment] = e.label
		}
		targets[e.path] = t
	}
	return &contentWalker{targets: targets}
}

func (w *contentWalker) walk(r io.Reader, name string) error {
	targets := w.targets[name]
	if targets.hasStart {
		w.setChapter(targets.start)
	}

	// The tokenizer is used instead of html.Parse because content documents
	// are XHTML: html.Parse would treat a self-closing <span/> as an open
	// element and nest the following text inside it.
	z := html.NewTokenizer(r)
	var stack []openElement
	for {
		switch tt := z.Next(); tt {
		case html.ErrorToken:
			if errors.Is(z.Err(), io.EOF) {
				for i := len(stack) - 1; i >= 0; i-- {
					w.close(stack[i])
				}
				w.flush()
				return nil
			}
			return z.Err()
		case html.TextToken:
			w.addText(string(z.Text()))
		case html.StartTagToken, html.SelfClosingTagToken:
			e := w.open(z.Token(), targets)
			if tt == html.SelfClosingTagToken || voidTags[e.tag] {
				w.close(e)
			} else {
				stack = append(stack, e)
			}
		case html.EndTagToken:
			tag, _ := z.TagName()
			for i := len(stack) - 1; i >= 0; i-- {
				if stack[i].tag == string(tag) {
					for j := len(stack) - 1; j >= i; j-- {
						w.close(stack[j])
					}
					stack = stack[:i]
					break
				}
			}
		}
	}
}

func (w *contentWalker) open(tok html.Token, targets tocTargets) openElement {
	e := openElement{tag: tok.Data, block: blockTags[tok.Data], heading: headingTags[tok.Data]}
	if e.block {
		w.flush()
	}
	if label, ok := targets.ids[attr(tok.Attr, "id")]; ok {
		w.setChapter(label)
	}

	semantics := strings.Fields(attr(tok.Attr, "epub:type") + " " + attr(tok.Attr, "role"))
	switch {
	case slices.Contains(semantics, "pagebreak") || slices.Contains(semantics, "doc-pagebreak"):
		e.pageBreak = true
		e.pageLabel = cmp.Or(attr(tok.Attr, "title"), attr(tok.Attr, "aria-label"))
		e.id = attr(tok.Attr, "id")
		w.pageBreakDepth++
		w.pageText.Reset()
	case skippedTags[e.tag] || slices.Contains(semantics, "noteref") || slices.Contains(semantics, "doc-noteref"):
		e.skip = true
		w.skipDepth++
	}
	if e.heading {
		w.headingDepth++
	}
	if e.tag == "br" {
		w.addText(" ")
	}
	return e
}

func (w *contentWalker) close(e openElement) {
	if e.block {
		w.flush()
	}
	if e.heading {
		w.headingDepth--
	}
	if e.skip {
		w.skipDepth--
	}
	if e.pageBreak {
		w.pageBreakDepth--
		w.page = trailingNumber(cmp.Or(e.pageLabel, strings.TrimSpace(w.pageText.String()), e.id))
	}
}

func (w *contentWalker) addText(s string) {
	if w.pageBreakDepth > 0 {
		w.pageText.WriteString(s)
		return
	}
	if w.skipDepth > 0 {
		return
	}
	if w.text.Len() == 0 {
		if normalizeText(s) == "" {
			return
		}
		w.textPage = w.page
	}
	w.text.WriteString(s)
}

func (w *contentWalker) flush() {
	text := normalizeText(w.text.String())
	w.text.Reset()
	if text == "" {
		return
	}
	w.blocks = append(w.blocks, Block{Chapter: w.chapter, Page: w.textPage, Text: text, Heading: w.headingDepth > 0})
}

func (w *contentWalker) setChapter(label string) {
	if label != w.chapter {
		w.flush()
		w.chapter = label
	}
}

// normalizeText collapses whitespace (including no-break spaces) and removes
// soft hyphens and zero-width characters.
func normalizeText(s string) string {
	s = strings.Map(func(r rune) rune {
		switch r {
		case 0x00ad, 0x200b, 0xfeff:
			return -1
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

// trailingNumber returns the number at the end of a page label such as "12",
// "Page 12" or "page_12", or 0 when there is none (for example "xii").
func trailingNumber(label string) int {
	digits := strings.TrimRightFunc(label, unicode.IsDigit)
	n, err := strconv.Atoi(label[len(digits):])
	if err != nil {
		return 0
	}
	return n
}
