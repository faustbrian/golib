package diff

import (
	"testing"

	wsdl "github.com/faustbrian/golib/pkg/wsdl"
	wsdlcompile "github.com/faustbrian/golib/pkg/wsdl/compile"
)

func TestOperationIdentityIncludesNamedOutput(t *testing.T) {
	t.Parallel()

	operation := wsdlcompile.Operation{
		Name:   "Call",
		Output: &wsdlcompile.Message{Label: "Result"},
	}
	if got := operationKey(operation); got != "Call\x00\x00Result" {
		t.Fatalf("operationKey() = %q", got)
	}
	if got := operationPathName(operation, true); got != "Call[|Result]" {
		t.Fatalf("operationPathName() = %q", got)
	}
}

func TestCompareOperationsCoversProtocolAndRepeatedOutputs(t *testing.T) {
	t.Parallel()

	report := Report{}
	compareOperations("/interface", []wsdlcompile.Operation{{
		Name:    "Call",
		Pattern: "urn:before",
		Style:   "urn:style-before",
		Outputs: []wsdlcompile.Message{
			{Label: "First", Element: wsdl.QName{Local: "Before"}},
			{Label: "Removed"},
		},
	}}, []wsdlcompile.Operation{{
		Name:    "Call",
		Pattern: "urn:after",
		Style:   "urn:style-after",
		Outputs: []wsdlcompile.Message{
			{Label: "First", Element: wsdl.QName{Local: "After"}},
			{Label: "Added"},
		},
	}}, &report)

	assertChangePaths(t, report,
		"/interface/operations/Call/pattern",
		"/interface/operations/Call/style",
		"/interface/operations/Call/outputs/First/element",
		"/interface/operations/Call/outputs/Removed",
		"/interface/operations/Call/outputs/Added",
	)
}

func TestCompareMessageCoversPresencePropertiesAndParts(t *testing.T) {
	t.Parallel()

	report := Report{}
	compareMessage("/nil", nil, nil, &report)
	compareMessage("/added", nil, &wsdlcompile.Message{}, &report)
	compareMessage("/removed", &wsdlcompile.Message{}, nil, &report)
	compareMessage("/message", &wsdlcompile.Message{
		Label:        "Before",
		Name:         wsdl.QName{Local: "BeforeMessage"},
		Element:      wsdl.QName{Local: "BeforeElement"},
		ContentModel: wsdl.MessageContentNone,
		Parts: []wsdlcompile.Part{
			{Name: "Removed"},
			{Name: "Changed", Element: wsdl.QName{Local: "Before"}, Type: wsdl.QName{Local: "Old"}},
		},
	}, &wsdlcompile.Message{
		Label:        "After",
		Name:         wsdl.QName{Local: "AfterMessage"},
		Element:      wsdl.QName{Local: "AfterElement"},
		ContentModel: wsdl.MessageContentAny,
		Parts: []wsdlcompile.Part{
			{Name: "Added"},
			{Name: "Changed", Element: wsdl.QName{Local: "After"}, Type: wsdl.QName{Local: "New"}},
		},
	}, &report)

	assertChangePaths(t, report,
		"/added", "/removed", "/message/label", "/message/message",
		"/message/element", "/message/content-model",
		"/message/parts/Removed", "/message/parts/Changed/element",
		"/message/parts/Changed/type", "/message/parts/Added",
	)
}

func TestIndexedMessagesAndBindingReferencesPreserveIdentity(t *testing.T) {
	t.Parallel()

	messages := indexedMessages([]wsdlcompile.Message{{}, {Label: "Named"}})
	if _, exists := messages["000000"]; !exists {
		t.Fatalf("indexedMessages() = %#v", messages)
	}
	operations := operationReferenceStrings(wsdlcompile.Binding{
		OperationReferences: []wsdlcompile.OperationReference{{
			Name: "Call", Input: "Request", Output: "Response",
		}},
	})
	if len(operations) != 1 || operations[0] != "Call|Request|Response" {
		t.Fatalf("operationReferenceStrings() = %#v", operations)
	}
}

