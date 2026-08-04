package parser

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ldaidone/go-graphed/internal/scanner"
)

func TestParse_PDFFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "report.pdf")
	if err := os.WriteFile(path, buildMinimalPDF(t, "First Page Content", "Second Page Content"), 0644); err != nil {
		t.Fatal(err)
	}

	file := scanner.File{Path: path, Language: "pdf", Size: 0}
	doc, err := Parse(file)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}

	if doc.Metadata["processor"] != "pdf-extractor" {
		t.Errorf("Metadata[processor] = %q, want pdf-extractor", doc.Metadata["processor"])
	}

	if len(doc.Entities) != 2 {
		t.Fatalf("expected 2 page entities, got %d", len(doc.Entities))
	}

	for i, e := range doc.Entities {
		if e.Type != "page" {
			t.Errorf("entity %d type = %q, want page", i, e.Type)
		}
		if e.Name != fmt.Sprintf("Page %d", i+1) {
			t.Errorf("entity %d name = %q, want Page %d", i, e.Name, i+1)
		}
		if e.Metadata["page"] != fmt.Sprintf("%d", i+1) {
			t.Errorf("entity %d metadata page = %q, want %d", i, e.Metadata["page"], i+1)
		}
	}
}

func TestParse_PDFFile_EmptyPagesSkipped(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "blank.pdf")
	if err := os.WriteFile(path, buildMinimalPDF(t, "Only One Page Has Text", ""), 0644); err != nil {
		t.Fatal(err)
	}

	file := scanner.File{Path: path, Language: "pdf", Size: 0}
	doc, err := Parse(file)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}

	if len(doc.Entities) != 1 {
		t.Fatalf("expected 1 page entity (blank page skipped), got %d", len(doc.Entities))
	}
	if doc.Entities[0].Name != "Page 1" {
		t.Errorf("entity name = %q, want Page 1", doc.Entities[0].Name)
	}
}

// buildMinimalPDF assembles a small, valid single-stream-per-page PDF
// with a correct xref table, so the pure-Go reader can open it. Page
// texts use the standard Helvetica font and default encoding.
func buildMinimalPDF(t *testing.T, pageTexts ...string) []byte {
	t.Helper()

	var kids []string
	for i := range pageTexts {
		kids = append(kids, fmt.Sprintf("%d 0 R", 3+2*i))
	}

	fontNum := 3 + 2*len(pageTexts) + 1

	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), len(pageTexts)),
	}
	for i, text := range pageTexts {
		streamNum := 4 + 2*i
		objects = append(objects,
			fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /ProcSet [/PDF /Text] /Font << /F1 %d 0 R >> >> /Contents %d 0 R >>", fontNum, streamNum),
		)
		stream := fmt.Sprintf("BT /F1 12 Tf 72 720 Td (%s) Tj ET", text)
		objects = append(objects, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream))
	}
	objects = append(objects, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects)+1)
	for i, body := range objects {
		offsets[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, body)
	}

	xrefPos := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n", len(objects)+1)
	fmt.Fprint(&buf, "0000000000 65535 f \n")
	for i := 1; i <= len(objects); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \n", offsets[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xrefPos)
	return buf.Bytes()
}
