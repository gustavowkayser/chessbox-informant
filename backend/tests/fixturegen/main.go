// Command fixturegen builds book.epub and book.pdf from the book.md of each
// test fixture. With no arguments it regenerates every fixture in tests/assets.
//
//	go run ./tests/fixturegen [fixture-dir...]
package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var fixedTime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func main() {
	dirs := os.Args[1:]
	if len(dirs) == 0 {
		sources, err := filepath.Glob(filepath.Join("tests", "assets", "*", "book.md"))
		if err != nil {
			log.Fatal(err)
		}
		for _, s := range sources {
			dirs = append(dirs, filepath.Dir(s))
		}
	}
	for _, dir := range dirs {
		if err := generate(dir, dir); err != nil {
			log.Fatalf("%s: %v", dir, err)
		}
		fmt.Println("generated", dir)
	}
}

func generate(srcDir, outDir string) error {
	source, err := os.ReadFile(filepath.Join(srcDir, "book.md"))
	if err != nil {
		return err
	}
	text := strings.ReplaceAll(string(source), "\r\n", "\n")
	b, err := parseBook(text)
	if err != nil {
		return err
	}
	epub, err := buildEPUB(b, text)
	if err != nil {
		return fmt.Errorf("epub: %w", err)
	}
	pdf, err := buildPDF(b)
	if err != nil {
		return fmt.Errorf("pdf: %w", err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "book.epub"), epub, 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(outDir, "book.pdf"), pdf, 0o644)
}
