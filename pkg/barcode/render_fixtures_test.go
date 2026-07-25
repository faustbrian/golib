package barcode_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"flag"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/barcode/aztec"
	"github.com/faustbrian/golib/pkg/barcode/barcode"
	"github.com/faustbrian/golib/pkg/barcode/codabar"
	"github.com/faustbrian/golib/pkg/barcode/code128"
	"github.com/faustbrian/golib/pkg/barcode/code39"
	"github.com/faustbrian/golib/pkg/barcode/code93"
	"github.com/faustbrian/golib/pkg/barcode/datamatrix"
	"github.com/faustbrian/golib/pkg/barcode/ean"
	"github.com/faustbrian/golib/pkg/barcode/gs1"
	"github.com/faustbrian/golib/pkg/barcode/imagedecode"
	"github.com/faustbrian/golib/pkg/barcode/itf"
	"github.com/faustbrian/golib/pkg/barcode/pdf417"
	"github.com/faustbrian/golib/pkg/barcode/qr"
	"github.com/faustbrian/golib/pkg/barcode/render"
	"github.com/faustbrian/golib/pkg/barcode/upc"
)

var updateRenderFixtures = flag.Bool(
	"update-render-fixtures",
	false,
	"rewrite deterministic rendering fixture files",
)

const renderFixtureHeader = "id\tformat\tpayload_class\tmodule_scale\tcorrection\tlogical_sha256\tpng_sha256\tsvg_sha256\tpng\tsvg"

type renderFixture struct {
	id           string
	format       barcode.Format
	payloadClass string
	scale        int
	correction   string
	encode       func() (barcode.Symbol, error)
}

type renderedFixture struct {
	fixture renderFixture
	symbol  barcode.Symbol
	logical []byte
	png     []byte
	svg     []byte
}

func TestRenderFixtureGoldensCoverEveryFormat(t *testing.T) {
	fixtures := renderFixtures(t)
	wantFormats := barcode.Formats()
	gotFormats := make([]barcode.Format, 0, len(fixtures))
	seen := make(map[barcode.Format]bool, len(fixtures))
	for _, fixture := range fixtures {
		if seen[fixture.format] {
			t.Fatalf("duplicate render fixture format %q", fixture.format)
		}
		seen[fixture.format] = true
		gotFormats = append(gotFormats, fixture.format)
	}
	for _, format := range wantFormats {
		if !seen[format] {
			t.Fatalf("render fixture for %q is missing", format)
		}
	}
	if len(gotFormats) != len(wantFormats) {
		t.Fatalf("render fixture formats = %v, want %v", gotFormats, wantFormats)
	}

	rendered := make([]renderedFixture, 0, len(fixtures))
	for _, fixture := range fixtures {
		rendered = append(rendered, buildRenderedFixture(t, fixture))
	}
	if *updateRenderFixtures {
		writeRenderFixtures(t, rendered)
	}
	verifyRenderFixtures(t, rendered)
}