func TestSortChangesUsesKindAsStablePathTieBreaker(t *testing.T) {
	t.Parallel()

	changes := []Change{
		{Path: "/z", Kind: ChangeModified},
		{Path: "/a", Kind: ChangeRemoved},
		{Path: "/a", Kind: ChangeAdded},
	}
	sortChanges(changes)
	if changes[0].Kind != ChangeAdded || changes[1].Kind != ChangeRemoved ||
		changes[2].Path != "/z" {
		t.Fatalf("sortChanges() = %#v", changes)
	}
}

func TestCompareOperationsUsesSingularOutputAtExactBoundary(t *testing.T) {
	t.Parallel()

	beforeOutput := wsdlcompile.Message{Element: wsdl.QName{Local: "Before"}}
	afterOutput := wsdlcompile.Message{Element: wsdl.QName{Local: "After"}}
	report := Report{}
	compareOperations("/interface", []wsdlcompile.Operation{{
		Name: "Call", Output: &beforeOutput, Outputs: []wsdlcompile.Message{beforeOutput},
	}}, []wsdlcompile.Operation{{
		Name: "Call", Output: &afterOutput, Outputs: []wsdlcompile.Message{afterOutput},
	}}, &report)

	assertChangePaths(t, report, "/interface/operations/Call/output/element")
}

func TestCompareOperationsNamesAddedOverloadsUnambiguously(t *testing.T) {
	t.Parallel()

	request := &wsdlcompile.Message{Label: "Request"}
	alternate := &wsdlcompile.Message{Label: "Alternate"}
	report := Report{}
	compareOperations("/interface", []wsdlcompile.Operation{{
		Name: "Call", Input: request,
	}}, []wsdlcompile.Operation{
		{Name: "Call", Input: request},
		{Name: "Call", Input: alternate},
	}, &report)

	assertChangePaths(t, report, "/interface/operations/Call[Alternate|]")
}

func TestCompareOperationsKeepsUniqueAddedOperationNamePlain(t *testing.T) {
	t.Parallel()

	report := Report{}
	compareOperations("/interface", nil, []wsdlcompile.Operation{{
		Name: "Call", Input: &wsdlcompile.Message{Label: "Request"},
	}}, &report)

	assertChangePaths(t, report, "/interface/operations/Call")
}

func TestCompareFaultsDetectsPayloadChanges(t *testing.T) {
	t.Parallel()

	report := Report{}
	compareFaults("/faults", []wsdlcompile.Fault{{
		Name: wsdl.QName{Local: "Problem"}, Element: wsdl.QName{Local: "Before"},
	}}, []wsdlcompile.Fault{{
		Name: wsdl.QName{Local: "Problem"}, Element: wsdl.QName{Local: "After"},
	}}, &report)

	assertChangePaths(t, report, "/faults//{}Problem/")
}

func TestCompareEndpointsChecksBindingAndAddressIndependently(t *testing.T) {
	t.Parallel()

	for name, after := range map[string]wsdlcompile.Endpoint{
		"binding": {Name: "API", Binding: wsdl.QName{Local: "After"}, Address: "https://before.test"},
		"address": {Name: "API", Binding: wsdl.QName{Local: "Before"}, Address: "https://after.test"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			report := Report{}
			compareEndpoints("/service", []wsdlcompile.Endpoint{{
				Name: "API", Binding: wsdl.QName{Local: "Before"}, Address: "https://before.test",
			}}, []wsdlcompile.Endpoint{after}, &report)
			assertChangePaths(t, report, "/service/endpoints/API")
		})
	}
}

func assertChangePaths(t *testing.T, report Report, expected ...string) {
	t.Helper()

	paths := make(map[string]struct{}, len(report.Changes))
	for _, change := range report.Changes {
		paths[change.Path] = struct{}{}
	}
	for _, path := range expected {
		if _, exists := paths[path]; !exists {
			t.Fatalf("missing change %q in %#v", path, report.Changes)
		}
	}
}
