//go:build cgo
// +build cgo

package sources

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	_ "embed"

	"github.com/ledongthuc/pdf"

	"github.com/cbip-solutions/hades-system/internal/research/ecosystem"
)

//go:embed arxiv_testdata/cs_PL_atom_response.xml
var arxivAtomXML []byte

//go:embed arxiv_testdata/sample_paper.pdf
var arxivSamplePDF []byte

func TestArxivSource_Ecosystem(t *testing.T) {
	src := NewArxivSource(ArxivOptions{Ecosystem: ecosystem.EcoGo})
	if src.Ecosystem() != ecosystem.EcoGo {
		t.Errorf("Ecosystem = %s; want go", src.Ecosystem())
	}
	if src.Kind() != ecosystem.SrcArXiv {
		t.Errorf("Kind = %s; want arxiv", src.Kind())
	}
}

func TestArxivSource_ImplementsSource(t *testing.T) {
	var _ ecosystem.Source = (*ArxivSource)(nil)
}

func TestArxivSource_DefaultsApplied(t *testing.T) {
	src := NewArxivSource(ArxivOptions{Ecosystem: ecosystem.EcoGo})
	if src.opts.BaseURL != "https://export.arxiv.org/api" {
		t.Errorf("BaseURL default = %q; want https://export.arxiv.org/api", src.opts.BaseURL)
	}
	if src.opts.AbsURL != "https://arxiv.org/abs" {
		t.Errorf("AbsURL default = %q; want https://arxiv.org/abs", src.opts.AbsURL)
	}
	if src.opts.PDFURL != "https://arxiv.org/pdf" {
		t.Errorf("PDFURL default = %q; want https://arxiv.org/pdf", src.opts.PDFURL)
	}
	if src.opts.MaxResults != 2000 {
		t.Errorf("MaxResults default = %d; want 2000", src.opts.MaxResults)
	}
	if src.opts.HTTPTimeout != 60*time.Second {
		t.Errorf("HTTPTimeout default = %v; want 60s", src.opts.HTTPTimeout)
	}
	wantCats := []string{"cs.PL", "cs.SE"}
	if !equalStrings(src.opts.Categories, wantCats) {
		t.Errorf("Categories default for Go = %v; want %v", src.opts.Categories, wantCats)
	}

	if src.opts.IncludePDF {
		t.Errorf("IncludePDF default = true; want false (opt-in)")
	}
}

func TestArxivSource_OverridesPreserved(t *testing.T) {
	src := NewArxivSource(ArxivOptions{
		Ecosystem:   ecosystem.EcoPython,
		BaseURL:     "https://example.com/api",
		AbsURL:      "https://example.com/abs",
		PDFURL:      "https://example.com/pdf",
		Categories:  []string{"cs.CL"},
		MaxResults:  7,
		HTTPTimeout: 5 * time.Second,
		IncludePDF:  true,
	})
	if src.opts.BaseURL != "https://example.com/api" {
		t.Errorf("BaseURL override lost: %q", src.opts.BaseURL)
	}
	if src.opts.AbsURL != "https://example.com/abs" {
		t.Errorf("AbsURL override lost: %q", src.opts.AbsURL)
	}
	if src.opts.PDFURL != "https://example.com/pdf" {
		t.Errorf("PDFURL override lost: %q", src.opts.PDFURL)
	}
	if src.opts.MaxResults != 7 {
		t.Errorf("MaxResults override lost: %d", src.opts.MaxResults)
	}
	if src.opts.HTTPTimeout != 5*time.Second {
		t.Errorf("HTTPTimeout override lost: %v", src.opts.HTTPTimeout)
	}
	if !src.opts.IncludePDF {
		t.Errorf("IncludePDF override lost")
	}
	if !equalStrings(src.opts.Categories, []string{"cs.CL"}) {
		t.Errorf("Categories override lost: %v", src.opts.Categories)
	}
}

