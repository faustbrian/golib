package httpsignature

import (
	"bytes"
	"encoding/base32"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/dunglas/httpsfv"
)

const (
	httpwgRFC8941ParseFiles         = 18
	httpwgRFC8941ParseCases         = 1526
	httpwgRFC8941SerializationFiles = 4
	httpwgRFC8941SerializationCases = 544
)

type httpwgRFC8941Case struct {
	Name       string          `json:"name"`
	Raw        []string        `json:"raw"`
	HeaderType string          `json:"header_type"`
	Expected   json.RawMessage `json:"expected"`
	MustFail   bool            `json:"must_fail"`
	CanFail    bool            `json:"can_fail"`
	Canonical  *[]string       `json:"canonical"`
}

func TestHTTPWGRFC8941Corpus(t *testing.T) {
	corpusRoot := os.Getenv("HTTPWG_RFC8941_CORPUS")
	if corpusRoot == "" {
		t.Skip("HTTPWG_RFC8941_CORPUS is set by the conformance gate")
	}

	parseFiles, err := filepath.Glob(filepath.Join(corpusRoot, "*.json"))
	if err != nil {
		t.Fatalf("find parse corpus: %v", err)
	}
	serializationFiles, err := filepath.Glob(filepath.Join(corpusRoot, "serialisation-tests", "*.json"))
	if err != nil {
		t.Fatalf("find serialization corpus: %v", err)
	}
	if len(parseFiles) != httpwgRFC8941ParseFiles {
		t.Fatalf("parse corpus files = %d, want %d", len(parseFiles), httpwgRFC8941ParseFiles)
	}
	if len(serializationFiles) != httpwgRFC8941SerializationFiles {
		t.Fatalf("serialization corpus files = %d, want %d", len(serializationFiles), httpwgRFC8941SerializationFiles)
	}

	parseCases := runHTTPWGRFC8941ParseFiles(t, parseFiles)
	if parseCases != httpwgRFC8941ParseCases {
		t.Fatalf("parse corpus cases = %d, want %d", parseCases, httpwgRFC8941ParseCases)
	}
	serializationCases := runHTTPWGRFC8941SerializationFiles(t, serializationFiles)
	if serializationCases != httpwgRFC8941SerializationCases {
		t.Fatalf("serialization corpus cases = %d, want %d", serializationCases, httpwgRFC8941SerializationCases)
	}
}

func runHTTPWGRFC8941ParseFiles(t *testing.T, files []string) int {
	t.Helper()

	total := 0
	for _, file := range files {
		cases := readHTTPWGRFC8941Cases(t, file)
		total += len(cases)
		for _, test := range cases {
			test := test
			t.Run("parse/"+filepath.Base(file)+"/"+test.Name, func(t *testing.T) {
				fieldType, err := httpwgRFC8941FieldType(test.HeaderType)
				if err != nil {
					t.Fatal(err)
				}
				actual, parseErr := strictStructuredField(test.Raw, fieldType)
				if test.MustFail {
					if parseErr == nil {
						t.Fatalf("parse succeeded with canonical value %q, want failure", actual)
					}
					return
				}
				if parseErr != nil {
					t.Fatalf("parse (can_fail=%t): %v", test.CanFail, parseErr)
				}
				want, err := httpwgRFC8941Canonical(test)
				if err != nil {
					t.Fatal(err)
				}
				if actual != want {
					t.Fatalf("canonical value = %q, want %q", actual, want)
				}
			})
		}
	}

	return total
}

func runHTTPWGRFC8941SerializationFiles(t *testing.T, files []string) int {
	t.Helper()

	total := 0
	for _, file := range files {
		cases := readHTTPWGRFC8941Cases(t, file)
		total += len(cases)
		for _, test := range cases {
			test := test
			t.Run("serialize/"+filepath.Base(file)+"/"+test.Name, func(t *testing.T) {
				value, err := httpwgRFC8941StructuredValue(test.HeaderType, test.Expected)
				if err != nil {
					t.Fatalf("decode expected model: %v", err)
				}
				actual, marshalErr := marshalRFC8941(value)
				if test.MustFail {
					if marshalErr == nil {
						t.Fatalf("serialization succeeded with %q, want failure", actual)
					}
					return
				}
				if marshalErr != nil {
					t.Fatalf("serialize (can_fail=%t): %v", test.CanFail, marshalErr)
				}
				want, err := httpwgRFC8941Canonical(test)
				if err != nil {
					t.Fatal(err)
				}
				if actual != want {
					t.Fatalf("canonical value = %q, want %q", actual, want)
				}
			})
		}
	}

	return total
}

func readHTTPWGRFC8941Cases(t *testing.T, file string) []httpwgRFC8941Case {
	t.Helper()

	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read %s: %v", file, err)
	}
	var cases []httpwgRFC8941Case
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatalf("decode %s: %v", file, err)
	}
	if len(cases) == 0 {
		t.Fatalf("%s contains no cases", file)
	}

	return cases
}

func httpwgRFC8941FieldType(headerType string) (StructuredFieldType, error) {
	switch headerType {
	case "dictionary":
		return StructuredFieldDictionary, nil
	case "list":
		return StructuredFieldList, nil
	case "item":
		return StructuredFieldItem, nil
	default:
		return 0, fmt.Errorf("unknown header type %q", headerType)
	}
}

