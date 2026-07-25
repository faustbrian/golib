package jsonapi

import (
	"encoding/json"
	"errors"
	"math"
	"testing"
)

func TestCursorPageMetaBuildsCanonicalProfileMetadata(t *testing.T) {
	t.Parallel()

	total := int64(200)
	bestGuess := int64(210)
	rangeTruncated := true
	meta, err := (CursorPageMeta{
		RangeTruncated: &rangeTruncated,
		Total:          &total,
		EstimatedTotal: &CursorEstimatedTotal{BestGuess: &bestGuess},
	}).Meta()
	if err != nil {
		t.Fatalf("build cursor page meta: %v", err)
	}

	payload, err := Marshal(Document{Data: ResourceCollection(), Meta: meta})
	if err != nil {
		t.Fatalf("marshal document: %v", err)
	}
	want := `{"data":[],"meta":{"page":{"rangeTruncated":true,"total":200,"estimatedTotal":{"bestGuess":210}}}}`
	if string(payload) != want {
		t.Fatalf("unexpected metadata:\n got: %s\nwant: %s", payload, want)
	}
}

func TestCursorMetadataSupportsAnAliasedPageMember(t *testing.T) {
	t.Parallel()

	total := int64(7)
	meta, err := (CursorPageMeta{Total: &total}).MetaAs("cursorPage")
	if err != nil {
		t.Fatalf("build aliased page metadata: %v", err)
	}
	itemMeta, err := CursorItemMetaAs("cursorPage", "opaque")
	if err != nil {
		t.Fatalf("build aliased item metadata: %v", err)
	}
	meta["item"] = itemMeta["cursorPage"]

	metadata, present, err := ParseCursorPageMetaAs(meta, "cursorPage")
	if err != nil || !present || metadata.Total == nil || *metadata.Total != total {
		t.Fatalf("parse aliased page metadata: %#v present=%v err=%v", metadata, present, err)
	}
	cursor, present, err := ParseCursorItemMetaAs(
		Meta{"cursorPage": meta["item"]},
		"cursorPage",
	)
	if err != nil || !present || cursor != "opaque" {
		t.Fatalf("parse aliased item metadata: %q present=%v err=%v", cursor, present, err)
	}
}

func TestCursorMetadataRejectsAnInvalidPageMemberAlias(t *testing.T) {
	t.Parallel()

	if _, err := (CursorPageMeta{}).MetaAs("@page"); err == nil {
		t.Fatal("expected @ page member alias error")
	}
	if _, err := (CursorPageMeta{}).MetaAs("bad/name"); err == nil {
		t.Fatal("expected invalid page member alias error")
	}
	if _, err := CursorItemMetaAs("bad/name", "opaque"); err == nil {
		t.Fatal("expected invalid item member alias error")
	}
	if _, _, err := ParseCursorPageMetaAs(nil, "bad/name"); err == nil {
		t.Fatal("expected invalid page member parse error")
	}
	if _, _, err := ParseCursorItemMetaAs(nil, "bad/name"); err == nil {
		t.Fatal("expected invalid item member parse error")
	}
}

func TestCursorPageMetaPreservesEmptyEstimateObject(t *testing.T) {
	t.Parallel()

	meta, err := (CursorPageMeta{EstimatedTotal: &CursorEstimatedTotal{}}).Meta()
	if err != nil {
		t.Fatalf("build empty estimate: %v", err)
	}
	payload, err := Marshal(Document{Data: ResourceCollection(), Meta: meta})
	if err != nil {
		t.Fatalf("marshal document: %v", err)
	}
	want := `{"data":[],"meta":{"page":{"estimatedTotal":{}}}}`
	if string(payload) != want {
		t.Fatalf("unexpected metadata: got %s, want %s", payload, want)
	}
}

