package book

import (
	"cmp"
	"errors"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

var ErrNoTextLayer = errors.New("the PDF has no text layer (it is probably scanned); OCR is not supported yet")

const (
	// minPageChars is how many non-space characters a page needs to count as
	// having text.
	minPageChars = 50
	// minRepeats is how many pages a top or bottom row must appear on to be
	// treated as a running header or footer.
	minRepeats = 3
	// runningBand is the share of the page height, at the top and at the
	// bottom, where running headers and footers sit.
	runningBand = 0.12
	// minColumnLines is how many lines a flow needs for its left edge to count
	// as a column margin.
	minColumnLines  = 2
	maxHeadingLines = 3
)

type outlineEntry struct {
	title string
	// page is the physical page, starting at 1.
	page int
}

// pdfLine is a line of text with its bounding box in points, as reported by
// `pdftotext -bbox-layout`. Lines come in reading order, and flow numbers the
// text flows (columns, headers, captions) of a page.
type pdfLine struct {
	text           string
	x0, y0, x1, y1 float64
	flow           int
}

type pdfPage struct {
	height  float64
	lines   []pdfLine
	printed int
}

var (
	digitRun  = regexp.MustCompile(`[0-9]+`)
	layoutGap = regexp.MustCompile(`\s{2,}`)
	spacedNum = regexp.MustCompile(`^[0-9][0-9 ]*$`)
	ligatures = strings.NewReplacer(
		string(rune(0xfb00)), "ff",
		string(rune(0xfb01)), "fi",
		string(rune(0xfb02)), "fl",
		string(rune(0xfb03)), "ffi",
		string(rune(0xfb04)), "ffl",
		string(rune(0xfb05)), "st",
		string(rune(0xfb06)), "st",
	)
)

// layoutBlocks turns positioned lines into blocks: it removes running headers
// and footers, reads printed page numbers from them, splits paragraphs on
// first-line indents and vertical gaps, and starts a chapter at each outline
// entry.
func layoutBlocks(pages []pdfPage, bookTitle string, outline []outlineEntry) ([]Block, error) {
	if !hasTextLayer(pages) {
		return nil, ErrNoTextLayer
	}
	stripRunningLines(pages, bookTitle, outline)
	return segment(pages, outline), nil
}

func hasTextLayer(pages []pdfPage) bool {
	withText := 0
	for _, p := range pages {
		n := 0
		for _, l := range p.lines {
			for _, r := range l.text {
				if !unicode.IsSpace(r) {
					n++
				}
			}
		}
		if n >= minPageChars {
			withText++
		}
	}
	return withText > 0 && withText*2 >= len(pages)
}

// stripRunningLines removes running headers and footers and sets each page's
// printed number. The top or bottom row of a page is running when it sits near
// the edge of the page and either repeats on several pages (digits ignored) or
// equals the book title or a chapter title. A chapter title on the chapter's
// own first page is its heading, not a running header.
func stripRunningLines(pages []pdfPage, bookTitle string, outline []outlineEntry) {
	titles := map[string]bool{}
	if bookTitle != "" {
		titles[fold(bookTitle)] = true
	}
	headings := map[int][]string{}
	for _, e := range outline {
		titles[fold(e.title)] = true
		headings[e.page] = append(headings[e.page], fold(e.title))
	}

	tops, bottoms := map[string]int{}, map[string]int{}
	for _, p := range pages {
		if _, text := edgeRow(p, true); text != "" {
			tops[runningKey(text)]++
		}
		if _, text := edgeRow(p, false); text != "" {
			bottoms[runningKey(text)]++
		}
	}

	detected := make([]int, len(pages))
	for i := range pages {
		isRunning := func(text string, counts map[string]int) bool {
			if slices.Contains(headings[i+1], fold(text)) {
				return false
			}
			return counts[runningKey(text)] >= minRepeats || titles[fold(text)]
		}
		if rows, text := edgeRow(pages[i], true); text != "" && isRunning(text, tops) {
			detected[i] = pageNumberIn(text)
			pages[i].lines = removeLines(pages[i].lines, rows)
		}
		if rows, text := edgeRow(pages[i], false); text != "" && isRunning(text, bottoms) {
			if n := pageNumberIn(text); n > 0 {
				detected[i] = n
			}
			pages[i].lines = removeLines(pages[i].lines, rows)
		}
	}
	for i, n := range printedPageNumbers(detected) {
		pages[i].printed = n
	}
}

// edgeRow returns the lines of the topmost (or bottommost) row of a page, if
// that row lies within the running band, and their text from left to right.
// A row can hold several lines, such as a page number and a title set apart.
func edgeRow(p pdfPage, top bool) ([]int, string) {
	if len(p.lines) == 0 {
		return nil, ""
	}
	edge := p.lines[0]
	for _, l := range p.lines {
		if (top && l.y0 < edge.y0) || (!top && l.y1 > edge.y1) {
			edge = l
		}
	}
	band := runningBand * p.height
	if (top && edge.y0 > band) || (!top && edge.y1 < p.height-band) {
		return nil, ""
	}
	var rows []int
	for i, l := range p.lines {
		if mid := (l.y0 + l.y1) / 2; mid >= edge.y0 && mid <= edge.y1 {
			rows = append(rows, i)
		}
	}
	slices.SortFunc(rows, func(a, b int) int { return cmp.Compare(p.lines[a].x0, p.lines[b].x0) })
	texts := make([]string, len(rows))
	for i, r := range rows {
		texts[i] = p.lines[r].text
	}
	return rows, strings.Join(texts, "    ")
}

func removeLines(lines []pdfLine, indexes []int) []pdfLine {
	var kept []pdfLine
	for i, l := range lines {
		if !slices.Contains(indexes, i) {
			kept = append(kept, l)
		}
	}
	return kept
}

func runningKey(text string) string {
	return digitRun.ReplaceAllString(fold(text), "#")
}

// pageNumberIn returns the page number in a running header or footer: a row
// that is only a number, or a number set apart from the rest of the row by a
// wide gap ("12      Classics Retold"). Numbers inside text, as in
// "Chapter 1: The Opera Game", are not page numbers. Digits split by a space
// ("1 18"), as some fonts are extracted, are joined.
func pageNumberIn(text string) int {
	parts := layoutGap.Split(strings.TrimSpace(text), -1)
	for _, p := range []string{parts[0], parts[len(parts)-1]} {
		if !spacedNum.MatchString(p) {
			continue
		}
		if n, err := strconv.Atoi(strings.ReplaceAll(p, " ", "")); err == nil && n > 0 {
			return n
		}
	}
	return 0
}

// printedPageNumbers fills the gaps between detected page numbers, such as
// chapter openers without a number. Pages before the first numbered page count
// down to 1 and are 0 before that. Without any detected number, the physical
// page number is used.
func printedPageNumbers(detected []int) []int {
	printed := make([]int, len(detected))
	found := false
	for i, n := range detected {
		switch {
		case n > 0:
			printed[i] = n
			found = true
		case i > 0 && printed[i-1] > 0:
			printed[i] = printed[i-1] + 1
		}
	}
	if !found {
		for i := range printed {
			printed[i] = i + 1
		}
		return printed
	}
	for i := len(printed) - 2; i >= 0; i-- {
		if printed[i] == 0 && printed[i+1] > 1 {
			printed[i] = printed[i+1] - 1
		}
	}
	return printed
}

type chapterStart struct {
	line    int
	title   string
	heading int // number of heading lines found on the page, 0 if none
}

func segment(pages []pdfPage, outline []outlineEntry) []Block {
	entries := map[int][]outlineEntry{}
	for _, e := range outline {
		entries[e.page] = append(entries[e.page], e)
	}

	var blocks []Block
	var chapter string
	var paragraph []string
	var paragraphPage int
	flush := func() {
		if text := joinLines(paragraph); text != "" {
			blocks = append(blocks, Block{Chapter: chapter, Page: paragraphPage, Text: text})
		}
		paragraph = nil
	}

	for i, page := range pages {
		texts := make([]string, len(page.lines))
		for j, l := range page.lines {
			texts[j] = l.text
		}
		starts := chapterStarts(texts, entries[i+1])
		metrics := measurePage(page.lines)
		var prev *pdfLine
		for j := 0; j < len(page.lines); {
			if len(starts) > 0 && starts[0].line == j {
				s := starts[0]
				starts = starts[1:]
				flush()
				chapter = s.title
				if s.heading > 0 {
					text := joinLines(texts[j : j+s.heading])
					blocks = append(blocks, Block{Chapter: chapter, Page: page.printed, Text: text, Heading: true})
					j += s.heading
					prev = &page.lines[j-1]
				}
				continue
			}
			line := &page.lines[j]
			j++
			if len(paragraph) > 0 && metrics.startsParagraph(line, prev) {
				flush()
			}
			if len(paragraph) == 0 {
				paragraphPage = page.printed
			}
			paragraph = append(paragraph, line.text)
			prev = line
		}
		for _, s := range starts {
			flush()
			chapter = s.title
		}
	}
	flush()
	return blocks
}

type pageMetrics struct {
	// margins are the left edges of the page's columns, in ascending order.
	margins    []float64
	lineHeight float64
	lineGap    float64
}

func measurePage(lines []pdfLine) pageMetrics {
	var heights, gaps []float64
	flowMargins := map[int]float64{}
	flowLines := map[int]int{}
	for i, l := range lines {
		heights = append(heights, l.y1-l.y0)
		if m, ok := flowMargins[l.flow]; !ok || l.x0 < m {
			flowMargins[l.flow] = l.x0
		}
		flowLines[l.flow]++
		if i > 0 && sameColumnBelow(&lines[i-1], &lines[i]) {
			gaps = append(gaps, l.y0-lines[i-1].y1)
		}
	}

	m := pageMetrics{lineHeight: median(heights), lineGap: median(gaps)}
	for flow, margin := range flowMargins {
		if flowLines[flow] >= minColumnLines {
			m.margins = append(m.margins, margin)
		}
	}
	if len(m.margins) == 0 && len(lines) > 0 {
		leftmost := slices.MinFunc(lines, func(a, b pdfLine) int { return cmp.Compare(a.x0, b.x0) })
		m.margins = append(m.margins, leftmost.x0)
	}
	slices.Sort(m.margins)
	return m
}

// startsParagraph reports whether line begins a new paragraph: it is indented
// from its column margin, or a wider than usual vertical gap separates it from
// the previous line of the same column. A line that is not indented continues
// the paragraph, also across columns and pages.
func (m pageMetrics) startsParagraph(line, prev *pdfLine) bool {
	margin := math.Inf(-1)
	for _, c := range m.margins {
		if c <= line.x0+1 {
			margin = c
		}
	}
	if !math.IsInf(margin, -1) && line.x0-margin >= 0.5*m.lineHeight {
		return true
	}
	return prev != nil && sameColumnBelow(prev, line) && line.y0-prev.y1 > m.lineGap+0.5*m.lineHeight
}

func sameColumnBelow(upper, lower *pdfLine) bool {
	return lower.y0 > upper.y0 && lower.x0 < upper.x1 && upper.x0 < lower.x1
}

func median(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := slices.Sorted(slices.Values(values))
	return sorted[len(sorted)/2]
}

// chapterStarts finds where each outline entry of a page begins: at its
// heading when the title is printed on the page, otherwise right after the
// previous entry (or at the top of the page).
func chapterStarts(lines []string, entries []outlineEntry) []chapterStart {
	var starts []chapterStart
	from := 0
	for _, e := range entries {
		line, n := findHeading(lines, from, e.title)
		if n == 0 {
			line = from
		}
		starts = append(starts, chapterStart{line: line, title: e.title, heading: n})
		from = line + n
	}
	return starts
}

func findHeading(lines []string, from int, title string) (line, n int) {
	want := fold(title)
	for j := from; j < len(lines); j++ {
		var acc []string
		for k := j; k < len(lines) && k < j+maxHeadingLines; k++ {
			acc = append(acc, lines[k])
			got := fold(strings.Join(acc, " "))
			if got == want {
				return j, k - j + 1
			}
			if !strings.HasPrefix(want, got) {
				break
			}
		}
	}
	return from, 0
}

// joinLines joins the lines of a paragraph. A word hyphenated across lines is
// rejoined ("posi-" + "tion"). Other line-final hyphens are kept and joined
// without a space, so "O-O-" + "O" stays "O-O-O" and "d-" + "file" stays
// "d-file".
func joinLines(lines []string) string {
	var b strings.Builder
	for i, l := range lines {
		l = strings.TrimSpace(l)
		if i > 0 {
			prev := b.String()
			if strings.HasSuffix(prev, "-") {
				if isWordHyphenation(prev, l) {
					b.Reset()
					b.WriteString(strings.TrimSuffix(prev, "-"))
				}
			} else {
				b.WriteByte(' ')
			}
		}
		b.WriteString(l)
	}
	return normalizeText(b.String())
}

func isWordHyphenation(prev, next string) bool {
	stem := strings.TrimSuffix(prev, "-")
	word := stem
	if i := strings.LastIndexFunc(stem, func(r rune) bool { return !unicode.IsLetter(r) }); i >= 0 {
		_, size := utf8.DecodeRuneInString(stem[i:])
		word = stem[i+size:]
	}
	first, _ := utf8.DecodeRuneInString(next)
	return utf8.RuneCountInString(word) >= 2 && unicode.IsLower(first)
}

func fold(s string) string {
	return strings.ToLower(normalizeText(s))
}
