package book

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const fixtureDir = "../../tests/assets/classics"

func TestEPUBExtractorFixture(t *testing.T) {
	doc, err := EPUBExtractor{}.Extract(context.Background(), filepath.Join(fixtureDir, "book.epub"))
	if err != nil {
		t.Fatal(err)
	}
	wantMeta, wantBlocks := readFixtureMarkdown(t, filepath.Join(fixtureDir, "book.md"))
	if len(wantBlocks) < 20 {
		t.Fatalf("read only %d blocks from book.md", len(wantBlocks))
	}
	if doc.Format != FormatEPUB {
		t.Errorf("format = %q", doc.Format)
	}
	if doc.Metadata != wantMeta {
		t.Errorf("metadata = %+v, want %+v", doc.Metadata, wantMeta)
	}
	assertBlocks(t, doc.Blocks, wantBlocks)
}

// readFixtureMarkdown reads the markdown subset that tests/fixturegen turns
// into the fixture ePUB and PDF.
func readFixtureMarkdown(t *testing.T, file string) (Metadata, []Block) {
	t.Helper()
	src, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	front, body, _ := strings.Cut(strings.TrimPrefix(strings.ReplaceAll(string(src), "\r\n", "\n"), "---\n"), "\n---\n")
	var meta Metadata
	for line := range strings.SplitSeq(front, "\n") {
		key, value, _ := strings.Cut(line, ":")
		switch key {
		case "title":
			meta.Title = strings.TrimSpace(value)
		case "author":
			meta.Author = strings.TrimSpace(value)
		case "language":
			meta.Language = strings.TrimSpace(value)
		}
	}
	var blocks []Block
	var chapter string
	for paragraph := range strings.SplitSeq(body, "\n\n") {
		text := normalizeText(paragraph)
		if title, ok := strings.CutPrefix(text, "# "); ok {
			chapter = title
			blocks = append(blocks, Block{Chapter: chapter, Text: title, Heading: true})
		} else if text != "" {
			blocks = append(blocks, Block{Chapter: chapter, Text: text})
		}
	}
	return meta, blocks
}

func TestEPUBExtractorNCXOnly(t *testing.T) {
	doc := readTestEPUB(t, map[string]string{
		"OPS/package.opf": `<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="2.0">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:title>
      Endgame   Notes
    </dc:title>
    <dc:creator>First Author</dc:creator>
    <dc:creator>Second Author</dc:creator>
    <dc:language>pt-BR</dc:language>
  </metadata>
  <manifest>
    <item id="ncx" href="toc.ncx" media-type="application/x-dtbncx+xml"/>
    <item id="cover" href="text/cover.xhtml" media-type="application/xhtml+xml"/>
    <item id="part1" href="text/part%201.xhtml" media-type="application/xhtml+xml"/>
    <item id="part1b" href="text/part1b.xhtml" media-type="application/xhtml+xml"/>
    <item id="notes" href="text/notes.xhtml" media-type="application/xhtml+xml"/>
    <item id="image" href="images/board.svg" media-type="image/svg+xml"/>
    <item id="ch2" href="text/ch2.xhtml" media-type="application/xhtml+xml"/>
  </manifest>
  <spine toc="ncx">
    <itemref idref="cover"/>
    <itemref idref="part1"/>
    <itemref idref="part1b"/>
    <itemref idref="notes" linear="no"/>
    <itemref idref="image"/>
    <itemref idref="ch2"/>
  </spine>
</package>`,
		"OPS/toc.ncx": `<?xml version="1.0" encoding="UTF-8"?>
<ncx xmlns="http://www.daisy.org/z3986/2005/ncx/" version="2005-1">
  <navMap>
    <navPoint id="p1"><navLabel><text>Part One</text></navLabel><content src="text/part%201.xhtml"/>
      <navPoint id="c1"><navLabel><text>Chapter 1</text></navLabel><content src="text/part%201.xhtml#ch1"/>
        <navPoint id="s1"><navLabel><text>Rook endings</text></navLabel><content src="text/part%201.xhtml#rooks"/></navPoint>
      </navPoint>
    </navPoint>
    <navPoint id="c2"><navLabel><text>Chapter 2</text></navLabel><content src="text/ch2.xhtml"/></navPoint>
  </navMap>
</ncx>`,
		"OPS/text/cover.xhtml":  xhtml(`<p>Cover text</p>`),
		"OPS/text/part 1.xhtml": xhtml(`<h1>Part One</h1><h2 id="ch1">Chapter 1</h2><p>Intro &amp; <b>1.e4</b>&#160;e5</p><h3 id="rooks">Rook endings</h3><p>Lucena</p>`),
		"OPS/text/part1b.xhtml": xhtml(`<p>Philidor</p>`),
		"OPS/text/notes.xhtml":  xhtml(`<p>Not in reading order</p>`),
		"OPS/images/board.svg":  `<svg xmlns="http://www.w3.org/2000/svg"><text>e4</text></svg>`,
		"OPS/text/ch2.xhtml":    xhtml(`<h1>Chapter 2</h1><p>Done</p>`),
	})

	want := Metadata{Title: "Endgame Notes", Author: "First Author", Language: "pt-BR"}
	if doc.Metadata != want {
		t.Errorf("metadata = %+v, want %+v", doc.Metadata, want)
	}
	assertBlocks(t, doc.Blocks, []Block{
		{Chapter: "", Text: "Cover text"},
		{Chapter: "Part One", Text: "Part One", Heading: true},
		{Chapter: "Chapter 1", Text: "Chapter 1", Heading: true},
		{Chapter: "Chapter 1", Text: "Intro & 1.e4 e5"},
		{Chapter: "Rook endings", Text: "Rook endings", Heading: true},
		{Chapter: "Rook endings", Text: "Lucena"},
		{Chapter: "Rook endings", Text: "Philidor"},
		{Chapter: "Chapter 2", Text: "Chapter 2", Heading: true},
		{Chapter: "Chapter 2", Text: "Done"},
	})
}

