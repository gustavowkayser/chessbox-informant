package main

import (
	"errors"
	"fmt"
	"strings"
)

type book struct {
	Title    string
	Author   string
	Language string
	Chapters []chapter
}

type chapter struct {
	Title      string
	Paragraphs []string
}

// parseBook reads the markdown subset used by fixtures: a front matter block
// with title, author and language, "# " chapter headings, and paragraphs
// separated by blank lines.
func parseBook(src string) (book, error) {
	rest, hasFront := strings.CutPrefix(src, "---\n")
	front, body, closed := strings.Cut(rest, "\n---\n")
	if !hasFront || !closed {
		return book{}, errors.New("missing front matter")
	}

	var b book
	for line := range strings.SplitSeq(front, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			return book{}, fmt.Errorf("front matter: invalid line %q", line)
		}
		value = strings.TrimSpace(value)
		switch strings.TrimSpace(key) {
		case "title":
			b.Title = value
		case "author":
			b.Author = value
		case "language":
			b.Language = value
		default:
			return book{}, fmt.Errorf("front matter: unknown key %q", key)
		}
	}
	if b.Title == "" || b.Language == "" {
		return book{}, errors.New("front matter: title and language are required")
	}

	var paragraph []string
	flush := func() {
		if len(paragraph) > 0 {
			ch := &b.Chapters[len(b.Chapters)-1]
			ch.Paragraphs = append(ch.Paragraphs, strings.Join(paragraph, " "))
			paragraph = nil
		}
	}
	for line := range strings.SplitSeq(body, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "# "):
			flush()
			b.Chapters = append(b.Chapters, chapter{Title: strings.TrimSpace(line[2:])})
		case line == "":
			flush()
		case len(b.Chapters) == 0:
			return book{}, errors.New("text before the first chapter heading")
		default:
			paragraph = append(paragraph, line)
		}
	}
	flush()
	if len(b.Chapters) == 0 {
		return book{}, errors.New("no chapters")
	}
	return b, nil
}