func httpwgRFC8941Canonical(test httpwgRFC8941Case) (string, error) {
	if test.Canonical != nil {
		switch len(*test.Canonical) {
		case 0:
			return "", nil
		case 1:
			return (*test.Canonical)[0], nil
		default:
			return "", fmt.Errorf("canonical field has %d lines", len(*test.Canonical))
		}
	}
	if len(test.Raw) != 1 {
		return "", fmt.Errorf("case without canonical form has %d raw lines", len(test.Raw))
	}

	return test.Raw[0], nil
}

func httpwgRFC8941StructuredValue(headerType string, raw json.RawMessage) (httpsfv.StructuredFieldValue, error) {
	if len(raw) == 0 {
		return nil, errors.New("expected model is absent")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return nil, err
	}

	switch headerType {
	case "dictionary":
		return httpwgRFC8941Dictionary(decoded)
	case "list":
		return httpwgRFC8941List(decoded)
	case "item":
		return httpwgRFC8941Item(decoded)
	default:
		return nil, fmt.Errorf("unknown header type %q", headerType)
	}
}

func httpwgRFC8941Dictionary(value any) (*httpsfv.Dictionary, error) {
	entries, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("dictionary model has type %T", value)
	}
	dictionary := httpsfv.NewDictionary()
	for index, rawEntry := range entries {
		entry, ok := rawEntry.([]any)
		if !ok || len(entry) != 2 {
			return nil, fmt.Errorf("dictionary entry %d is malformed", index)
		}
		key, ok := entry[0].(string)
		if !ok {
			return nil, fmt.Errorf("dictionary key %d has type %T", index, entry[0])
		}
		member, err := httpwgRFC8941Member(entry[1])
		if err != nil {
			return nil, fmt.Errorf("dictionary member %q: %w", key, err)
		}
		dictionary.Add(key, member)
	}

	return dictionary, nil
}

func httpwgRFC8941List(value any) (httpsfv.List, error) {
	entries, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("list model has type %T", value)
	}
	list := make(httpsfv.List, len(entries))
	for index, entry := range entries {
		member, err := httpwgRFC8941Member(entry)
		if err != nil {
			return nil, fmt.Errorf("list member %d: %w", index, err)
		}
		list[index] = member
	}

	return list, nil
}

func httpwgRFC8941Member(value any) (httpsfv.Member, error) {
	pair, ok := value.([]any)
	if !ok || len(pair) != 2 {
		return nil, fmt.Errorf("member has type %T", value)
	}
	if rawItems, ok := pair[0].([]any); ok {
		items := make([]httpsfv.Item, len(rawItems))
		for index, rawItem := range rawItems {
			item, err := httpwgRFC8941Item(rawItem)
			if err != nil {
				return nil, fmt.Errorf("inner-list item %d: %w", index, err)
			}
			items[index] = item
		}
		parameters, err := httpwgRFC8941Params(pair[1])
		if err != nil {
			return nil, err
		}
		return httpsfv.InnerList{Items: items, Params: parameters}, nil
	}

	return httpwgRFC8941Item(value)
}

func httpwgRFC8941Item(value any) (httpsfv.Item, error) {
	pair, ok := value.([]any)
	if !ok || len(pair) != 2 {
		return httpsfv.Item{}, fmt.Errorf("item has type %T", value)
	}
	bare, err := httpwgRFC8941BareItem(pair[0])
	if err != nil {
		return httpsfv.Item{}, err
	}
	parameters, err := httpwgRFC8941Params(pair[1])
	if err != nil {
		return httpsfv.Item{}, err
	}

	return httpsfv.Item{Value: bare, Params: parameters}, nil
}

func httpwgRFC8941Params(value any) (*httpsfv.Params, error) {
	entries, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("parameters have type %T", value)
	}
	parameters := httpsfv.NewParams()
	for index, rawEntry := range entries {
		entry, ok := rawEntry.([]any)
		if !ok || len(entry) != 2 {
			return nil, fmt.Errorf("parameter %d is malformed", index)
		}
		key, ok := entry[0].(string)
		if !ok {
			return nil, fmt.Errorf("parameter key %d has type %T", index, entry[0])
		}
		bare, err := httpwgRFC8941BareItem(entry[1])
		if err != nil {
			return nil, fmt.Errorf("parameter %q: %w", key, err)
		}
		parameters.Add(key, bare)
	}

	return parameters, nil
}

func httpwgRFC8941BareItem(value any) (any, error) {
	switch value := value.(type) {
	case bool, string:
		return value, nil
	case json.Number:
		if strings.Contains(value.String(), ".") {
			decimal, err := strconv.ParseFloat(value.String(), 64)
			if err != nil {
				return nil, err
			}
			return decimal, nil
		}
		integer, err := strconv.ParseInt(value.String(), 10, 64)
		if err != nil {
			return nil, err
		}
		return integer, nil
	case map[string]any:
		kind, kindOK := value["__type"].(string)
		encoded, valueOK := value["value"].(string)
		if !kindOK || !valueOK {
			return nil, errors.New("typed bare item is malformed")
		}
		switch kind {
		case "token":
			return httpsfv.Token(encoded), nil
		case "binary":
			decoded, err := base32.StdEncoding.DecodeString(encoded)
			if err != nil {
				return nil, err
			}
			return decoded, nil
		default:
			return nil, fmt.Errorf("unknown bare item type %q", kind)
		}
	default:
		return nil, fmt.Errorf("bare item has type %T", value)
	}
}