func renderFixtures(t *testing.T) []renderFixture {
	t.Helper()
	elements, err := gs1.ParseBracketed(
		"(01)09501101530003(10)RENDER26",
		gs1.ParseLimits{MaxInputBytes: 128, MaxElements: 8},
	)
	if err != nil {
		t.Fatalf("ParseBracketed() error = %v", err)
	}

	return []renderFixture{
		matrixFixture("qr", barcode.QRCode, "alphanumeric", 8, "H", func() (barcode.Symbol, error) {
			symbol, encodeErr := qr.Encode([]byte("RENDER-QR-2026"), qr.Options{ErrorCorrection: qr.High})
			return symbol.Logical(), encodeErr
		}),
		linearFixture("code128", barcode.Code128, "mixed", func() (barcode.Symbol, error) {
			return code128.Encode([]byte("RENDER-C128-2026"), code128.Options{})
		}),
		linearFixture("gs1-128", barcode.GS1128, "gs1", func() (barcode.Symbol, error) {
			return code128.EncodeGS1(elements, code128.Options{})
		}),
		linearFixture("code39", barcode.Code39, "full-ascii", func() (barcode.Symbol, error) {
			return code39.Encode([]byte("Render-39"), code39.Options{Checksum: true})
		}),
		linearFixture("code93", barcode.Code93, "full-ascii", func() (barcode.Symbol, error) {
			return code93.Encode([]byte("Render-93"), code93.Options{})
		}),
		linearFixture("ean8", barcode.EAN8, "retail", func() (barcode.Symbol, error) {
			return ean.Encode8("5512345", ean.Options{Supplement: "12"})
		}),
		linearFixture("ean13", barcode.EAN13, "retail", func() (barcode.Symbol, error) {
			return ean.Encode13("590123412345", ean.Options{Supplement: "51234"})
		}),
		linearFixture("upca", barcode.UPCA, "retail", func() (barcode.Symbol, error) {
			return upc.EncodeA("04210000526", upc.Options{Supplement: "12"})
		}),
		linearFixture("upce", barcode.UPCE, "retail", func() (barcode.Symbol, error) {
			return upc.EncodeE("0123456", upc.Options{Supplement: "51234"})
		}),
		linearFixture("itf", barcode.ITF, "numeric", func() (barcode.Symbol, error) {
			return itf.Encode("1234567890", itf.Options{})
		}),
		matrixFixture("itf14", barcode.ITF14, "gs1", 4, "check-digit", func() (barcode.Symbol, error) {
			return itf.Encode14("0950110153000", itf.ITF14Options{})
		}),
		linearFixture("codabar", barcode.Codabar, "numeric", func() (barcode.Symbol, error) {
			return codabar.Encode([]byte("1234-56"), codabar.Options{Start: 'A', Stop: 'D'})
		}),
		matrixFixture("datamatrix", barcode.DataMatrix, "byte", 8, "ECC-200", func() (barcode.Symbol, error) {
			symbol, encodeErr := datamatrix.Encode([]byte("INDEPENDENT-DM"), datamatrix.Options{})
			return symbol.Logical(), encodeErr
		}),
		matrixFixture("pdf417", barcode.PDF417, "byte", 4, "level-4", func() (barcode.Symbol, error) {
			symbol, encodeErr := pdf417.Encode([]byte("RENDER-PDF417-2026"), pdf417.Options{ErrorCorrection: pdf417.Level4})
			return symbol.Logical(), encodeErr
		}),
		matrixFixture("aztec", barcode.Aztec, "byte", 8, "40-percent", func() (barcode.Symbol, error) {
			symbol, encodeErr := aztec.Encode([]byte("RENDER-AZTEC-2026"), aztec.Options{ErrorCorrectionPercent: 40})
			return symbol.Logical(), encodeErr
		}),
	}
}

func linearFixture(id string, format barcode.Format, payloadClass string, encode func() (barcode.Symbol, error)) renderFixture {
	return renderFixture{
		id: id, format: format, payloadClass: payloadClass, scale: 4,
		correction: "checksum", encode: encode,
	}
}

func matrixFixture(id string, format barcode.Format, payloadClass string, scale int, correction string, encode func() (barcode.Symbol, error)) renderFixture {
	return renderFixture{
		id: id, format: format, payloadClass: payloadClass, scale: scale,
		correction: correction, encode: encode,
	}
}

func buildRenderedFixture(t *testing.T, fixture renderFixture) renderedFixture {
	t.Helper()
	symbol, err := fixture.encode()
	if err != nil {
		t.Fatalf("encode %s: %v", fixture.id, err)
	}
	if symbol.Format() != fixture.format {
		t.Fatalf("encode %s format = %q, want %q", fixture.id, symbol.Format(), fixture.format)
	}
	var pngOutput, svgOutput bytes.Buffer
	options := render.Options{Scale: fixture.scale}
	if err = render.PNG(&pngOutput, symbol, options); err != nil {
		t.Fatalf("render %s PNG: %v", fixture.id, err)
	}
	if err = render.SVG(&svgOutput, symbol, options); err != nil {
		t.Fatalf("render %s SVG: %v", fixture.id, err)
	}

	return renderedFixture{
		fixture: fixture,
		symbol:  symbol,
		logical: logicalRenderFixture(symbol),
		png:     pngOutput.Bytes(),
		svg:     svgOutput.Bytes(),
	}
}

func logicalRenderFixture(symbol barcode.Symbol) []byte {
	var output bytes.Buffer
	if bars, ok := symbol.Bars(); ok {
		fmt.Fprintf(&output, "%s\tbars\t%d\t%d\n", symbol.Format(), bars.Width(), bars.Height())
		for _, run := range bars.Runs() {
			if run.Dark {
				output.WriteByte('1')
			} else {
				output.WriteByte('0')
			}
			fmt.Fprintf(&output, ":%d\n", run.Width)
		}
		return output.Bytes()
	}
	matrix := symbol.Matrix()
	fmt.Fprintf(&output, "%s\tmatrix\t%d\t%d\n", symbol.Format(), matrix.Width(), matrix.Height())
	for y := 0; y < matrix.Height(); y++ {
		for x := 0; x < matrix.Width(); x++ {
			if matrix.At(x, y) {
				output.WriteByte('1')
			} else {
				output.WriteByte('0')
			}
		}
		output.WriteByte('\n')
	}
	return output.Bytes()
}

