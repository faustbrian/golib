package wkt

import (
	"errors"
	"math"
	"strings"
	"testing"

	geo "github.com/faustbrian/golib/pkg/geo"
)

func TestEWKTPrefixAndEncodedByteBoundaries(t *testing.T) {
	t.Parallel()

	valid := []byte("SRID=1;POINT (0 0)")
	limits := geo.DefaultLimits()
	limits.MaxEncodedBytes = int64(len(valid))
	geometry, err := UnmarshalEWKT(valid, limits)
	if err != nil {
		t.Fatalf("UnmarshalEWKT(exact byte limit) error = %v", err)
	}
	if geometry.CRS().SRID() != 1 {
		t.Fatalf("UnmarshalEWKT(exact byte limit) SRID = %d", geometry.CRS().SRID())
	}
	limits.MaxEncodedBytes--
	if _, err := UnmarshalEWKT(valid, limits); !errors.Is(err, geo.ErrEncoding) {
		t.Fatalf("UnmarshalEWKT(over byte limit) error = %v", err)
	}

	for _, input := range []string{
		"POINT (0 0)",
		";POINT (0 0)",
		"SRID=;POINT (0 0)",
	} {
		if _, err := UnmarshalEWKT([]byte(input), geo.DefaultLimits()); !errors.Is(err, geo.ErrEncoding) {
			t.Fatalf("UnmarshalEWKT(%q) error = %v", input, err)
		}
	}
}

func TestDepthCountdownRejectsExhaustedAndNegativeLimits(t *testing.T) {
	t.Parallel()

	data := []byte("POINT (0 0)")
	for _, remainingDepth := range []int{0, -1} {
		parser := parser{data: data, crs: geo.WGS84(), limits: geo.DefaultLimits()}
		if _, err := parser.geometry(remainingDepth); !errors.Is(err, geo.ErrTopology) {
			t.Fatalf("geometry(depth %d) error = %v", remainingDepth, err)
		}
	}
	limits := geo.DefaultLimits()
	limits.MaxCollectionDepth = -1
	if _, err := Unmarshal(data, geo.WGS84(), limits); !errors.Is(err, geo.ErrTopology) {
		t.Fatalf("Unmarshal(negative depth limit) error = %v", err)
	}
	parser := parser{
		data:   []byte("GEOMETRYCOLLECTION (POINT (0 0))"),
		crs:    geo.WGS84(),
		limits: geo.DefaultLimits(),
	}
	if _, err := parser.geometry(1); err == nil || !strings.Contains(err.Error(), "collection depth limit exceeded") {
		t.Fatalf("geometry(exhausted child depth) error = %v", err)
	}
}

func TestParserResourceLimitsReportTheOwningBoundary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		parse func(*parser) error
		want  string
	}{
		{
			name:  "coordinates",
			input: "0 0,1 1)",
			parse: func(parser *parser) error {
				parser.limits.MaxPoints = 1
				_, err := parser.coordinates()
				return err
			},
			want: "point limit exceeded",
		},
		{
			name:  "rings",
			input: "((0 0,1 0,0 0),(0 0,1 0,0 0))",
			parse: func(parser *parser) error {
				parser.limits.MaxRings = 1
				_, err := parser.ringList()
				return err
			},
			want: "ring limit exceeded",
		},
		{
			name:  "multi point",
			input: "((0 0),(1 1))",
			parse: func(parser *parser) error {
				parser.limits.MaxPoints = 1
				_, err := parser.multiPoint()
				return err
			},
			want: "point limit exceeded",
		},
		{
			name:  "multi line string",
			input: "((0 0,1 1),(2 2,3 3))",
			parse: func(parser *parser) error {
				parser.limits.MaxGeometries = 1
				_, err := parser.multiLineString()
				return err
			},
			want: "geometry limit exceeded",
		},
		{
			name:  "multi polygon",
			input: "(((0 0,1 0,1 1,0 0)),((0 0,1 0,1 1,0 0)))",
			parse: func(parser *parser) error {
				parser.limits.MaxGeometries = 1
				_, err := parser.multiPolygon()
				return err
			},
			want: "geometry limit exceeded",
		},
		{
			name:  "collection",
			input: "(POINT (0 0),POINT (1 1))",
			parse: func(parser *parser) error {
				parser.limits.MaxGeometries = 1
				_, err := parser.collection(parser.limits.MaxCollectionDepth)
				return err
			},
			want: "geometry limit exceeded",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			parser := parser{
				data:   []byte(test.input),
				crs:    geo.WGS84(),
				limits: geo.DefaultLimits(),
			}
			if err := test.parse(&parser); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("parse error = %v, want problem %q", err, test.want)
			}
		})
	}
}

