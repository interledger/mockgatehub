package pdf

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// textPattern picks the drawn strings out of a content stream.
var textPattern = regexp.MustCompile(`\(((?:[^()\\]|\\.)*)\) Tj`)

// extractText returns the strings the document draws, in order. It is the
// reader's view of the document: what a person opening the PDF would see.
func extractText(t *testing.T, body []byte) []string {
	t.Helper()
	matches := textPattern.FindAllStringSubmatch(string(body), -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		unescaped := strings.NewReplacer(`\(`, "(", `\)`, ")", `\\`, `\`).Replace(m[1])
		out = append(out, unescaped)
	}
	return out
}

func TestRender_ProducesAStructurallyValidDocument(t *testing.T) {
	body := Render(Document{Title: "Account Statement", Lines: []string{"one", "two"}})
	text := string(body)

	// A reader checks the header, finds the trailer, seeks to startxref and
	// reads the cross-reference table. All four have to be present.
	assert.True(t, strings.HasPrefix(text, "%PDF-"), "must start with the PDF header")
	assert.True(t, strings.HasSuffix(text, "%%EOF\n"), "must end with the EOF marker")
	assert.Contains(t, text, "trailer")
	assert.Contains(t, text, "/Root 1 0 R")
	assert.Contains(t, text, "/Type /Page")
}

func TestRender_XrefOffsetsPointAtTheObjects(t *testing.T) {
	// A cross-reference table with wrong offsets produces a file readers reject,
	// and the offsets can only be right if computed after layout.
	body := Render(Document{Title: "Statement", Lines: []string{"a line"}})
	text := string(body)

	startxrefIdx := strings.LastIndex(text, "startxref")
	require.NotEqual(t, -1, startxrefIdx)

	fields := strings.Fields(text[startxrefIdx+len("startxref"):])
	require.NotEmpty(t, fields)
	xrefOffset, err := strconv.Atoi(fields[0])
	require.NoError(t, err)
	require.Less(t, xrefOffset, len(body))
	assert.True(t, strings.HasPrefix(text[xrefOffset:], "xref"),
		"startxref must point at the xref table")

	// Each entry must land on the object it claims.
	entryPattern := regexp.MustCompile(`(?m)^(\d{10}) 00000 n $`)
	entries := entryPattern.FindAllStringSubmatch(text, -1)
	require.Len(t, entries, 5, "one entry per object")

	for i, entry := range entries {
		offset, err := strconv.Atoi(entry[1])
		require.NoError(t, err)
		require.Less(t, offset, len(body))
		assert.True(t, strings.HasPrefix(text[offset:], strconv.Itoa(i+1)+" 0 obj"),
			"xref entry %d points at %q", i+1, text[offset:min(offset+20, len(text))])
	}
}

func TestRender_StreamLengthMatchesTheStream(t *testing.T) {
	// A /Length that disagrees with the stream makes the page render as blank.
	body := Render(Document{Title: "Statement", Lines: []string{"a", "b", "c"}})
	text := string(body)

	lengthPattern := regexp.MustCompile(`<< /Length (\d+) >>\nstream\n`)
	m := lengthPattern.FindStringSubmatchIndex(text)
	require.NotNil(t, m, "expected a content stream")

	declared, err := strconv.Atoi(text[m[2]:m[3]])
	require.NoError(t, err)

	streamStart := m[1]
	streamEnd := strings.Index(text[streamStart:], "\nendstream")
	require.NotEqual(t, -1, streamEnd)
	assert.Equal(t, declared, streamEnd, "declared length must match the actual stream")
}

func TestRender_DrawsTheTitleAndEveryLine(t *testing.T) {
	doc := Document{
		Title: "Transfer Confirmation",
		Lines: []string{"Transaction: abc-123", "Amount: 10.00 EUR", "", "Issued: today"},
	}
	drawn := extractText(t, Render(doc))

	// A blank line is spacing, not content, but everything else must appear.
	assert.Equal(t, []string{
		"Transfer Confirmation",
		"Transaction: abc-123",
		"Amount: 10.00 EUR",
		"",
		"Issued: today",
	}, drawn)
}

func TestRender_EscapesCharactersThatWouldBreakTheDocument(t *testing.T) {
	// Unescaped parentheses or a backslash terminate the string literal early
	// and corrupt everything after it.
	doc := Document{Title: "Statement (2026)", Lines: []string{`path\to\thing`, "a) b ( c"}}
	body := Render(doc)

	drawn := extractText(t, body)
	assert.Equal(t, "Statement (2026)", drawn[0])
	assert.Equal(t, `path\to\thing`, drawn[1])
	assert.Equal(t, "a) b ( c", drawn[2])

	// And the document must still be well-formed.
	assert.True(t, strings.HasSuffix(string(body), "%%EOF\n"))
}

func TestRender_ReplacesCharactersTheFontCannotEncode(t *testing.T) {
	// The base font has no glyphs beyond ASCII; emitting the raw bytes would
	// produce an invalid string for the declared encoding.
	drawn := extractText(t, Render(Document{Title: "Zahlungsbestätigung", Lines: []string{"€10 — done"}}))

	assert.NotContains(t, drawn[0], "ä")
	assert.Contains(t, drawn[0], "?")
	assert.NotContains(t, drawn[1], "€")
}

func TestRender_HandlesAnEmptyDocument(t *testing.T) {
	body := Render(Document{Title: "Empty"})

	assert.True(t, strings.HasPrefix(string(body), "%PDF-"))
	assert.True(t, strings.HasSuffix(string(body), "%%EOF\n"))
	assert.Equal(t, []string{"Empty"}, extractText(t, body))
}

func TestRender_StaysOnOnePage(t *testing.T) {
	// More lines than fit must not spill into a second page object, since the
	// page tree declares exactly one.
	lines := make([]string, 200)
	for i := range lines {
		lines[i] = "line " + strconv.Itoa(i)
	}
	body := Render(Document{Title: "Long", Lines: lines})

	assert.Equal(t, 1, strings.Count(string(body), "/Type /Page\n")+strings.Count(string(body), "/Type /Page "),
		"exactly one page object")
	assert.Contains(t, string(body), "/Count 1")
	assert.Less(t, len(extractText(t, body)), len(lines), "lines beyond the page are dropped")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
