package schemaregistry

import (
	"context"
	"errors"
	"testing"
)

type codecFunctions struct {
	encode func(context.Context, Schema, any) ([]byte, error)
	decode func(context.Context, Schema, []byte, any) error
}

func (codec *codecFunctions) Encode(ctx context.Context, schema Schema, value any) ([]byte, error) {
	return codec.encode(ctx, schema, value)
}
func (codec *codecFunctions) Decode(ctx context.Context, schema Schema, payload []byte, target any) error {
	return codec.decode(ctx, schema, payload, target)
}

type framerFunctions struct {
	frame   func(context.Context, ProviderID, []byte) ([]byte, error)
	unframe func(context.Context, []byte) (ProviderID, []byte, error)
}

func (framer *framerFunctions) Frame(ctx context.Context, id ProviderID, payload []byte) ([]byte, error) {
	return framer.frame(ctx, id, payload)
}
func (framer *framerFunctions) Unframe(ctx context.Context, framed []byte) (ProviderID, []byte, error) {
	return framer.unframe(ctx, framed)
}

func TestCodecIntegrationBoundaryFailures(t *testing.T) {
	t.Parallel()

	var nilCodec *codecFunctions
	var nilFramer *framerFunctions
	validCodec := &codecFunctions{
		encode: func(context.Context, Schema, any) ([]byte, error) { return []byte("payload"), nil },
		decode: func(context.Context, Schema, []byte, any) error { return nil },
	}
	validFramer := &framerFunctions{
		frame: func(_ context.Context, _ ProviderID, payload []byte) ([]byte, error) {
			return append([]byte("f"), payload...), nil
		},
		unframe: func(_ context.Context, framed []byte) (ProviderID, []byte, error) {
			return ProviderID{Provider: "test", Value: "1"}, append([]byte(nil), framed...), nil
		},
	}
	if _, err := NewCodecIntegration(nilCodec, validFramer, CodecLimits{MaxPayloadBytes: 1, MaxFrameBytes: 1}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("NewCodecIntegration(nil codec) error = %v", err)
	}
	if _, err := NewCodecIntegration(validCodec, nilFramer, CodecLimits{MaxPayloadBytes: 1, MaxFrameBytes: 1}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("NewCodecIntegration(nil framer) error = %v", err)
	}
	for _, limits := range []CodecLimits{{}, {MaxPayloadBytes: 2, MaxFrameBytes: 1}} {
		if _, err := NewCodecIntegration(validCodec, validFramer, limits); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("NewCodecIntegration(%+v) error = %v", limits, err)
		}
	}
	integration, err := NewCodecIntegration(validCodec, validFramer, CodecLimits{MaxPayloadBytes: 7, MaxFrameBytes: 8})
	if err != nil {
		t.Fatal(err)
	}
	schema := internalSchema(t, FormatAvro, `"string"`, nil)
	id := ProviderID{Provider: "test", Value: "1"}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := integration.Encode(canceled, schema, id, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("Encode(canceled) error = %v", err)
	}
	if _, err := integration.Encode(context.Background(), Schema{}, id, nil); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("Encode(invalid schema) error = %v", err)
	}
	codecError := errors.New("codec")
	validCodec.encode = func(context.Context, Schema, any) ([]byte, error) { return nil, codecError }
	if _, err := integration.Encode(context.Background(), schema, id, nil); !errors.Is(err, codecError) {
		t.Fatalf("Encode(codec error) error = %v", err)
	}
	validCodec.encode = func(context.Context, Schema, any) ([]byte, error) { return []byte("payload"), nil }
	frameError := errors.New("frame")
	validFramer.frame = func(context.Context, ProviderID, []byte) ([]byte, error) { return nil, frameError }
	if _, err := integration.Encode(context.Background(), schema, id, nil); !errors.Is(err, frameError) {
		t.Fatalf("Encode(frame error) error = %v", err)
	}
	validFramer.frame = func(context.Context, ProviderID, []byte) ([]byte, error) { return make([]byte, 9), nil }
	if _, err := integration.Encode(context.Background(), schema, id, nil); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("Encode(frame too large) error = %v", err)
	}
	if _, err := integration.Parse(canceled, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("Parse(canceled) error = %v", err)
	}
	if _, err := integration.Parse(context.Background(), make([]byte, 9)); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("Parse(frame too large) error = %v", err)
	}
	validFramer.unframe = func(context.Context, []byte) (ProviderID, []byte, error) { return ProviderID{}, nil, frameError }
	if _, err := integration.Parse(context.Background(), nil); !errors.Is(err, frameError) {
		t.Fatalf("Parse(frame error) error = %v", err)
	}
	validFramer.unframe = func(context.Context, []byte) (ProviderID, []byte, error) { return ProviderID{}, nil, nil }
	if _, err := integration.Parse(context.Background(), nil); !errors.Is(err, ErrInvalidSchema) {
		t.Fatalf("Parse(invalid ID) error = %v", err)
	}
	validFramer.unframe = func(context.Context, []byte) (ProviderID, []byte, error) { return id, make([]byte, 8), nil }
	if _, err := integration.Parse(context.Background(), nil); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("Parse(payload too large) error = %v", err)
	}
	if err := integration.Decode(canceled, schema, WireMessage{ID: id}, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("Decode(canceled) error = %v", err)
	}
	if err := integration.Decode(context.Background(), Schema{}, WireMessage{ID: id}, nil); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("Decode(invalid schema) error = %v", err)
	}
	if err := integration.Decode(context.Background(), schema, WireMessage{ID: id, Payload: make([]byte, 8)}, nil); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("Decode(payload too large) error = %v", err)
	}
	validCodec.decode = func(context.Context, Schema, []byte, any) error { return codecError }
	if err := integration.Decode(context.Background(), schema, WireMessage{ID: id}, nil); !errors.Is(err, codecError) {
		t.Fatalf("Decode(codec error) error = %v", err)
	}
}

