package jsonrpc_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	correlation "github.com/faustbrian/golib/pkg/correlation"
	jsonrpccorrelation "github.com/faustbrian/golib/pkg/correlation/jsonrpc"
)

type generator struct{ values []string }

func (generator *generator) New() (string, error) {
	value := generator.values[0]
	generator.values = generator.values[1:]
	return value, nil
}

func TestMetadataIsExplicitAndStrictlyEncoded(t *testing.T) {
	factory, _ := correlation.NewFactory(correlation.FactoryOptions{
		Generator: &generator{values: []string{"rpc-request", "server-request"}},
	})
	adapter, err := jsonrpccorrelation.New(factory, jsonrpccorrelation.Options{})
	if err != nil {
		t.Fatal(err)
	}
	parent := correlation.Values{
		CorrelationID: correlation.MustCorrelationID("flow", correlation.Policy{}),
		RequestID:     correlation.MustRequestID("caller", correlation.Policy{}),
	}
	metadata := jsonrpccorrelation.Metadata{"application": {json.RawMessage(`"kept"`)}}
	_, err = adapter.Send(metadata, parent)
	if err != nil {
		t.Fatal(err)
	}
	received, err := adapter.Receive(metadata, true)
	if err != nil {
		t.Fatal(err)
	}
	if string(metadata["application"][0]) != `"kept"` || received.CorrelationID != parent.CorrelationID || received.CausationID.String() != "rpc-request" {
		t.Fatalf("metadata = %v, received = %#v", metadata, received)
	}

	metadata[jsonrpccorrelation.CorrelationField] = []json.RawMessage{json.RawMessage(`"one"`), json.RawMessage(`"two"`)}
	if _, err := adapter.Receive(metadata, true); !errors.Is(err, correlation.ErrConflictingCarrier) {
		t.Fatalf("duplicate metadata error = %v", err)
	}
	metadata[jsonrpccorrelation.CorrelationField] = []json.RawMessage{json.RawMessage(`123`)}
	if _, err := adapter.Receive(metadata, true); !errors.Is(err, jsonrpccorrelation.ErrMalformedMetadata) {
		t.Fatalf("malformed metadata error = %v", err)
	}
}

func TestAdapterRejectsInvalidOptionsAndNilMetadata(t *testing.T) {
	if _, err := jsonrpccorrelation.New(nil, jsonrpccorrelation.Options{}); !errors.Is(err, jsonrpccorrelation.ErrInvalidOptions) {
		t.Fatalf("New(nil) error = %v", err)
	}
	factory, _ := correlation.NewFactory(correlation.FactoryOptions{})
	if _, err := jsonrpccorrelation.New(factory, jsonrpccorrelation.Options{Codec: correlation.CodecOptions{Policy: correlation.Policy{MaxLength: -1}}}); err == nil {
		t.Fatal("invalid codec accepted")
	}
	adapter, _ := jsonrpccorrelation.New(factory, jsonrpccorrelation.Options{})
	if _, err := adapter.Send(nil, correlation.Values{}); !errors.Is(err, jsonrpccorrelation.ErrInvalidOptions) {
		t.Fatalf("Send(nil) error = %v", err)
	}
	if _, err := (*jsonrpccorrelation.Adapter)(nil).Send(jsonrpccorrelation.Metadata{}, correlation.Values{}); !errors.Is(err, jsonrpccorrelation.ErrInvalidOptions) {
		t.Fatalf("nil adapter Send() error = %v", err)
	}
	if _, err := (*jsonrpccorrelation.Adapter)(nil).Receive(nil, false); !errors.Is(err, jsonrpccorrelation.ErrInvalidOptions) {
		t.Fatalf("nil adapter Receive() error = %v", err)
	}
	if values, err := adapter.Receive(nil, false); err != nil || values.CorrelationID == "" || values.RequestID == "" {
		t.Fatalf("Receive(nil) = %#v, %v", values, err)
	}
}

func TestCustomMetadataFieldsRemainTypeAndSizeBounded(t *testing.T) {
	factory, _ := correlation.NewFactory(correlation.FactoryOptions{})
	adapter, err := jsonrpccorrelation.New(factory, jsonrpccorrelation.Options{
		Codec: correlation.CodecOptions{
			CorrelationField: "workflow",
			RequestField:     "attempt",
			CausationField:   "cause",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	for name, raw := range map[string]json.RawMessage{
		"non-string": json.RawMessage(`123`),
		"oversized":  json.RawMessage(`"` + strings.Repeat("a", 1025) + `"`),
	} {
		t.Run(name, func(t *testing.T) {
			metadata := jsonrpccorrelation.Metadata{"workflow": {raw}}
			if _, err := adapter.Receive(metadata, true); !errors.Is(err, jsonrpccorrelation.ErrMalformedMetadata) {
				t.Fatalf("Receive() error = %v", err)
			}
		})
	}

	exactlyEight := make([]json.RawMessage, 8)
	for index := range exactlyEight {
		exactlyEight[index] = json.RawMessage(`"flow"`)
	}
	if _, err := adapter.Receive(jsonrpccorrelation.Metadata{"workflow": exactlyEight}, true); err != nil {
		t.Fatalf("Receive(exactly eight) error = %v", err)
	}

	tooMany := make([]json.RawMessage, len(exactlyEight)+1)
	copy(tooMany, exactlyEight)
	tooMany[len(exactlyEight)] = json.RawMessage(`"flow"`)
	metadata := jsonrpccorrelation.Metadata{"workflow": tooMany}
	if _, err := adapter.Receive(metadata, true); !errors.Is(err, correlation.ErrInvalidCarrier) {
		t.Fatalf("Receive(too many) error = %v", err)
	}
	parent := correlation.Values{CorrelationID: "flow", RequestID: "parent"}
	if _, err := adapter.Send(metadata, parent); !errors.Is(err, correlation.ErrCarrierOverwrite) {
		t.Fatalf("Send(too many) error = %v", err)
	}
}

func FuzzCustomMetadataCarrier(fuzz *testing.F) {
	fuzz.Add(`"flow"`, `"request"`, `"flow"`, uint8(1), true)
	fuzz.Add(`123`, `"control\n"`, `null`, uint8(9), false)
	factory, _ := correlation.NewFactory(correlation.FactoryOptions{})
	adapter, _ := jsonrpccorrelation.New(factory, jsonrpccorrelation.Options{
		Codec: correlation.CodecOptions{
			CorrelationField: "workflow",
			RequestField:     "attempt",
			CausationField:   "cause",
		},
	})
	fuzz.Fuzz(func(t *testing.T, correlationRaw, requestRaw, duplicateRaw string, count uint8, trusted bool) {
		metadata := jsonrpccorrelation.Metadata{
			"workflow": {json.RawMessage(correlationRaw)},
			"attempt":  {json.RawMessage(requestRaw)},
		}
		for range min(int(count), 16) {
			metadata["workflow"] = append(metadata["workflow"], json.RawMessage(duplicateRaw))
		}
		values, err := adapter.Receive(metadata, trusted)
		if err == nil && (values.CorrelationID == "" || values.RequestID == "") {
			t.Fatalf("successful Receive() returned incomplete values: %#v", values)
		}
	})
}