func TestLexicalBoundariesAreASCIIAndDelimiterAware(t *testing.T) {
	t.Parallel()

	for _, value := range []byte{'A', 'Z', 'a', 'z'} {
		if !asciiLetter(value) {
			t.Fatalf("asciiLetter(%q) = false", value)
		}
	}
	for _, value := range []byte{'@', '[', '`', '{', '0', '_'} {
		if asciiLetter(value) {
			t.Fatalf("asciiLetter(%q) = true", value)
		}
	}

	for _, test := range []struct {
		input string
		want  string
	}{
		{input: "AZaz", want: "AZaz"},
		{input: "A0", want: "A"},
		{input: "Z[", want: "Z"},
		{input: "a`", want: "a"},
		{input: "z{", want: "z"},
		{input: "ŁPOINT", want: ""},
	} {
		parser := parser{data: []byte(test.input)}
		if got := parser.identifier(); got != test.want {
			t.Fatalf("identifier(%q) = %q, want %q", test.input, got, test.want)
		}
	}

	for _, suffix := range []byte{'A', 'Z', 'a', 'z'} {
		parser := parser{data: append([]byte("EMPTY"), suffix)}
		if parser.keyword("EMPTY") {
			t.Fatalf("keyword(EMPTY%c) = true", suffix)
		}
	}
	for _, suffix := range []byte{'0', '_', '@', '['} {
		parser := parser{data: append([]byte("EMPTY"), suffix)}
		if !parser.keyword("EMPTY") || parser.position != len("EMPTY") {
			t.Fatalf("keyword(EMPTY%c) did not consume the standalone keyword", suffix)
		}
	}
}

func TestNumberAndWhitespaceScannersPreserveExactPositions(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		input        string
		want         float64
		wantPosition int
	}{
		{input: " 12.5,", want: 12.5, wantPosition: 5},
		{input: "-2)", want: -2, wantPosition: 2},
		{input: "3.25", want: 3.25, wantPosition: 4},
	} {
		parser := parser{data: []byte(test.input)}
		got, err := parser.number()
		if err != nil || got != test.want || parser.position != test.wantPosition {
			t.Fatalf("number(%q) = %v, position %d, error %v", test.input, got, parser.position, err)
		}
	}

	withoutSpace := parser{data: []byte("x")}
	if withoutSpace.requiredSpace() {
		t.Fatal("requiredSpace(no space) = true")
	}
	withSpace := parser{data: []byte(" \t\n\rx")}
	if !withSpace.requiredSpace() || withSpace.position != 4 {
		t.Fatalf("requiredSpace(all spaces) = %t at %d", withSpace.position > 0, withSpace.position)
	}
}

func TestValidateDataUsesExactCRSAndByteBoundaries(t *testing.T) {
	t.Parallel()

	limits := geo.DefaultLimits()
	limits.MaxEncodedBytes = 2
	if err := validateData([]byte("12"), geo.WGS84(), limits); err != nil {
		t.Fatalf("validateData(exact limit) error = %v", err)
	}
	if err := validateData([]byte("123"), geo.WGS84(), limits); !errors.Is(err, geo.ErrEncoding) {
		t.Fatalf("validateData(over limit) error = %v", err)
	}
	if err := validateData(nil, geo.CRS{}, limits); !errors.Is(err, geo.ErrCRS) {
		t.Fatalf("validateData(zero CRS) error = %v", err)
	}
	projected, err := geo.NewCRS(math.MaxInt32, "EPSG:2147483647")
	if err != nil {
		t.Fatalf("NewCRS() error = %v", err)
	}
	if err := validateData(nil, projected, limits); err != nil {
		t.Fatalf("validateData(maximum CRS) error = %v", err)
	}
}