func TestArxivSource_DefaultCategoriesPerEcosystem(t *testing.T) {
	cases := []struct {
		eco  ecosystem.Ecosystem
		want []string
	}{
		{ecosystem.EcoGo, []string{"cs.PL", "cs.SE"}},
		{ecosystem.EcoPython, []string{"cs.PL", "cs.ML", "stat.ML"}},
		{ecosystem.EcoTypeScript, []string{"cs.PL", "cs.HC"}},
		{ecosystem.EcoRust, []string{"cs.PL", "cs.OS", "cs.DC"}},
		{ecosystem.Ecosystem("unknown"), []string{"cs.PL"}},
	}
	for _, tc := range cases {
		t.Run(string(tc.eco), func(t *testing.T) {
			got := defaultCategoriesFor(tc.eco)
			if !equalStrings(got, tc.want) {
				t.Errorf("defaultCategoriesFor(%q) = %v; want %v", tc.eco, got, tc.want)
			}
		})
	}
}

func TestArxivSource_FetchManifest_OK(t *testing.T) {
	rv := &stubRevalidator{urls: map[string][]byte{
		"https://export.arxiv.org/api/query?search_query=cat:cs.PL+OR+cat:cs.SE&start=0&max_results=2000&sortBy=submittedDate&sortOrder=descending": arxivAtomXML,
	}}
	src := NewArxivSource(ArxivOptions{
		Revalidator: rv,
		Ecosystem:   ecosystem.EcoGo,
		Categories:  []string{"cs.PL", "cs.SE"},
	})
	mf, err := src.FetchManifest(context.Background())
	if err != nil {
		t.Fatalf("FetchManifest: %v", err)
	}
	if len(mf.Packages) != 2 {
		t.Fatalf("Packages = %d; want 2 (entry count)", len(mf.Packages))
	}

	if mf.Packages[0].Name != "2506.15655" {
		t.Errorf("Packages[0].Name = %q; want 2506.15655", mf.Packages[0].Name)
	}
	if mf.Packages[0].UpstreamURL != "https://arxiv.org/abs/2506.15655" {
		t.Errorf("Packages[0].UpstreamURL = %q; want https://arxiv.org/abs/2506.15655",
			mf.Packages[0].UpstreamURL)
	}
	wantTS := time.Date(2025, 6, 20, 0, 0, 0, 0, time.UTC)
	if !mf.Packages[0].LastUpdated.Equal(wantTS) {
		t.Errorf("Packages[0].LastUpdated = %v; want %v", mf.Packages[0].LastUpdated, wantTS)
	}
	if mf.Packages[1].Name != "2401.00396" {
		t.Errorf("Packages[1].Name = %q; want 2401.00396", mf.Packages[1].Name)
	}
}

func TestArxivSource_FetchManifest_NilRevalidator(t *testing.T) {
	src := NewArxivSource(ArxivOptions{Ecosystem: ecosystem.EcoGo})
	_, err := src.FetchManifest(context.Background())
	if err == nil || !strings.Contains(err.Error(), "nil Revalidator") {
		t.Errorf("err = %v; want contains 'nil Revalidator'", err)
	}
}

func TestArxivSource_FetchManifest_ErrorPropagates(t *testing.T) {
	rv := &stubRevalidator{urlsErr: map[string]error{
		"https://export.arxiv.org/api/query?search_query=cat:cs.PL+OR+cat:cs.SE&start=0&max_results=2000&sortBy=submittedDate&sortOrder=descending": errors.New("net down"),
	}}
	src := NewArxivSource(ArxivOptions{
		Revalidator: rv,
		Ecosystem:   ecosystem.EcoGo,
		Categories:  []string{"cs.PL", "cs.SE"},
	})
	_, err := src.FetchManifest(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "fetch query") {
		t.Errorf("err = %v; want contains 'fetch query'", err)
	}
}

