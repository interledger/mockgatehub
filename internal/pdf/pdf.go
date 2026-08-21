// Package pdf builds minimal, single-page, text-only PDF documents.
//
// It exists so that mock statements can name what they are a statement of --
// the wallet, the period, the transaction -- rather than every endpoint
// returning one indistinguishable placeholder blob. A caller checking that the
// right statement came back can read it.
//
// The output is deliberately the smallest thing a PDF reader will accept: one
// page, one standard font, no compression, no images.
package pdf

import (
	"bytes"
	"fmt"
	"strings"
)

// Page geometry in PostScript points (A4).
const (
	pageWidth  = 595
	pageHeight = 842

	marginLeft = 56
	marginTop  = 64

	titleFontSize = 18
	bodyFontSize  = 11
	lineHeight    = 18
)

// Document is a single-page text document.
type Document struct {
	Title string
	// Lines are rendered below the title, in order. An empty string leaves a
	// blank line.
	Lines []string
}

// Render returns the document as PDF bytes.
func Render(doc Document) []byte {
	content := buildContentStream(doc)

	// Object 5 is the content stream; the rest are fixed scaffolding.
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %d %d] "+
			"/Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>", pageWidth, pageHeight),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
	}

	var out bytes.Buffer
	out.WriteString("%PDF-1.4\n")

	// Record where each object starts: the cross-reference table is a table of
	// byte offsets, so it can only be written once the body is laid out.
	offsets := make([]int, len(objects))
	for i, body := range objects {
		offsets[i] = out.Len()
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", i+1, body)
	}

	xrefOffset := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n", len(objects)+1)
	// The head of the free list, which every PDF carries verbatim.
	out.WriteString("0000000000 65535 f \n")
	for _, offset := range offsets {
		fmt.Fprintf(&out, "%010d 00000 n \n", offset)
	}

	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n",
		len(objects)+1, xrefOffset)

	return out.Bytes()
}

// buildContentStream lays the text out top-down.
func buildContentStream(doc Document) string {
	var b strings.Builder
	b.WriteString("BT\n")

	y := pageHeight - marginTop
	fmt.Fprintf(&b, "/F1 %d Tf\n1 0 0 1 %d %d Tm\n(%s) Tj\n",
		titleFontSize, marginLeft, y, escapeText(doc.Title))

	y -= lineHeight * 2
	fmt.Fprintf(&b, "/F1 %d Tf\n", bodyFontSize)
	for _, line := range doc.Lines {
		if y < lineHeight {
			// One page only: silently dropping the rest is better than
			// producing a malformed document.
			break
		}
		fmt.Fprintf(&b, "1 0 0 1 %d %d Tm\n(%s) Tj\n", marginLeft, y, escapeText(line))
		y -= lineHeight
	}

	b.WriteString("ET")
	return b.String()
}

// escapeText escapes the three characters that are structural inside a PDF
// string literal, and drops anything outside printable ASCII so the output
// stays valid without needing a font encoding.
func escapeText(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '(' || r == ')' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r >= 32 && r < 127:
			b.WriteRune(r)
		default:
			b.WriteByte('?')
		}
	}
	return b.String()
}
