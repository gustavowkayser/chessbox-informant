package book

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
)

const popplerInstallHint = "install Poppler (Windows: winget install oschwartz10612.Poppler, macOS: brew install poppler, " +
	"Debian/Ubuntu: apt install poppler-utils) or point --poppler-dir at its bin directory"

// PDFExtractor reads PDFs with the Poppler command-line tools.
type PDFExtractor struct {
	// PopplerDir is the directory with pdftotext and pdftohtml.
	// Empty means PATH.
	PopplerDir string
}

func (x PDFExtractor) Extract(ctx context.Context, file string) (Document, error) {
	if err := x.CheckPoppler(ctx); err != nil {
		return Document{}, err
	}
	// -bbox-layout keeps Poppler's reading order, which separates columns;
	// -layout would print the columns of a page side by side.
	out, err := x.run(ctx, "pdftotext", "-bbox-layout", "-enc", "UTF-8", file, "-")
	if err != nil {
		return Document{}, err
	}
	meta, pages, err := parseBBoxLayout(out)
	if err != nil {
		return Document{}, err
	}

	doc := Document{Format: FormatPDF, Metadata: meta}
	outline, err := x.readOutline(ctx, file)
	switch {
	case err != nil:
		doc.Warnings = append(doc.Warnings, fmt.Sprintf("could not read the PDF outline, so chapters are unknown: %v", err))
	case len(outline) == 0:
		doc.Warnings = append(doc.Warnings, "the PDF has no outline, so chapters are unknown")
	}
	doc.Blocks, err = layoutBlocks(pages, meta.Title, outline)
	if err != nil {
		return Document{}, err
	}
	return doc, nil
}

// CheckPoppler fails when pdftotext is missing or is the xpdf build (shipped
// with Git for Windows, among others), whose output differs from Poppler's.
func (x PDFExtractor) CheckPoppler(ctx context.Context) error {
	cmd, err := x.command(ctx, "pdftotext", "-v")
	if err != nil {
		return err
	}
	// Some versions exit with a non-zero status after printing the version,
	// so only the output is checked.
	out, err := cmd.CombinedOutput()
	if _, exited := errors.AsType[*exec.ExitError](err); err != nil && !exited {
		return fmt.Errorf("%s -v: %w; %s", cmd.Path, err, popplerInstallHint)
	}
	if !bytes.Contains(out, []byte("Poppler")) {
		return fmt.Errorf("%s is not Poppler's pdftotext (probably xpdf); %s", cmd.Path, popplerInstallHint)
	}
	return nil
}

func (x PDFExtractor) command(ctx context.Context, tool string, args ...string) (*exec.Cmd, error) {
	name := tool
	if x.PopplerDir != "" {
		dir, err := filepath.Abs(x.PopplerDir)
		if err != nil {
			return nil, err
		}
		name = filepath.Join(dir, tool)
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return nil, fmt.Errorf("%s not found: %w; %s", tool, err, popplerInstallHint)
	}
	return exec.CommandContext(ctx, path, args...), nil
}

func (x PDFExtractor) run(ctx context.Context, tool string, args ...string) ([]byte, error) {
	cmd, err := x.command(ctx, tool, args...)
	if err != nil {
		return nil, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%s: %w: %s", tool, err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

type bboxLine struct {
	XMin  float64 `xml:"xMin,attr"`
	YMin  float64 `xml:"yMin,attr"`
	XMax  float64 `xml:"xMax,attr"`
	YMax  float64 `xml:"yMax,attr"`
	Words []struct {
		XMin float64 `xml:"xMin,attr"`
		XMax float64 `xml:"xMax,attr"`
		Text string  `xml:",chardata"`
	} `xml:"word"`
}

// parseBBoxLayout reads the XHTML written by `pdftotext -bbox-layout`: the
// title and author in the head, then pages of flows, blocks, lines and words.
func parseBBoxLayout(data []byte) (Metadata, []pdfPage, error) {
	var doc struct {
		Title string `xml:"head>title"`
		Metas []struct {
			Name    string `xml:"name,attr"`
			Content string `xml:"content,attr"`
		} `xml:"head>meta"`
		Pages []struct {
			Height float64 `xml:"height,attr"`
			Flows  []struct {
				Lines []bboxLine `xml:"block>line"`
			} `xml:"flow"`
		} `xml:"body>doc>page"`
	}
	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.Strict = false
	decoder.Entity = xml.HTMLEntity
	if err := decoder.Decode(&doc); err != nil {
		return Metadata{}, nil, fmt.Errorf("pdftotext -bbox-layout output: %w", err)
	}

	meta := Metadata{Title: normalizeText(doc.Title)}
	for _, m := range doc.Metas {
		if m.Name == "Author" {
			meta.Author = normalizeText(m.Content)
		}
	}
	pages := make([]pdfPage, len(doc.Pages))
	for i, p := range doc.Pages {
		pages[i].height = p.Height
		for f, flow := range p.Flows {
			for _, l := range flow.Lines {
				pages[i].lines = append(pages[i].lines, pdfLine{
					text: lineText(l),
					x0:   l.XMin, y0: l.YMin, x1: l.XMax, y1: l.YMax,
					flow: f,
				})
			}
		}
	}
	return meta, pages, nil
}

// lineText joins the words of a line. Words set apart by more than the line
// height (a page number next to a running title) are joined with a wide gap,
// which pageNumberIn relies on.
func lineText(l bboxLine) string {
	var b strings.Builder
	for i, w := range l.Words {
		if i > 0 {
			if w.XMin-l.Words[i-1].XMax > l.YMax-l.YMin {
				b.WriteString("    ")
			} else {
				b.WriteByte(' ')
			}
		}
		b.WriteString(ligatures.Replace(w.Text))
	}
	return b.String()
}

// readOutline reads the PDF outline (bookmarks) with pdftohtml, the only
// Poppler tool that prints it with page numbers. Only the first page is
// converted, since the outline is printed in full regardless of the range.
func (x PDFExtractor) readOutline(ctx context.Context, file string) ([]outlineEntry, error) {
	out, err := x.run(ctx, "pdftohtml", "-xml", "-i", "-q", "-stdout", "-f", "1", "-l", "1", file)
	if err != nil {
		return nil, err
	}
	return parseOutline(out)
}

// parseOutline reads the <outline> element of pdftohtml's XML output, where
// nested <outline> elements follow the <item> they belong to.
func parseOutline(data []byte) ([]outlineEntry, error) {
	start := bytes.Index(data, []byte("<outline"))
	if start < 0 {
		return nil, nil
	}
	decoder := xml.NewDecoder(bytes.NewReader(data[start:]))
	decoder.Strict = false
	var entries []outlineEntry
	depth := 0
	for {
		tok, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return entries, nil
		}
		if err != nil {
			return nil, fmt.Errorf("pdftohtml outline: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "outline":
				depth++
			case "item":
				var item struct {
					Page int    `xml:"page,attr"`
					Text string `xml:",chardata"`
				}
				if err := decoder.DecodeElement(&item, &t); err != nil {
					return nil, fmt.Errorf("pdftohtml outline: %w", err)
				}
				if title := normalizeText(item.Text); title != "" && item.Page > 0 {
					entries = append(entries, outlineEntry{title: title, page: item.Page})
				}
			}
		case xml.EndElement:
			if t.Name.Local == "outline" {
				depth--
				if depth == 0 {
					return entries, nil
				}
			}
		}
	}
}