func TestArxivSource_FetchManifest_MalformedXML(t *testing.T) {
	rv := &stubRevalidator{urls: map[string][]byte{
		"https://export.arxiv.org/api/query?search_query=cat:cs.PL+OR+cat:cs.SE&start=0&max_results=2000&sortBy=submittedDate&sortOrder=descending": []byte("<<<not xml>>>"),
	}}
	src := NewArxivSource(ArxivOptions{
		Revalidator: rv,
		Ecosystem:   ecosystem.EcoGo,
		Categories:  []string{"cs.PL", "cs.SE"},
	})
	_, err := src.FetchManifest(context.Background())
	if err == nil {
		t.Fatal("expected XML-parse error")
	}
	if !strings.Contains(err.Error(), "parse Atom") {
		t.Errorf("err = %v; want contains 'parse Atom'", err)
	}
}

func TestArxivSource_FetchPackageDoc_OK(t *testing.T) {
	abs := []byte("<html><body><h1>cAST Paper</h1><p>Abstract: This paper proposes...</p></body></html>")
	rv := &stubRevalidator{urls: map[string][]byte{
		"https://arxiv.org/abs/2506.15655": abs,
	}}
	src := NewArxivSource(ArxivOptions{Revalidator: rv, Ecosystem: ecosystem.EcoGo})
	pkg := ecosystem.PackageRef{
		Ecosystem: ecosystem.EcoGo, Name: "2506.15655", CanonicalNamespace: "arxiv:2506.15655",
		UpstreamURL: "https://arxiv.org/abs/2506.15655",
	}
	doc, err := src.FetchPackageDoc(context.Background(), pkg)
	if err != nil {
		t.Fatalf("FetchPackageDoc: %v", err)
	}
	if doc == nil {
		t.Fatal("doc nil")
	}
	if len(doc.Sections) == 0 {
		t.Fatal("expected sections")
	}

	if len(doc.Sections) != 1 {
		t.Errorf("Sections = %d; want 1 (Abstract only when IncludePDF=false)", len(doc.Sections))
	}
	s := doc.Sections[0]
	if s.Kind != ecosystem.KindGuide {
		t.Errorf("Sections[0].Kind = %s; want guide", s.Kind)
	}
	if s.SymbolPath != "arxiv:2506.15655" {
		t.Errorf("Sections[0].SymbolPath = %q; want arxiv:2506.15655", s.SymbolPath)
	}
	if s.Heading != "Abstract" {
		t.Errorf("Sections[0].Heading = %q; want Abstract", s.Heading)
	}
	if !bytes.Equal([]byte(s.Body), abs) {
		t.Errorf("Sections[0].Body mismatch with abstract HTML")
	}
	if s.SourceURL != "https://arxiv.org/abs/2506.15655" {
		t.Errorf("Sections[0].SourceURL = %q", s.SourceURL)
	}
	if s.ASTNodeType != "document" {
		t.Errorf("Sections[0].ASTNodeType = %q; want document", s.ASTNodeType)
	}
	if doc.Version != "v1" {
		t.Errorf("Version = %q; want v1", doc.Version)
	}
	if doc.SourceURL != "https://arxiv.org/abs/2506.15655" {
		t.Errorf("doc.SourceURL = %q", doc.SourceURL)
	}
	if !bytes.Equal([]byte(doc.RawBody), abs) {
		t.Errorf("doc.RawBody mismatch with abstract HTML")
	}
}

func TestArxivSource_FetchPackageDoc_NilRevalidator(t *testing.T) {
	src := NewArxivSource(ArxivOptions{Ecosystem: ecosystem.EcoGo})
	pkg := ecosystem.PackageRef{Ecosystem: ecosystem.EcoGo, Name: "x"}
	_, err := src.FetchPackageDoc(context.Background(), pkg)
	if err == nil || !strings.Contains(err.Error(), "nil Revalidator") {
		t.Errorf("err = %v; want contains 'nil Revalidator'", err)
	}
}