func TestParseCursorPageMetaFromDecodedDocument(t *testing.T) {
	t.Parallel()

	document, err := Unmarshal([]byte(`{
		"data":[],
		"meta":{"page":{"rangeTruncated":false,"total":200,"estimatedTotal":{"bestGuess":210}}}
	}`))
	if err != nil {
		t.Fatalf("decode document: %v", err)
	}
	metadata, present, err := ParseCursorPageMeta(document.Meta)
	if err != nil {
		t.Fatalf("parse cursor metadata: %v", err)
	}
	if !present || metadata.RangeTruncated == nil || *metadata.RangeTruncated ||
		metadata.Total == nil || *metadata.Total != 200 ||
		metadata.EstimatedTotal == nil || metadata.EstimatedTotal.BestGuess == nil ||
		*metadata.EstimatedTotal.BestGuess != 210 {
		t.Fatalf("unexpected parsed metadata: %#v", metadata)
	}
}

func TestCursorPageMetaRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	negative := int64(-1)
	if _, err := (CursorPageMeta{Total: &negative}).Meta(); err == nil {
		t.Fatal("expected negative total error")
	}
	if _, err := (CursorPageMeta{
		EstimatedTotal: &CursorEstimatedTotal{BestGuess: &negative},
	}).Meta(); err == nil {
		t.Fatal("expected negative estimate error")
	}

	tests := []struct {
		meta Meta
		path string
	}{
		{meta: Meta{"page": "invalid"}, path: "/meta/page"},
		{meta: Meta{"page": map[string]any{"total": 1.5}}, path: "/meta/page/total"},
		{meta: Meta{"page": map[string]any{"rangeTruncated": "yes"}}, path: "/meta/page/rangeTruncated"},
		{meta: Meta{"page": map[string]any{"estimatedTotal": 10}}, path: "/meta/page/estimatedTotal"},
		{meta: Meta{"page": map[string]any{"estimatedTotal": map[string]any{"bestGuess": -1.0}}}, path: "/meta/page/estimatedTotal/bestGuess"},
	}
	for _, test := range tests {
		_, _, err := ParseCursorPageMeta(test.meta)
		var validationError *ValidationError
		if !errors.As(err, &validationError) {
			t.Fatalf("expected ValidationError, got %T: %v", err, err)
		}
		if validationError.Violations[0].Path != test.path {
			t.Fatalf("unexpected violation: %#v", validationError.Violations[0])
		}
	}
}

func TestParseCursorMetadataAcceptsPackageAndMetaObjectRepresentations(t *testing.T) {
	t.Parallel()

	total := int64(20)
	bestGuess := int64(25)
	truncated := true
	meta, err := (CursorPageMeta{
		RangeTruncated: &truncated,
		Total:          &total,
		EstimatedTotal: &CursorEstimatedTotal{BestGuess: &bestGuess},
	}).Meta()
	if err != nil {
		t.Fatalf("build cursor metadata: %v", err)
	}
	parsed, present, err := ParseCursorPageMeta(meta)
	if err != nil || !present || parsed.Total == nil || *parsed.Total != total ||
		parsed.EstimatedTotal == nil || parsed.EstimatedTotal.BestGuess == nil ||
		*parsed.EstimatedTotal.BestGuess != bestGuess {
		t.Fatalf("unexpected package metadata parse: %#v present=%v err=%v", parsed, present, err)
	}

	parsed, present, err = ParseCursorPageMeta(Meta{"page": Meta{"total": int8(3)}})
	if err != nil || !present || parsed.Total == nil || *parsed.Total != 3 {
		t.Fatalf("unexpected Meta object parse: %#v present=%v err=%v", parsed, present, err)
	}

	emptyEstimate, err := (CursorPageMeta{
		EstimatedTotal: &CursorEstimatedTotal{},
	}).Meta()
	if err != nil {
		t.Fatalf("build empty estimate: %v", err)
	}
	parsed, _, err = ParseCursorPageMeta(emptyEstimate)
	if err != nil || parsed.EstimatedTotal == nil || parsed.EstimatedTotal.BestGuess != nil {
		t.Fatalf("empty estimate was not preserved: %#v err=%v", parsed, err)
	}
}