type referenceResolverFunction func(context.Context, ReferenceCoordinate) (ReferenceDocument, error)

func (function referenceResolverFunction) ResolveReference(ctx context.Context, coordinate ReferenceCoordinate) (ReferenceDocument, error) {
	return function(ctx, coordinate)
}

func TestReferenceGraphBoundaryFailures(t *testing.T) {
	t.Parallel()

	coordinate := ReferenceCoordinate{Subject: Subject{Name: "root"}, Version: Version{Number: 1}}
	resolver := referenceResolverFunction(func(context.Context, ReferenceCoordinate) (ReferenceDocument, error) {
		return ReferenceDocument{Coordinate: coordinate}, nil
	})
	valid := GraphLimits{MaxSchemas: 2, MaxDepth: 2, MaxReferences: 2}
	if _, err := BuildReferenceGraph(&delayedCanceledContext{}, []ReferenceCoordinate{coordinate}, resolver, valid); !errors.Is(err, context.Canceled) {
		t.Fatalf("BuildReferenceGraph(canceled during visit) error = %v", err)
	}
	var nilResolver *referenceResolverFunction
	if _, err := BuildReferenceGraph(context.Background(), nil, resolver, valid); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("BuildReferenceGraph(no roots) error = %v", err)
	}
	if _, err := BuildReferenceGraph(context.Background(), []ReferenceCoordinate{coordinate}, nilResolver, valid); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("BuildReferenceGraph(nil resolver) error = %v", err)
	}
	if _, err := BuildReferenceGraph(context.Background(), []ReferenceCoordinate{coordinate}, resolver, GraphLimits{}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("BuildReferenceGraph(invalid limits) error = %v", err)
	}
	if _, err := BuildReferenceGraph(context.Background(), []ReferenceCoordinate{{}}, resolver, valid); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("BuildReferenceGraph(invalid coordinate) error = %v", err)
	}
	mismatch := referenceResolverFunction(func(context.Context, ReferenceCoordinate) (ReferenceDocument, error) {
		return ReferenceDocument{Coordinate: ReferenceCoordinate{Subject: Subject{Name: "other"}, Version: Version{Number: 1}}}, nil
	})
	if _, err := BuildReferenceGraph(context.Background(), []ReferenceCoordinate{coordinate}, mismatch, valid); !errors.Is(err, ErrInvalidSchema) {
		t.Fatalf("BuildReferenceGraph(mismatch) error = %v", err)
	}
	emptyName := referenceResolverFunction(func(context.Context, ReferenceCoordinate) (ReferenceDocument, error) {
		return ReferenceDocument{Coordinate: coordinate, References: []ProviderReference{{Target: coordinate}}}, nil
	})
	if _, err := BuildReferenceGraph(context.Background(), []ReferenceCoordinate{coordinate}, emptyName, valid); !errors.Is(err, ErrInvalidSchema) {
		t.Fatalf("BuildReferenceGraph(empty name) error = %v", err)
	}
	child := ReferenceCoordinate{Subject: Subject{Name: "child"}, Version: Version{Number: 1}}
	tooMany := referenceResolverFunction(func(_ context.Context, current ReferenceCoordinate) (ReferenceDocument, error) {
		if current == coordinate {
			return ReferenceDocument{Coordinate: coordinate, References: []ProviderReference{{Name: "a", Target: child}, {Name: "b", Target: child}}}, nil
		}
		return ReferenceDocument{Coordinate: child}, nil
	})
	if _, err := BuildReferenceGraph(context.Background(), []ReferenceCoordinate{coordinate}, tooMany, GraphLimits{MaxSchemas: 2, MaxDepth: 2, MaxReferences: 1}); !errors.Is(err, ErrReferenceLimit) {
		t.Fatalf("BuildReferenceGraph(references) error = %v", err)
	}
	duplicateRoots, err := BuildReferenceGraph(context.Background(), []ReferenceCoordinate{coordinate, coordinate}, resolver, valid)
	if err != nil || len(duplicateRoots.Documents()) != 1 {
		t.Fatalf("BuildReferenceGraph(duplicate roots) = (%+v, %v)", duplicateRoots.Documents(), err)
	}
	depthResolver := referenceResolverFunction(func(_ context.Context, current ReferenceCoordinate) (ReferenceDocument, error) {
		if current == coordinate {
			return ReferenceDocument{Coordinate: coordinate, References: []ProviderReference{{Name: "child", Target: child}}}, nil
		}
		return ReferenceDocument{Coordinate: child}, nil
	})
	if _, err := BuildReferenceGraph(context.Background(), []ReferenceCoordinate{coordinate}, depthResolver, GraphLimits{MaxSchemas: 2, MaxDepth: 1, MaxReferences: 1}); !errors.Is(err, ErrReferenceLimit) {
		t.Fatalf("BuildReferenceGraph(depth) error = %v", err)
	}
}