func TestArxivSource_FetchPackageDoc_AbsError(t *testing.T) {
	rv := &stubRevalidator{urlsErr: map[string]error{
		"https://arxiv.org/abs/foo": errors.New("abs 503"),
	}}
	src := NewArxivSource(ArxivOptions{Revalidator: rv, Ecosystem: ecosystem.EcoGo})
	pkg := ecosystem.PackageRef{Ecosystem: ecosystem.EcoGo, Name: "foo"}
	_, err := src.FetchPackageDoc(context.Background(), pkg)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "fetch abs") {
		t.Errorf("err = %v; want contains 'fetch abs'", err)
	}
}

func TestArxivSource_FetchPackageDoc_IncludePDF_Success(t *testing.T) {

	pdfBytes := mustGenerateMinimalPDF(t)
	abs := []byte("<html><body><h1>Paper</h1><p>Abstract</p></body></html>")
	rv := &stubRevalidator{urls: map[string][]byte{
		"https://arxiv.org/abs/2506.15655": abs,
		"https://arxiv.org/pdf/2506.15655": pdfBytes,
	}}
	src := NewArxivSource(ArxivOptions{
		Revalidator: rv, Ecosystem: ecosystem.EcoGo, IncludePDF: true,
	})
	pkg := ecosystem.PackageRef{
		Ecosystem: ecosystem.EcoGo, Name: "2506.15655", CanonicalNamespace: "arxiv:2506.15655",
		UpstreamURL: "https://arxiv.org/abs/2506.15655",
	}
	doc, err := src.FetchPackageDoc(context.Background(), pkg)
	if err != nil {
		t.Fatalf("FetchPackageDoc: %v", err)
	}

	if len(doc.Sections) != 2 {
		t.Fatalf("Sections = %d; want 2 (Abstract + Full Paper when PDF parses)", len(doc.Sections))
	}
	pdfSec := doc.Sections[1]
	if pdfSec.Heading != "Full Paper" {
		t.Errorf("Sections[1].Heading = %q; want 'Full Paper'", pdfSec.Heading)
	}
	if pdfSec.SourceURL != "https://arxiv.org/pdf/2506.15655" {
		t.Errorf("Sections[1].SourceURL = %q", pdfSec.SourceURL)
	}
	if pdfSec.SymbolPath != "arxiv:2506.15655" {
		t.Errorf("Sections[1].SymbolPath = %q", pdfSec.SymbolPath)
	}
	if pdfSec.Kind != ecosystem.KindGuide {
		t.Errorf("Sections[1].Kind = %s; want guide", pdfSec.Kind)
	}
	if pdfSec.ASTNodeType != "document" {
		t.Errorf("Sections[1].ASTNodeType = %q; want document", pdfSec.ASTNodeType)
	}
	if pdfSec.Body == "" {
		t.Errorf("Sections[1].Body empty; want extracted PDF text")
	}
}

func TestArxivSource_FetchPackageDoc_IncludePDF_FetchError(t *testing.T) {

	abs := []byte("<html><body><h1>Paper</h1><p>Abstract</p></body></html>")
	rv := &stubRevalidator{
		urls: map[string][]byte{
			"https://arxiv.org/abs/foo": abs,
		},
		urlsErr: map[string]error{
			"https://arxiv.org/pdf/foo": errors.New("pdf 404"),
		},
	}
	src := NewArxivSource(ArxivOptions{
		Revalidator: rv, Ecosystem: ecosystem.EcoGo, IncludePDF: true,
	})
	pkg := ecosystem.PackageRef{Ecosystem: ecosystem.EcoGo, Name: "foo"}
	doc, err := src.FetchPackageDoc(context.Background(), pkg)
	if err != nil {
		t.Fatalf("FetchPackageDoc: %v (PDF fetch error must NOT propagate)", err)
	}
	if len(doc.Sections) != 1 {
		t.Errorf("Sections = %d; want 1 (PDF skipped on fetch error)", len(doc.Sections))
	}
}

