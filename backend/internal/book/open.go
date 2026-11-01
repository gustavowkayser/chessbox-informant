package book

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
)

var ErrUnknownFormat = errors.New("not a PDF or ePUB file")

// pdfHeaderWindow is how far into the file the %PDF- header may start; PDF
// readers accept some junk before it.
const pdfHeaderWindow = 1024

// DetectFormat tells PDFs and ePUBs apart by their content, so a missing or
// wrong file extension does not matter.
func DetectFormat(file string) (Format, error) {
	f, err := os.Open(file)
	if err != nil {
		return "", err
	}
	defer f.Close()
	head := make([]byte, pdfHeaderWindow)
	n, err := io.ReadFull(f, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return "", err
	}
	head = head[:n]

	switch {
	case bytes.Contains(head, []byte("%PDF-")):
		return FormatPDF, nil
	case bytes.HasPrefix(head, []byte("PK\x03\x04")) && isEPUB(file):
		return FormatEPUB, nil
	}
	return "", ErrUnknownFormat
}

func isEPUB(file string) bool {
	zr, err := zip.OpenReader(file)
	if err != nil {
		return false
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name == "META-INF/container.xml" {
			return true
		}
	}
	return false
}

// Extract reads a PDF or an ePUB. popplerDir is where the Poppler tools are,
// or empty to use PATH.
func Extract(ctx context.Context, file, popplerDir string) (Document, error) {
	format, err := DetectFormat(file)

	if err != nil {
		return Document{}, err
	}
	strategies := map[Format]func(context.Context, string) (Document, error){
		FormatPDF:  PDFExtractor{PopplerDir: popplerDir}.Extract,
		FormatEPUB: EPUBExtractor{}.Extract,
	}

	strategy, exists := strategies[format]
	if !exists {
		return Document{}, ErrUnknownFormat
	}

	return strategy(ctx, file)
}

// FileSHA256 returns the hex SHA-256 of a file, which identifies a book.
func FileSHA256(file string) (string, error) {
	f, err := os.Open(file)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
