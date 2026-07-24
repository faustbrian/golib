package abac

import (
	"encoding/json"
	"math"
	"strconv"
	"testing"
)

func FuzzDecodeDocument(f *testing.F) {
	f.Add([]byte(`{"version":1,"rules":[],"named_conditions":[]}`))
	f.Add([]byte(`{"version":1,"rules":[{"condition":{"operator":"not"}}],"named_conditions":[]}`))
	f.Add([]byte{0xff, 0x00, 0x01})

	largeSet := make([]ValueDocument, defaultMaxSetSize)
	for index := range largeSet {
		largeSet[index] = ValueDocument{Kind: ValueString, String: strconv.Itoa(index)}
	}
	unicode := ValueDocument{Kind: ValueString, String: "Grüße 世界 🧪"}
	maximumInt := ValueDocument{Kind: ValueInt, Int: int64(1<<63 - 1)}
	maximumFloat := ValueDocument{Kind: ValueFloat, Float: math.MaxFloat64}
	timeWithZone := ValueDocument{Kind: ValueTime, Time: "2026-07-15T13:00:00+03:00"}
	f.Add(fuzzDocumentSeed(Document{
		Version: DocumentVersion,
		Rules: []RuleDocument{{
			ID: "hostile-boundaries", Action: "read", ResourceType: "document",
			Effect: EffectAllow,
			Condition: &ConditionDocument{
				Operator: OperatorAll,
				Conditions: []ConditionDocument{
					{Operator: OperatorEqual, Source: SourceSubject, Attribute: "unicode", Value: &unicode},
					{Operator: OperatorGreaterThan, Source: SourceResource, Attribute: "integer", Value: &maximumInt},
					{Operator: OperatorLessThan, Source: SourceRequest, Attribute: "float", Value: &maximumFloat},
					{Operator: OperatorEqual, Source: SourceEnvironment, Attribute: "time", Value: &timeWithZone},
					{Operator: OperatorIPIn, Source: SourceEnvironment, Attribute: "address", Prefix: "2001:db8::/32"},
					{Operator: OperatorIn, Source: SourceSubject, Attribute: "large_set", Values: largeSet},
					{
						Operator: OperatorNot,
						Condition: &ConditionDocument{
							Operator:   OperatorAny,
							Conditions: []ConditionDocument{{Operator: OperatorIsNull, Source: SourceResource, Attribute: "nested"}},
						},
					},
				},
			},
		}},
		NamedConditions: []NamedConditionDocument{},
	}))
	f.Fuzz(func(t *testing.T, encoded []byte) {
		_, _ = DecodeDocument(encoded)
	})
}

func fuzzDocumentSeed(document Document) []byte {
	encoded, err := json.Marshal(document)
	if err != nil {
		panic(err)
	}

	return encoded
}