func TestArxivSource_FetchPackageDoc_IncludePDF_ParseFails(t *testing.T) {

	abs := []byte("<html><body><h1>Paper</h1><p>Abstract</p></body></html>")
	rv := &stubRevalidator{urls: map[string][]byte{
		"https://arxiv.org/abs/foo": abs,
		"https://arxiv.org/pdf/foo": []byte("not a pdf"),
	}}
	src := NewArxivSource(ArxivOptions{
		Revalidator: rv, Ecosystem: ecosystem.EcoGo, IncludePDF: true,
	})
	pkg := ecosystem.PackageRef{Ecosystem: ecosystem.EcoGo, Name: "foo"}
	doc, err := src.FetchPackageDoc(context.Background(), pkg)
	if err != nil {
		t.Fatalf("FetchPackageDoc: %v (PDF parse error must NOT propagate)", err)
	}
	if len(doc.Sections) != 1 {
		t.Errorf("Sections = %d; want 1 (PDF section skipped on parse error)", len(doc.Sections))
	}
}

func TestArxivSource_FetchPackageDoc_IncludePDF_EmptyBody(t *testing.T) {

	abs := []byte("<html><body><h1>Paper</h1><p>Abstract</p></body></html>")
	rv := &stubRevalidator{urls: map[string][]byte{
		"https://arxiv.org/abs/foo": abs,
		"https://arxiv.org/pdf/foo": []byte{},
	}}
	src := NewArxivSource(ArxivOptions{
		Revalidator: rv, Ecosystem: ecosystem.EcoGo, IncludePDF: true,
	})
	pkg := ecosystem.PackageRef{Ecosystem: ecosystem.EcoGo, Name: "foo"}
	doc, err := src.FetchPackageDoc(context.Background(), pkg)
	if err != nil {
		t.Fatalf("FetchPackageDoc: %v", err)
	}
	if len(doc.Sections) != 1 {
		t.Errorf("Sections = %d; want 1 (empty PDF body → skip)", len(doc.Sections))
	}
}

func TestArxivSource_FetchChangelog_NotAvailable(t *testing.T) {
	src := NewArxivSource(ArxivOptions{
		Revalidator: &stubRevalidator{}, Ecosystem: ecosystem.EcoGo,
	})
	pkg := ecosystem.PackageRef{
		Ecosystem: ecosystem.EcoGo, Name: "2506.15655", CanonicalNamespace: "x",
	}
	cl, err := src.FetchChangelog(context.Background(), pkg, "v1")
	if err != nil {
		t.Fatalf("FetchChangelog: %v", err)
	}
	if cl.FormatDetected != "not-available" {
		t.Errorf("FormatDetected = %s; arxiv papers immutable per ADR-0067", cl.FormatDetected)
	}
	if cl.VersionTo != "v1" {
		t.Errorf("VersionTo = %q; want v1", cl.VersionTo)
	}
	if cl.Package.Name != "2506.15655" {
		t.Errorf("Package.Name = %q; want round-trip", cl.Package.Name)
	}
}

func TestBuildArxivQuery(t *testing.T) {
	cases := []struct {
		cats []string
		want string
	}{
		{[]string{"cs.PL"}, "cat:cs.PL"},
		{[]string{"cs.PL", "cs.SE"}, "cat:cs.PL+OR+cat:cs.SE"},
		{[]string{"cs.PL", "cs.ML", "stat.ML"}, "cat:cs.PL+OR+cat:cs.ML+OR+cat:stat.ML"},
	}
	for _, tc := range cases {
		got := buildArxivQuery(tc.cats)
		if got != tc.want {
			t.Errorf("buildArxivQuery(%v) = %q; want %q", tc.cats, got, tc.want)
		}
	}
}

func TestExtractArxivID(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"http://arxiv.org/abs/2506.15655v1", "2506.15655"},
		{"http://arxiv.org/abs/2401.00396v12", "2401.00396"},
		{"https://arxiv.org/abs/2506.15655v3", "2506.15655"},
		{"http://arxiv.org/abs/2506.15655", "2506.15655"},

		{"http://arxiv.org/abs/2506.15655vX", "2506.15655vX"},
		{"http://arxiv.org/abs/foo", "foo"},
	}
	for _, tc := range cases {
		got := extractArxivID(tc.in)
		if got != tc.want {
			t.Errorf("extractArxivID(%q) = %q; want %q", tc.in, got, tc.want)
		}
	}
}

