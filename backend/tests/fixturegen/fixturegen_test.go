package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestGeneratedFixturesAreUpToDate(t *testing.T) {
	sources, err := filepath.Glob(filepath.Join("..", "assets", "*", "book.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) == 0 {
		t.Fatal("no fixtures found")
	}
	for _, source := range sources {
		dir := filepath.Dir(source)
		t.Run(filepath.Base(dir), func(t *testing.T) {
			out := t.TempDir()
			if err := generate(dir, out); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"book.epub", "book.pdf"} {
				want, err := os.ReadFile(filepath.Join(out, name))
				if err != nil {
					t.Fatal(err)
				}
				got, err := os.ReadFile(filepath.Join(dir, name))
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(got, want) {
					t.Errorf("%s is out of date with book.md; run `make fixtures`", name)
				}
			}
		})
	}
}

func TestParseBook(t *testing.T) {
	src := "---\ntitle: T\nauthor: A\nlanguage: en\n---\n\n# One\n\nFirst line\nsame paragraph.\n\nSecond.\n\n# Two\n\nThird.\n"
	b, err := parseBook(src)
	if err != nil {
		t.Fatal(err)
	}
	if b.Title != "T" || b.Author != "A" || b.Language != "en" {
		t.Errorf("metadata = %q %q %q", b.Title, b.Author, b.Language)
	}
	if len(b.Chapters) != 2 {
		t.Fatalf("got %d chapters, want 2", len(b.Chapters))
	}
	if got := b.Chapters[0].Paragraphs; len(got) != 2 || got[0] != "First line same paragraph." || got[1] != "Second." {
		t.Errorf("chapter one paragraphs = %q", got)
	}
	if got := b.Chapters[1]; got.Title != "Two" || len(got.Paragraphs) != 1 {
		t.Errorf("chapter two = %+v", got)
	}
}

func TestParseBookRejectsInvalidInput(t *testing.T) {
	cases := map[string]string{
		"no front matter":       "# One\n\nText.\n",
		"unclosed front matter": "---\ntitle: T\nlanguage: en\n# One\n",
		"missing title":         "---\nlanguage: en\n---\n# One\n",
		"unknown key":           "---\ntitle: T\nlanguage: en\nyear: 1900\n---\n# One\n",
		"text before heading":   "---\ntitle: T\nlanguage: en\n---\nText.\n# One\n",
		"no chapters":           "---\ntitle: T\nlanguage: en\n---\n\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parseBook(src); err == nil {
				t.Error("expected an error")
			}
		})
	}
}