func TestEPUBExtractorNavAndPages(t *testing.T) {
	doc := readTestEPUB(t, map[string]string{
		"OPS/package.opf": `<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:title>Openings</dc:title>
    <dc:language>en</dc:language>
  </metadata>
  <manifest>
    <item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>
    <item id="ncx" href="toc.ncx" media-type="application/x-dtbncx+xml"/>
    <item id="c1" href="c1.xhtml" media-type="application/xhtml+xml"/>
  </manifest>
  <spine toc="ncx"><itemref idref="c1"/></spine>
</package>`,
		"OPS/nav.xhtml": xhtml(`
<nav epub:type="landmarks"><ol><li><a href="c1.xhtml">Start here</a></li></ol></nav>
<nav epub:type="toc"><ol><li><span>Part</span><ol><li><a href="c1.xhtml"><span>The</span> <em>Opening</em></a></li></ol></li></ol></nav>`),
		"OPS/toc.ncx": `<?xml version="1.0" encoding="UTF-8"?>
<ncx xmlns="http://www.daisy.org/z3986/2005/ncx/" version="2005-1">
  <navMap><navPoint><navLabel><text>NCX label</text></navLabel><content src="c1.xhtml"/></navPoint></navMap>
</ncx>`,
		"OPS/c1.xhtml": xhtml(`
<span epub:type="pagebreak" id="page7" title="7"/>
<section epub:type="chapter">
  <h1>The Opening</h1>
  <p>First para&#173;graph<a epub:type="noteref" href="#n1">1</a>.</p>
  <script>var ignored = "1.e4";</script>
  <p>Line one<br/>line two <span role="doc-pagebreak" aria-label="8"></span>continues</p>
  <p><span epub:type="pagebreak">9</span>Next</p>
  <div>Loose text<p>Inner</p>tail</div>
  <p><span epub:type="pagebreak" id="page_10"/>Last</p>
  <p><span epub:type="pagebreak" title="xii"/>Roman</p>
</section>`),
	})

	assertBlocks(t, doc.Blocks, []Block{
		{Chapter: "The Opening", Page: 7, Text: "The Opening", Heading: true},
		{Chapter: "The Opening", Page: 7, Text: "First paragraph."},
		{Chapter: "The Opening", Page: 7, Text: "Line one line two continues"},
		{Chapter: "The Opening", Page: 9, Text: "Next"},
		{Chapter: "The Opening", Page: 9, Text: "Loose text"},
		{Chapter: "The Opening", Page: 9, Text: "Inner"},
		{Chapter: "The Opening", Page: 9, Text: "tail"},
		{Chapter: "The Opening", Page: 10, Text: "Last"},
		{Chapter: "The Opening", Page: 0, Text: "Roman"},
	})
}

func TestEPUBExtractorAnchorInsideParagraph(t *testing.T) {
	doc := readTestEPUB(t, map[string]string{
		"OPS/package.opf": minimalOPF,
		"OPS/toc.ncx": `<ncx xmlns="http://www.daisy.org/z3986/2005/ncx/"><navMap>
  <navPoint><navLabel><text>One</text></navLabel><content src="c1.xhtml"/></navPoint>
  <navPoint><navLabel><text>Two</text></navLabel><content src="c1.xhtml#two"/></navPoint>
</navMap></ncx>`,
		"OPS/c1.xhtml": xhtml(`<p>End of one. <a id="two"/>Start of two.</p>`),
	})
	assertBlocks(t, doc.Blocks, []Block{
		{Chapter: "One", Text: "End of one."},
		{Chapter: "Two", Text: "Start of two."},
	})
}