func TestCursorIntegerSupportsPublicMetaNumberRepresentations(t *testing.T) {
	t.Parallel()

	valid := []any{
		int(7), int8(7), int16(7), int32(7), int64(7),
		uint(7), uint8(7), uint16(7), uint32(7), uint64(7),
		float64(7), json.Number("7"), json.Number("7.0"), json.Number("7e0"),
	}
	for _, value := range valid {
		got, ok := cursorInteger(value)
		if !ok || got != 7 {
			t.Fatalf("integer representation %T was rejected: got %d ok=%v", value, got, ok)
		}
	}

	invalid := []any{
		uint64(math.MaxUint64),
		float64(1.5),
		math.Inf(1),
		json.Number("not-a-number"),
		json.Number("7.1"),
		json.Number("1/1"),
		json.Number("+7"),
		json.Number("1e999"),
		"7",
	}
	for _, value := range invalid {
		if got, ok := cursorInteger(value); ok {
			t.Fatalf("invalid representation %T was accepted as %d", value, got)
		}
	}
}

func TestCursorMetadataAcceptsIntegralJSONNumberForms(t *testing.T) {
	t.Parallel()

	document, err := Unmarshal([]byte(`{
		"data":[],
		"meta":{"page":{"total":1e2,"estimatedTotal":{"bestGuess":101.0}}}
	}`))
	if err != nil {
		t.Fatalf("decode document: %v", err)
	}
	metadata, present, err := ParseCursorPageMeta(document.Meta)
	if err != nil {
		t.Fatalf("parse cursor metadata: %v", err)
	}
	if !present || metadata.Total == nil || *metadata.Total != 100 ||
		metadata.EstimatedTotal == nil || metadata.EstimatedTotal.BestGuess == nil ||
		*metadata.EstimatedTotal.BestGuess != 101 {
		t.Fatalf("unexpected parsed metadata: %#v", metadata)
	}
}

func TestParseCursorItemMetaPresenceAndTypeSemantics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		meta    Meta
		present bool
		path    string
	}{
		{meta: nil, present: false},
		{meta: Meta{"page": Meta{}}, present: false},
		{meta: Meta{"page": "invalid"}, present: false, path: "/meta/page"},
		{meta: Meta{"page": map[string]any{"cursor": 7}}, present: true, path: "/meta/page/cursor"},
	}
	for _, test := range tests {
		_, present, err := ParseCursorItemMeta(test.meta)
		if present != test.present {
			t.Fatalf("unexpected presence for %#v: got %v", test.meta, present)
		}
		if test.path == "" && err != nil {
			t.Fatalf("unexpected item meta error: %v", err)
		}
		if test.path != "" {
			var validationError *ValidationError
			if !errors.As(err, &validationError) ||
				validationError.Violations[0].Path != test.path {
				t.Fatalf("unexpected item meta error: %T %#v", err, validationError)
			}
		}
	}
}

func TestCursorItemMetaRoundTrip(t *testing.T) {
	t.Parallel()

	resource := ResourceObject{
		Type: "articles",
		ID:   "1",
		Meta: CursorItemMeta("opaque"),
	}
	cursor, present, err := ParseCursorItemMeta(resource.Meta)
	if err != nil {
		t.Fatalf("parse item cursor: %v", err)
	}
	if !present || cursor != "opaque" {
		t.Fatalf("unexpected cursor: %q present=%v", cursor, present)
	}
}

func TestCursorPageMetaPreservesLargeDecodedIntegers(t *testing.T) {
	t.Parallel()

	document, err := Unmarshal([]byte(`{
		"data":[],
		"meta":{"page":{"total":9007199254740993}}
	}`))
	if err != nil {
		t.Fatalf("decode document: %v", err)
	}
	metadata, _, err := ParseCursorPageMeta(document.Meta)
	if err != nil {
		t.Fatalf("parse cursor metadata: %v", err)
	}
	if metadata.Total == nil || *metadata.Total != 9007199254740993 {
		t.Fatalf("large integer lost precision: %#v", metadata.Total)
	}
}