func TestIsAllDigits(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", false},
		{"123", true},
		{"0", true},
		{"1a", false},
		{"a1", false},
		{"-1", false},
	}
	for _, tc := range cases {
		got := isAllDigits(tc.in)
		if got != tc.want {
			t.Errorf("isAllDigits(%q) = %v; want %v", tc.in, got, tc.want)
		}
	}
}

func TestParseArxivAtom_OK(t *testing.T) {
	entries, err := parseArxivAtom(arxivAtomXML)
	if err != nil {
		t.Fatalf("parseArxivAtom: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %d; want 2", len(entries))
	}
	if entries[0].ID != "http://arxiv.org/abs/2506.15655v1" {
		t.Errorf("entries[0].ID = %q", entries[0].ID)
	}
	if !strings.Contains(entries[0].Title, "cAST") {
		t.Errorf("entries[0].Title = %q; want contains cAST", entries[0].Title)
	}
	if len(entries[0].Authors) != 2 {
		t.Errorf("entries[0].Authors = %d; want 2", len(entries[0].Authors))
	}
}

func TestParseArxivAtom_Malformed(t *testing.T) {
	_, err := parseArxivAtom([]byte("<<<garbage>>>"))
	if err == nil {
		t.Fatal("expected XML parse error")
	}
}

func TestExtractPDFText_BadBytes(t *testing.T) {
	_, err := extractPDFText([]byte("not a pdf"))
	if err == nil {
		t.Fatal("expected error for non-PDF input")
	}
}

func TestExtractPDFText_Success(t *testing.T) {
	pdfBytes := mustGenerateMinimalPDF(t)
	text, err := extractPDFText(pdfBytes)
	if err != nil {
		t.Fatalf("extractPDFText: %v", err)
	}
	if text == "" {
		t.Errorf("text empty; want extracted content")
	}
}

// equalStrings is a small slice equality helper local to this test file so
// we do not depend on reflect.DeepEqual for primitive slices.
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func mustGenerateMinimalPDF(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /Resources << /Font << /F1 4 0 R >> >> /MediaBox [0 0 612 792] /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	stream := "BT\n/F1 24 Tf\n100 700 Td\n(Hello arXiv) Tj\nET\n"
	streamObj := "<< /Length " + itoa(len(stream)) + " >>\nstream\n" + stream + "endstream"
	objs = append(objs, streamObj)

	buf.WriteString("%PDF-1.4\n")

	buf.WriteByte('%')
	buf.Write([]byte{0xe2, 0xe3, 0xcf, 0xd3})
	buf.WriteByte('\n')

	offsets := make([]int, len(objs)+1)
	offsets[0] = 0
	for i, payload := range objs {
		offsets[i+1] = buf.Len()
		buf.WriteString(itoa(i+1) + " 0 obj\n")
		buf.WriteString(payload)
		buf.WriteString("\nendobj\n")
	}

	xrefStart := buf.Len()
	buf.WriteString("xref\n")
	buf.WriteString("0 " + itoa(len(objs)+1) + "\n")
	buf.WriteString("0000000000 65535 f \n")
	for i := 1; i <= len(objs); i++ {
		buf.WriteString(padOffset(offsets[i]) + " 00000 n \n")
	}

	buf.WriteString("trailer\n")
	buf.WriteString("<< /Size " + itoa(len(objs)+1) + " /Root 1 0 R >>\n")
	buf.WriteString("startxref\n")
	buf.WriteString(itoa(xrefStart) + "\n")
	buf.WriteString("%%EOF\n")

	if _, err := pdf.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len())); err != nil {
		t.Fatalf("mustGenerateMinimalPDF: pdf.NewReader rejected generated bytes: %v", err)
	}
	return buf.Bytes()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

func padOffset(n int) string {
	s := itoa(n)
	for len(s) < 10 {
		s = "0" + s
	}
	return s
}
