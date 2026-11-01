package book

import (
	"archive/zip"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"slices"
	"strings"

	"golang.org/x/net/html"
)

var ErrDRMProtected = errors.New("the ePUB is DRM-protected; only DRM-free books can be indexed")

type EPUBExtractor struct{}

func (EPUBExtractor) Extract(_ context.Context, file string) (Document, error) {
	zr, err := zip.OpenReader(file)
	if err != nil {
		return Document{}, fmt.Errorf("%s is not a valid ePUB: %w", file, err)
	}
	defer zr.Close()
	return readEPUB(&zr.Reader)
}

type epubArchive map[string]*zip.File

func (a epubArchive) open(name string) (io.ReadCloser, error) {
	f, ok := a[name]
	if !ok {
		return nil, fmt.Errorf("%s is missing from the ePUB", name)
	}
	return f.Open()
}

func (a epubArchive) decodeXML(name string, v any) error {
	r, err := a.open(name)
	if err != nil {
		return err
	}
	defer r.Close()
	if err := xml.NewDecoder(r).Decode(v); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

type manifestItem struct {
	path       string
	mediaType  string
	properties []string
}

type epubPackage struct {
	metadata  Metadata
	manifest  map[string]manifestItem
	spine     []string
	ncxItemID string
}

func readEPUB(zr *zip.Reader) (Document, error) {
	archive := make(epubArchive, len(zr.File))
	for _, f := range zr.File {
		archive[f.Name] = f
	}

	var container struct {
		Rootfiles []struct {
			FullPath string `xml:"full-path,attr"`
		} `xml:"rootfiles>rootfile"`
	}
	if err := archive.decodeXML("META-INF/container.xml", &container); err != nil {
		return Document{}, err
	}
	if len(container.Rootfiles) == 0 {
		return Document{}, errors.New("META-INF/container.xml lists no package document")
	}
	pkg, err := readPackage(archive, container.Rootfiles[0].FullPath)
	if err != nil {
		return Document{}, err
	}
	if err := checkEncryption(archive, pkg.spine); err != nil {
		return Document{}, err
	}
	toc, err := readTOC(archive, pkg)
	if err != nil {
		return Document{}, err
	}

	walker := newContentWalker(toc)
	for _, name := range pkg.spine {
		if err := walkContent(archive, name, walker); err != nil {
			return Document{}, err
		}
	}
	return Document{Format: FormatEPUB, Metadata: pkg.metadata, Blocks: walker.blocks}, nil
}

func walkContent(archive epubArchive, name string, walker *contentWalker) error {
	r, err := archive.open(name)
	if err != nil {
		return err
	}
	defer r.Close()
	if err := walker.walk(r, name); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

func readPackage(archive epubArchive, opfPath string) (epubPackage, error) {
	var opf struct {
		Titles    []string `xml:"metadata>title"`
		Creators  []string `xml:"metadata>creator"`
		Languages []string `xml:"metadata>language"`
		Items     []struct {
			ID         string `xml:"id,attr"`
			Href       string `xml:"href,attr"`
			MediaType  string `xml:"media-type,attr"`
			Properties string `xml:"properties,attr"`
		} `xml:"manifest>item"`
		Spine struct {
			TOC      string `xml:"toc,attr"`
			ItemRefs []struct {
				IDRef  string `xml:"idref,attr"`
				Linear string `xml:"linear,attr"`
			} `xml:"itemref"`
		} `xml:"spine"`
	}
	if err := archive.decodeXML(opfPath, &opf); err != nil {
		return epubPackage{}, err
	}

	first := func(values []string) string {
		if len(values) == 0 {
			return ""
		}
		return normalizeText(values[0])
	}
	pkg := epubPackage{
		metadata: Metadata{
			Title:    first(opf.Titles),
			Author:   first(opf.Creators),
			Language: first(opf.Languages),
		},
		manifest:  make(map[string]manifestItem, len(opf.Items)),
		ncxItemID: opf.Spine.TOC,
	}
	for _, item := range opf.Items {
		p, _ := resolveHref(opfPath, item.Href)
		pkg.manifest[item.ID] = manifestItem{
			path:       p,
			mediaType:  item.MediaType,
			properties: strings.Fields(item.Properties),
		}
		if pkg.ncxItemID == "" && item.MediaType == "application/x-dtbncx+xml" {
			pkg.ncxItemID = item.ID
		}
	}
	for _, ref := range opf.Spine.ItemRefs {
		if ref.Linear == "no" {
			continue
		}
		item, ok := pkg.manifest[ref.IDRef]
		if !ok {
			return epubPackage{}, fmt.Errorf("%s: spine item %q is not in the manifest", opfPath, ref.IDRef)
		}
		if item.mediaType == "application/xhtml+xml" || item.mediaType == "text/html" {
			pkg.spine = append(pkg.spine, item.path)
		}
	}
	return pkg, nil
}

// checkEncryption rejects books whose content documents are encrypted.
// Encrypted fonts alone are allowed: publishers obfuscate embedded fonts in
// otherwise DRM-free books.
func checkEncryption(archive epubArchive, spine []string) error {
	if _, ok := archive["META-INF/encryption.xml"]; !ok {
		return nil
	}
	var encryption struct {
		References []struct {
			URI string `xml:"URI,attr"`
		} `xml:"EncryptedData>CipherData>CipherReference"`
	}
	if err := archive.decodeXML("META-INF/encryption.xml", &encryption); err != nil {
		return err
	}
	for _, ref := range encryption.References {
		p, _ := resolveHref("", ref.URI)
		if slices.Contains(spine, p) {
			return ErrDRMProtected
		}
	}
	return nil
}

// resolveHref resolves an href found in the file at base into an archive path
// and a fragment.
func resolveHref(base, href string) (string, string) {
	ref, fragment, _ := strings.Cut(href, "#")
	if unescaped, err := url.PathUnescape(ref); err == nil {
		ref = unescaped
	}
	if ref == "" {
		return base, fragment
	}
	return path.Join(path.Dir(base), ref), fragment
}

type tocEntry struct {
	path     string
	fragment string
	label    string
}

func readTOC(archive epubArchive, pkg epubPackage) ([]tocEntry, error) {
	for _, item := range pkg.manifest {
		if slices.Contains(item.properties, "nav") {
			entries, err := readNav(archive, item.path)
			if err != nil || len(entries) > 0 {
				return entries, err
			}
			break
		}
	}
	if ncx, ok := pkg.manifest[pkg.ncxItemID]; ok {
		return readNCX(archive, ncx.path)
	}
	return nil, nil
}

func readNav(archive epubArchive, navPath string) ([]tocEntry, error) {
	r, err := archive.open(navPath)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	root, err := html.Parse(r)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", navPath, err)
	}

	var navs []*html.Node
	for n := range root.Descendants() {
		if n.Type == html.ElementNode && n.Data == "nav" {
			navs = append(navs, n)
		}
	}
	if len(navs) == 0 {
		return nil, nil
	}
	toc := navs[0]
	for _, n := range navs {
		if slices.Contains(strings.Fields(attr(n.Attr, "epub:type")), "toc") {
			toc = n
			break
		}
	}

	var entries []tocEntry
	for n := range toc.Descendants() {
		if n.Type != html.ElementNode || n.Data != "a" {
			continue
		}
		href := attr(n.Attr, "href")
		if href == "" {
			continue
		}
		p, fragment := resolveHref(navPath, href)
		entries = append(entries, tocEntry{path: p, fragment: fragment, label: normalizeText(nodeText(n))})
	}
	return entries, nil
}

type ncxNavPoint struct {
	Label   string `xml:"navLabel>text"`
	Content struct {
		Src string `xml:"src,attr"`
	} `xml:"content"`
	Children []ncxNavPoint `xml:"navPoint"`
}

func readNCX(archive epubArchive, ncxPath string) ([]tocEntry, error) {
	var ncx struct {
		NavPoints []ncxNavPoint `xml:"navMap>navPoint"`
	}
	if err := archive.decodeXML(ncxPath, &ncx); err != nil {
		return nil, err
	}
	var entries []tocEntry
	var flatten func([]ncxNavPoint)
	flatten = func(points []ncxNavPoint) {
		for _, p := range points {
			file, fragment := resolveHref(ncxPath, p.Content.Src)
			entries = append(entries, tocEntry{path: file, fragment: fragment, label: normalizeText(p.Label)})
			flatten(p.Children)
		}
	}
	flatten(ncx.NavPoints)
	return entries, nil
}

func attr(attrs []html.Attribute, key string) string {
	for _, a := range attrs {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func nodeText(n *html.Node) string {
	var b strings.Builder
	for d := range n.Descendants() {
		if d.Type == html.TextNode {
			b.WriteString(d.Data)
		}
	}
	return b.String()
}
