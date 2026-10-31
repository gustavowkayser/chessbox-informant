package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"fmt"
	"html"
	"text/template"
)

type epubChapter struct {
	chapter
	ID   string
	File string
}

type epubData struct {
	Title      string
	Author     string
	Language   string
	Identifier string
	Modified   string
	Chapters   []epubChapter
}

var epubTemplates = template.Must(template.New("epub").Funcs(template.FuncMap{
	"esc": html.EscapeString,
	"inc": func(i int) int { return i + 1 },
}).Parse(`
{{define "container"}}<?xml version="1.0" encoding="UTF-8"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles>
    <rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/>
  </rootfiles>
</container>
{{end}}

{{define "opf"}}<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="book-id" xml:lang="{{esc .Language}}">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:identifier id="book-id">{{esc .Identifier}}</dc:identifier>
    <dc:title>{{esc .Title}}</dc:title>
    {{- if .Author}}
    <dc:creator>{{esc .Author}}</dc:creator>
    {{- end}}
    <dc:language>{{esc .Language}}</dc:language>
    <meta property="dcterms:modified">{{.Modified}}</meta>
  </metadata>
  <manifest>
    <item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>
    <item id="ncx" href="toc.ncx" media-type="application/x-dtbncx+xml"/>
    {{- range .Chapters}}
    <item id="{{.ID}}" href="{{.File}}" media-type="application/xhtml+xml"/>
    {{- end}}
  </manifest>
  <spine toc="ncx">
    {{- range .Chapters}}
    <itemref idref="{{.ID}}"/>
    {{- end}}
  </spine>
</package>
{{end}}

{{define "nav"}}<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE html>
<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops" xml:lang="{{esc .Language}}" lang="{{esc .Language}}">
<head><title>{{esc .Title}}</title></head>
<body>
  <nav epub:type="toc" id="toc">
    <h1>Contents</h1>
    <ol>
      {{- range .Chapters}}
      <li><a href="{{.File}}">{{esc .Title}}</a></li>
      {{- end}}
    </ol>
  </nav>
</body>
</html>
{{end}}

{{define "ncx"}}<?xml version="1.0" encoding="UTF-8"?>
<ncx xmlns="http://www.daisy.org/z3986/2005/ncx/" version="2005-1">
  <head>
    <meta name="dtb:uid" content="{{esc .Identifier}}"/>
  </head>
  <docTitle><text>{{esc .Title}}</text></docTitle>
  <navMap>
    {{- range $i, $c := .Chapters}}
    <navPoint id="nav-{{$c.ID}}" playOrder="{{inc $i}}">
      <navLabel><text>{{esc $c.Title}}</text></navLabel>
      <content src="{{$c.File}}"/>
    </navPoint>
    {{- end}}
  </navMap>
</ncx>
{{end}}

{{define "chapter"}}<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE html>
<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops" xml:lang="{{esc .Language}}" lang="{{esc .Language}}">
<head><title>{{esc .Chapter.Title}}</title></head>
<body>
  <section epub:type="chapter">
    <h1>{{esc .Chapter.Title}}</h1>
    {{- range .Chapter.Paragraphs}}
    <p>{{esc .}}</p>
    {{- end}}
  </section>
</body>
</html>
{{end}}
`))

func buildEPUB(b book, source string) ([]byte, error) {
	data := epubData{
		Title:      b.Title,
		Author:     b.Author,
		Language:   b.Language,
		Identifier: fmt.Sprintf("urn:sha256:%x", sha256.Sum256([]byte(source))),
		Modified:   fixedTime.Format("2006-01-02T15:04:05Z"),
	}
	for i, ch := range b.Chapters {
		id := fmt.Sprintf("chapter-%02d", i+1)
		data.Chapters = append(data.Chapters, epubChapter{chapter: ch, ID: id, File: id + ".xhtml"})
	}

	type entry struct {
		name, template string
		data           any
	}
	entries := []entry{
		{"META-INF/container.xml", "container", nil},
		{"OEBPS/content.opf", "opf", data},
		{"OEBPS/nav.xhtml", "nav", data},
		{"OEBPS/toc.ncx", "ncx", data},
	}
	for _, ch := range data.Chapters {
		entries = append(entries, entry{"OEBPS/" + ch.File, "chapter", struct {
			Language string
			Chapter  epubChapter
		}{b.Language, ch}})
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	// Entries are stored, not deflated: the drift test compares bytes, and
	// deflate output may change between Go releases.
	add := func(name string, content []byte) error {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store, Modified: fixedTime})
		if err != nil {
			return err
		}
		_, err = w.Write(content)
		return err
	}

	// The OCF spec requires mimetype to be the first entry.
	if err := add("mimetype", []byte("application/epub+zip")); err != nil {
		return nil, err
	}
	for _, e := range entries {
		var content bytes.Buffer
		if err := epubTemplates.ExecuteTemplate(&content, e.template, e.data); err != nil {
			return nil, fmt.Errorf("%s: %w", e.name, err)
		}
		if err := add(e.name, content.Bytes()); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
