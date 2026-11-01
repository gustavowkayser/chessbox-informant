package book

import (
	"context"
	"errors"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const (
	gridChar   = 5.0
	gridRow    = 12.0
	gridTop    = 40.0
	gridLeft   = 50.0
	gridColumn = 250.0
	gridHeight = 600.0
	gridFooter = 45
)

// gridPage describes a page as monospaced text: every character is 5pt wide
// and every row 12pt tall, holding a 10pt line. Leading spaces indent a line,
// a blank line leaves a gap, and each column is a flow, as Poppler reports.
// Body text starts below the running band; the footer sits at the bottom.
type gridPage struct {
	header  string
	columns []string
	footer  string
}

func (g gridPage) build() pdfPage {
	p := pdfPage{height: gridHeight}
	flow := 0
	add := func(text string, row int, left float64) {
		trimmed := strings.TrimLeft(text, " ")
		x0 := left + float64(len(text)-len(trimmed))*gridChar
		y0 := gridTop + float64(row)*gridRow
		p.lines = append(p.lines, pdfLine{text: trimmed, x0: x0, y0: y0, x1: x0 + float64(len(trimmed))*gridChar, y1: y0 + 10, flow: flow})
	}
	if g.header != "" {
		add(g.header, 0, gridLeft)
		flow++
	}
	for c, column := range g.columns {
		row := 3
		for line := range strings.SplitSeq(column, "\n") {
			if strings.TrimSpace(line) != "" {
				add(line, row, gridLeft+float64(c)*gridColumn)
			}
			row++
		}
		flow++
	}
	if g.footer != "" {
		add(g.footer, gridFooter, gridLeft)
	}
	return p
}

func buildPages(pages ...gridPage) []pdfPage {
	built := make([]pdfPage, len(pages))
	for i, p := range pages {
		built[i] = p.build()
	}
	return built
}

func TestLayoutBlocks(t *testing.T) {
	pages := buildPages(
		gridPage{
			columns: []string{`Chapter One

First paragraph starts here and
continues on this line.
    Second paragraph is indented and posi-
tion is hyphenated, the d-
file keeps its hyphen, and 12.O-O-
O is castling. It flows onto the next`},
			footer: "          9",
		},
		gridPage{
			header:  "        Running Head",
			columns: []string{"page. Continuation text here.\n    New paragraph on page two."},
			footer:  "          10",
		},
		gridPage{
			header:  "        Running Head",
			columns: []string{"    Body text on page three.\nSection Two\nText in section two."},
			footer:  "          11",
		},
		gridPage{columns: []string{"Chapter Two\n\nLast chapter text."}},
	)
	outline := []outlineEntry{
		{title: "Chapter One", page: 1},
		{title: "Section Two", page: 3},
		{title: "Chapter Two", page: 4},
	}

	blocks, err := layoutBlocks(pages, "Running Head", outline)
	if err != nil {
		t.Fatal(err)
	}
	assertBlocks(t, blocks, []Block{
		{Chapter: "Chapter One", Page: 9, Text: "Chapter One", Heading: true},
		{Chapter: "Chapter One", Page: 9, Text: "First paragraph starts here and continues on this line."},
		{Chapter: "Chapter One", Page: 9, Text: "Second paragraph is indented and position is hyphenated, the d-file keeps its hyphen, and 12.O-O-O is castling. It flows onto the next page. Continuation text here."},
		{Chapter: "Chapter One", Page: 10, Text: "New paragraph on page two."},
		{Chapter: "Chapter One", Page: 11, Text: "Body text on page three."},
		{Chapter: "Section Two", Page: 11, Text: "Section Two", Heading: true},
		{Chapter: "Section Two", Page: 11, Text: "Text in section two."},
		{Chapter: "Chapter Two", Page: 12, Text: "Chapter Two", Heading: true},
		{Chapter: "Chapter Two", Page: 12, Text: "Last chapter text."},
	})
}

func TestLayoutBlocksTwoColumns(t *testing.T) {
	pages := buildPages(gridPage{columns: []string{
		"Left column paragraph that\ncontinues here.\n    Second paragraph in the left\ncolumn goes on into the",
		"right column without an indent.\n    A new paragraph on the right.",
	}})
	blocks, err := layoutBlocks(pages, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	assertBlocks(t, blocks, []Block{
		{Page: 1, Text: "Left column paragraph that continues here."},
		{Page: 1, Text: "Second paragraph in the left column goes on into the right column without an indent."},
		{Page: 1, Text: "A new paragraph on the right."},
	})
}

func TestLayoutBlocksWithoutOutlineOrPageNumbers(t *testing.T) {
	pages := buildPages(
		gridPage{columns: []string{"A first page with enough text to count as having a text layer,"}},
		gridPage{columns: []string{"and a second page that continues the same paragraph."}},
	)
	blocks, err := layoutBlocks(pages, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	assertBlocks(t, blocks, []Block{
		{Page: 1, Text: "A first page with enough text to count as having a text layer, and a second page that continues the same paragraph."},
	})
}

func TestLayoutBlocksRejectsScannedPDF(t *testing.T) {
	scanned := gridPage{footer: "12"}
	pages := buildPages(scanned, scanned, scanned, scanned,
		gridPage{columns: []string{"One page with a caption that has enough characters in it."}})
	if _, err := layoutBlocks(pages, "", nil); !errors.Is(err, ErrNoTextLayer) {
		t.Fatalf("error = %v, want ErrNoTextLayer", err)
	}
}

func TestParseBBoxLayout(t *testing.T) {
	fi := string(rune(0xfb01))
	var pages [][]bboxTestLine
	for n := 18; n <= 20; n++ {
		pages = append(pages, []bboxTestLine{
			{flow: 0, x: 24, y: 14, text: fmt.Sprintf("1 %d", n)},
			{flow: 1, x: 162, y: 15, text: "Classics & Co"},
			{flow: 2, x: 33, y: 50, text: fmt.Sprintf("Page %d starts a paragraph with the %srst", n, fi)},
			{flow: 2, x: 23, y: 62, text: "ligature and enough characters to count."},
		})
	}
	meta, parsed, err := parseBBoxLayout(bboxXHTML("Classics & Co", "A. Author", pages))
	if err != nil {
		t.Fatal(err)
	}
	if want := (Metadata{Title: "Classics & Co", Author: "A. Author"}); meta != want {
		t.Errorf("metadata = %+v, want %+v", meta, want)
	}
	blocks, err := layoutBlocks(parsed, meta.Title, nil)
	if err != nil {
		t.Fatal(err)
	}
	var want []Block
	for n := 18; n <= 20; n++ {
		want = append(want, Block{Page: 100 + n, Text: fmt.Sprintf("Page %d starts a paragraph with the first ligature and enough characters to count.", n)})
	}
	assertBlocks(t, blocks, want)
}

func TestLineTextKeepsWideGaps(t *testing.T) {
	_, pages, err := parseBBoxLayout(bboxXHTML("", "", [][]bboxTestLine{{{x: 20, y: 14, text: "12      Running  Title"}}}))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := pages[0].lines[0].text, "12    Running Title"; got != want {
		t.Errorf("text = %q, want %q", got, want)
	}
}

func TestPageNumberIn(t *testing.T) {
	cases := map[string]int{
		"12":                         12,
		"   12      Classics Retold": 12,
		"Chapter 3          45":      45,
		"1 18    Running Title":      118,
		"Chapter 1: The Opera Game":  0,
		"Chapter 12":                 0,
		"Classics Retold":            0,
		"     xii     ":              0,
	}
	for text, want := range cases {
		if got := pageNumberIn(text); got != want {
			t.Errorf("pageNumberIn(%q) = %d, want %d", text, got, want)
		}
	}
}

func TestPrintedPageNumbers(t *testing.T) {
	cases := []struct{ detected, want []int }{
		{[]int{0, 0, 3, 0, 5, 0}, []int{1, 2, 3, 4, 5, 6}},
		{[]int{0, 0, 1, 0}, []int{0, 0, 1, 2}},
		{[]int{0, 0, 0}, []int{1, 2, 3}},
	}
	for _, c := range cases {
		if got := printedPageNumbers(c.detected); !slices.Equal(got, c.want) {
			t.Errorf("printedPageNumbers(%v) = %v, want %v", c.detected, got, c.want)
		}
	}
}

func TestParseOutline(t *testing.T) {
	data := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE pdf2xml SYSTEM "pdf2xml.dtd">
<pdf2xml producer="poppler" version="26.09.0">
<page number="1" position="absolute" top="0" left="0" height="420" width="298">
<text top="60" left="51" width="80" height="17" font="0"><b>Preface &amp; notes</b></text>
</page>
<outline>
<item page="1">Preface &amp; notes</item>
<item page="2">Part One</item>
<outline>
<item page="2">Chapter 1</item>
<item>Unresolved destination</item>
</outline>
<item page="9">Index</item>
</outline>
</pdf2xml>`)
	got, err := parseOutline(data)
	if err != nil {
		t.Fatal(err)
	}
	want := []outlineEntry{
		{title: "Preface & notes", page: 1},
		{title: "Part One", page: 2},
		{title: "Chapter 1", page: 2},
		{title: "Index", page: 9},
	}
	if !slices.Equal(got, want) {
		t.Errorf("outline = %+v, want %+v", got, want)
	}
}

func TestPDFExtractorFixture(t *testing.T) {
	x := requirePoppler(t)
	pdf, err := x.Extract(context.Background(), filepath.Join(fixtureDir, "book.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	if len(pdf.Warnings) > 0 {
		t.Errorf("warnings = %q", pdf.Warnings)
	}
	epub, err := EPUBExtractor{}.Extract(context.Background(), filepath.Join(fixtureDir, "book.epub"))
	if err != nil {
		t.Fatal(err)
	}
	if want := (Metadata{Title: epub.Metadata.Title, Author: epub.Metadata.Author}); pdf.Metadata != want {
		t.Errorf("metadata = %+v, want %+v", pdf.Metadata, want)
	}

	withoutPages := func(blocks []Block) []Block {
		out := slices.Clone(blocks)
		for i := range out {
			out[i].Page = 0
		}
		return out
	}
	assertBlocks(t, withoutPages(pdf.Blocks), withoutPages(epub.Blocks))

	headingPages := map[string]int{}
	for _, b := range pdf.Blocks {
		if b.Heading {
			headingPages[b.Text] = b.Page
		}
	}
	wantPages := map[string]int{
		"Preface":                      1,
		"Chapter 1: The Opera Game":    2,
		"Chapter 2: The Immortal Game": 5,
		"Chapter 3: Saavedra's Study":  7,
		"Chapter 4: On Pawn Structure": 8,
	}
	for title, page := range wantPages {
		if headingPages[title] != page {
			t.Errorf("%q on page %d, want %d", title, headingPages[title], page)
		}
	}
}

func TestPDFExtractorRejectsXpdf(t *testing.T) {
	dir := os.Getenv("CHESSINFO_TEST_XPDF_DIR")
	if dir == "" {
		t.Skip("set CHESSINFO_TEST_XPDF_DIR to a directory with xpdf's pdftotext to run this test")
	}
	err := PDFExtractor{PopplerDir: dir}.CheckPoppler(context.Background())
	if err == nil || !strings.Contains(err.Error(), "xpdf") {
		t.Fatalf("error = %v, want a message about xpdf", err)
	}
}

// requirePoppler skips the test when Poppler is not installed, unless
// CHESSINFO_REQUIRE_POPPLER is set (as in CI), in which case it fails.
func requirePoppler(t *testing.T) PDFExtractor {
	t.Helper()
	x := PDFExtractor{PopplerDir: os.Getenv("CHESSINFO_POPPLER_DIR")}
	if err := x.CheckPoppler(context.Background()); err != nil {
		if os.Getenv("CHESSINFO_REQUIRE_POPPLER") != "" {
			t.Fatal(err)
		}
		t.Skip(err)
	}
	return x
}

type bboxTestLine struct {
	flow int
	x, y float64
	text string
}

// bboxXHTML writes pages in the format of `pdftotext -bbox-layout`, with 5pt
// per character and 10pt tall lines. Extra spaces in a line's text widen the
// gap before the next word.
func bboxXHTML(title, author string, pages [][]bboxTestLine) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, `<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.0 Transitional//EN" "http://www.w3.org/TR/xhtml1/DTD/xhtml1-transitional.dtd"><html xmlns="http://www.w3.org/1999/xhtml">
<head>
<title>%s</title>
<meta name="Author" content="%s"/>
</head>
<body>
<doc>
`, html.EscapeString(title), html.EscapeString(author))
	for _, page := range pages {
		b.WriteString(`<page width="300.000000" height="420.000000">` + "\n")
		flow := -1
		for _, l := range page {
			if l.flow != flow {
				if flow >= 0 {
					b.WriteString("</block></flow>\n")
				}
				b.WriteString("<flow><block>\n")
				flow = l.flow
			}
			var words strings.Builder
			x := l.x
			for _, w := range strings.Split(l.text, " ") {
				if w == "" {
					x += gridChar
					continue
				}
				fmt.Fprintf(&words, `<word xMin="%f" yMin="%f" xMax="%f" yMax="%f">%s</word>`+"\n",
					x, l.y, x+float64(len([]rune(w)))*gridChar, l.y+10, html.EscapeString(w))
				x += float64(len([]rune(w))+1) * gridChar
			}
			fmt.Fprintf(&b, `<line xMin="%f" yMin="%f" xMax="%f" yMax="%f">`+"\n%s</line>\n", l.x, l.y, x-gridChar, l.y+10, words.String())
		}
		if flow >= 0 {
			b.WriteString("</block></flow>\n")
		}
		b.WriteString("</page>\n")
	}
	b.WriteString("</doc>\n</body>\n</html>\n")
	return []byte(b.String())
}
