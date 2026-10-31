package main

import (
	"bytes"
	"strconv"

	"codeberg.org/go-pdf/fpdf"
)

const (
	pageWidth    = 105.0
	pageHeight   = 148.0
	marginTop    = 18.0
	marginSide   = 12.0
	marginBottom = 18.0
	lineHeight   = 5.0
	indent       = 6.0
)

// buildPDF lays the book out like a printed A6 pocket book: a running header with
// the book title on even pages and the chapter title on odd pages (left out on
// chapter openers), page numbers in the footer, and indented paragraphs without
// extra spacing. These are the artifacts the PDF extractor must remove or
// interpret.
func buildPDF(b book) ([]byte, error) {
	pdf := fpdf.NewCustom(&fpdf.InitType{
		OrientationStr: "P",
		UnitStr:        "mm",
		Size:           fpdf.SizeType{Wd: pageWidth, Ht: pageHeight},
	})
	// Uncompressed and with fixed dates, so the output is byte-for-byte stable.
	pdf.SetCompression(false)
	pdf.SetCatalogSort(true)
	pdf.SetCreationDate(fixedTime)
	pdf.SetModificationDate(fixedTime)
	pdf.SetTitle(b.Title, true)
	pdf.SetAuthor(b.Author, true)
	pdf.SetLang(b.Language)
	pdf.SetMargins(marginSide, marginTop, marginSide)
	pdf.SetAutoPageBreak(true, marginBottom)
	tr := pdf.UnicodeTranslatorFromDescriptor("")

	var chapterTitle string
	var chapterFirstPage int
	pdf.SetHeaderFunc(func() {
		if pdf.PageNo() != chapterFirstPage {
			header := chapterTitle
			if pdf.PageNo()%2 == 0 {
				header = b.Title
			}
			pdf.SetFont("Times", "I", 9)
			pdf.SetY(8)
			pdf.CellFormat(0, lineHeight, tr(header), "", 0, "C", false, 0, "")
		}
		pdf.SetY(marginTop)
	})
	pdf.SetFooterFunc(func() {
		pdf.SetFont("Times", "", 9)
		pdf.SetY(-13)
		pdf.CellFormat(0, lineHeight, strconv.Itoa(pdf.PageNo()), "", 0, "C", false, 0, "")
	})

	for _, ch := range b.Chapters {
		chapterTitle = ch.Title
		chapterFirstPage = pdf.PageNo() + 1
		pdf.AddPage()
		pdf.Bookmark(tr(ch.Title), 0, -1)
		pdf.SetFont("Times", "B", 16)
		pdf.MultiCell(0, 8, tr(ch.Title), "", "L", false)
		pdf.Ln(6)

		pdf.SetFont("Times", "", 11)
		for i, p := range ch.Paragraphs {
			if i > 0 {
				pdf.SetX(marginSide + indent)
			}
			pdf.Write(lineHeight, tr(p))
			pdf.Ln(lineHeight)
		}
	}

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