func TestEPUBExtractorFontObfuscationIsAllowed(t *testing.T) {
	readTestEPUB(t, map[string]string{
		"OPS/package.opf":         minimalOPF,
		"OPS/toc.ncx":             minimalNCX,
		"OPS/c1.xhtml":            xhtml(`<p>Text</p>`),
		"META-INF/encryption.xml": encryptionXML("OPS/fonts/serif.otf"),
	})
}

func TestEPUBExtractorErrors(t *testing.T) {
	cases := map[string]struct {
		files map[string]string
		is    error
		msg   string
	}{
		"missing container": {msg: "container.xml", files: map[string]string{"OPS/package.opf": minimalOPF}},
		"missing package":   {msg: "package.opf", files: map[string]string{"META-INF/container.xml": containerXML}},
		"missing spine document": {msg: "c1.xhtml", files: map[string]string{
			"META-INF/container.xml": containerXML,
			"OPS/package.opf":        minimalOPF,
			"OPS/toc.ncx":            minimalNCX,
		}},
		"DRM": {is: ErrDRMProtected, files: map[string]string{
			"META-INF/container.xml":  containerXML,
			"OPS/package.opf":         minimalOPF,
			"OPS/toc.ncx":             minimalNCX,
			"OPS/c1.xhtml":            xhtml(`<p>Text</p>`),
			"META-INF/encryption.xml": encryptionXML("OPS/c1.xhtml"),
		}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := readEPUB(zipReader(t, c.files))
			if err == nil {
				t.Fatal("expected an error")
			}
			if c.is != nil && !errors.Is(err, c.is) {
				t.Errorf("error = %v, want %v", err, c.is)
			}
			if !strings.Contains(err.Error(), c.msg) {
				t.Errorf("error = %q, want it to mention %q", err, c.msg)
			}
		})
	}
}

func TestEPUBExtractorRejectsNonZip(t *testing.T) {
	file := filepath.Join(t.TempDir(), "book.epub")
	if err := os.WriteFile(file, []byte("not a zip"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := (EPUBExtractor{}).Extract(context.Background(), file); err == nil {
		t.Fatal("expected an error")
	}
}

const containerXML = `<?xml version="1.0" encoding="UTF-8"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles><rootfile full-path="OPS/package.opf" media-type="application/oebps-package+xml"/></rootfiles>
</container>`

const minimalOPF = `<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="2.0">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>T</dc:title></metadata>
  <manifest>
    <item id="ncx" href="toc.ncx" media-type="application/x-dtbncx+xml"/>
    <item id="c1" href="c1.xhtml" media-type="application/xhtml+xml"/>
  </manifest>
  <spine toc="ncx"><itemref idref="c1"/></spine>
</package>`

const minimalNCX = `<ncx xmlns="http://www.daisy.org/z3986/2005/ncx/"><navMap>
  <navPoint><navLabel><text>One</text></navLabel><content src="c1.xhtml"/></navPoint>
</navMap></ncx>`

func encryptionXML(uri string) string {
	return `<encryption xmlns="urn:oasis:names:tc:opendocument:xmlns:container" xmlns:enc="http://www.w3.org/2001/04/xmlenc#">
  <enc:EncryptedData>
    <enc:EncryptionMethod Algorithm="http://www.idpf.org/2008/embedding"/>
    <enc:CipherData><enc:CipherReference URI="` + uri + `"/></enc:CipherData>
  </enc:EncryptedData>
</encryption>`
}

func xhtml(body string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE html>
<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops">
<head><title>Head text is ignored</title><style>p { margin: 0 }</style></head>
<body>` + body + `</body>
</html>`
}

func readTestEPUB(t *testing.T, files map[string]string) Document {
	t.Helper()
	if _, ok := files["META-INF/container.xml"]; !ok {
		files["META-INF/container.xml"] = containerXML
	}
	doc, err := readEPUB(zipReader(t, files))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func zipReader(t *testing.T, files map[string]string) *zip.Reader {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	return zr
}

func assertBlocks(t *testing.T, got, want []Block) {
	t.Helper()
	if slices.Equal(got, want) {
		return
	}
	for i := range max(len(got), len(want)) {
		var g, w Block
		if i < len(got) {
			g = got[i]
		}
		if i < len(want) {
			w = want[i]
		}
		if g != w {
			t.Errorf("block %d:\n got  %+v\n want %+v", i, g, w)
		}
	}
}