func writeRenderFixtures(t *testing.T, fixtures []renderedFixture) {
	t.Helper()
	directory := filepath.Join("specification", "render-fixtures")
	// #nosec G301 -- checked-in fixture directories are intentionally world-readable.
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatalf("create render fixture directory: %v", err)
	}
	var manifest bytes.Buffer
	manifest.WriteString(renderFixtureHeader + "\n")
	for _, rendered := range fixtures {
		pngName := rendered.fixture.id + ".png"
		svgName := rendered.fixture.id + ".svg"
		// #nosec G306 -- checked-in fixture artifacts are intentionally world-readable.
		if err := os.WriteFile(filepath.Join(directory, pngName), rendered.png, 0o644); err != nil {
			t.Fatalf("write %s: %v", pngName, err)
		}
		// #nosec G306 -- checked-in fixture artifacts are intentionally world-readable.
		if err := os.WriteFile(filepath.Join(directory, svgName), rendered.svg, 0o644); err != nil {
			t.Fatalf("write %s: %v", svgName, err)
		}
		fmt.Fprintf(&manifest, "%s\t%s\t%s\t%d\t%s\t%s\t%s\t%s\t%s\t%s\n",
			rendered.fixture.id, rendered.fixture.format, rendered.fixture.payloadClass,
			rendered.fixture.scale, rendered.fixture.correction,
			sha256Hex(rendered.logical), sha256Hex(rendered.png), sha256Hex(rendered.svg),
			pngName, svgName,
		)
	}
	// #nosec G306 -- the checked-in fixture manifest is intentionally world-readable.
	if err := os.WriteFile("specification/render-fixtures.tsv", manifest.Bytes(), 0o644); err != nil {
		t.Fatalf("write render fixture manifest: %v", err)
	}
}

func verifyRenderFixtures(t *testing.T, fixtures []renderedFixture) {
	t.Helper()
	manifest, err := os.Open("specification/render-fixtures.tsv")
	if err != nil {
		t.Fatalf("open render fixture manifest: %v", err)
	}
	reader := csv.NewReader(manifest)
	reader.Comma = '\t'
	records, err := reader.ReadAll()
	closeErr := manifest.Close()
	if err != nil {
		t.Fatalf("read render fixture manifest: %v", err)
	}
	if closeErr != nil {
		t.Fatalf("close render fixture manifest: %v", closeErr)
	}
	if len(records) != len(fixtures)+1 || !slices.Equal(records[0], strings.Split(renderFixtureHeader, "\t")) {
		t.Fatalf("render fixture manifest has invalid header or row count")
	}
	for index, rendered := range fixtures {
		record := records[index+1]
		want := []string{
			rendered.fixture.id, string(rendered.fixture.format), rendered.fixture.payloadClass,
			strconv.Itoa(rendered.fixture.scale), rendered.fixture.correction,
			sha256Hex(rendered.logical), sha256Hex(rendered.png), sha256Hex(rendered.svg),
			rendered.fixture.id + ".png", rendered.fixture.id + ".svg",
		}
		if !slices.Equal(record, want) {
			t.Fatalf("render fixture row %d = %v, want %v", index+2, record, want)
		}
		for _, asset := range []struct {
			name string
			want []byte
		}{{record[8], rendered.png}, {record[9], rendered.svg}} {
			contents, readErr := os.ReadFile(filepath.Join("specification", "render-fixtures", asset.name))
			if readErr != nil {
				t.Fatalf("read render fixture %s: %v", asset.name, readErr)
			}
			if !bytes.Equal(contents, asset.want) {
				t.Fatalf("render fixture %s differs from deterministic render", asset.name)
			}
		}
		input, decodeErr := png.Decode(bytes.NewReader(rendered.png))
		if decodeErr != nil {
			t.Fatalf("decode render fixture %s PNG: %v", rendered.fixture.id, decodeErr)
		}
		result, decodeErr := imagedecode.Decode(context.Background(), input, imagedecode.Options{
			Formats:              []barcode.Format{rendered.fixture.format},
			AssumeCode39Checksum: rendered.fixture.format == barcode.Code39,
		})
		if decodeErr != nil {
			t.Fatalf("decode render fixture %s: %v", rendered.fixture.id, decodeErr)
		}
		if result.Format() != rendered.fixture.format ||
			!bytes.Equal(result.Payload(), rendered.symbol.Payload()) {
			t.Fatalf("decode render fixture %s = (%q, %q), want (%q, %q)",
				rendered.fixture.id, result.Format(), result.Payload(),
				rendered.fixture.format, rendered.symbol.Payload())
		}
		if rendered.fixture.format == barcode.GS1128 &&
			!slices.Contains(result.Diagnostics(), "SYMBOLOGY_IDENTIFIER=]C1") {
			t.Fatalf("decode render fixture %s diagnostics = %v, missing GS1 identifier",
				rendered.fixture.id, result.Diagnostics())
		}
	}
}

func sha256Hex(input []byte) string {
	sum := sha256.Sum256(input)
	return hex.EncodeToString(sum[:])
}
