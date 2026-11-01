package book

import (
	"archive/zip"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestDetectFormat(t *testing.T) {
	dir := t.TempDir()
	copyTo := func(src, name string) string {
		data, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		dst := filepath.Join(dir, name)
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			t.Fatal(err)
		}
		return dst
	}
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	plainZip := filepath.Join(dir, "archive.zip")
	f, err := os.Create(plainZip)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	if _, err := zw.Create("readme.txt"); err != nil {
		t.Fatal(err)
	}
	zw.Close()
	f.Close()

	cases := []struct {
		file string
		want Format
		err  error
	}{
		{filepath.Join(fixtureDir, "book.epub"), FormatEPUB, nil},
		{filepath.Join(fixtureDir, "book.pdf"), FormatPDF, nil},
		{copyTo(filepath.Join(fixtureDir, "book.epub"), "renamed.pdf"), FormatEPUB, nil},
		{copyTo(filepath.Join(fixtureDir, "book.pdf"), "no-extension"), FormatPDF, nil},
		{write("junk-before.bin", "\x00\x01junk\n%PDF-1.7\n"), FormatPDF, nil},
		{write("notes.txt", "not a book"), "", ErrUnknownFormat},
		{write("empty.epub", ""), "", ErrUnknownFormat},
		{plainZip, "", ErrUnknownFormat},
	}
	for _, c := range cases {
		t.Run(filepath.Base(c.file), func(t *testing.T) {
			got, err := DetectFormat(c.file)
			if got != c.want || !errors.Is(err, c.err) {
				t.Errorf("DetectFormat = %q, %v; want %q, %v", got, err, c.want, c.err)
			}
		})
	}
}

func TestFileSHA256(t *testing.T) {
	file := filepath.Join(t.TempDir(), "abc")
	if err := os.WriteFile(file, []byte("abc"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := FileSHA256(file)
	if err != nil {
		t.Fatal(err)
	}
	if want := "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"; got != want {
		t.Errorf("FileSHA256 = %s, want %s", got, want)
	}
}
