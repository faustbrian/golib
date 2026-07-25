// Package diff compares immutable compiled WSDL component graphs.
package diff

import (
	"cmp"
	"fmt"
	"sort"
	"strconv"
	"strings"

	wsdl "github.com/faustbrian/golib/pkg/wsdl"
	wsdlcompile "github.com/faustbrian/golib/pkg/wsdl/compile"
	xsd "github.com/faustbrian/golib/pkg/xsd"
)

// ChangeKind identifies how a compiled component changed.
type ChangeKind string

const (
	ChangeAdded    ChangeKind = "added"
	ChangeRemoved  ChangeKind = "removed"
	ChangeModified ChangeKind = "modified"
)

// Compatibility is a conservative source-compatibility classification.
type Compatibility string

const (
	CompatibilityBreaking    Compatibility = "breaking"
	CompatibilityNonBreaking Compatibility = "non-breaking"
	CompatibilityUnknown     Compatibility = "unknown"
)

// Change describes one deterministic semantic graph difference.
type Change struct {
	Path          string
	Kind          ChangeKind
	Compatibility Compatibility
	Before        string
	After         string
}

// Report is a semantic change set plus interpretation caveats.
type Report struct {
	Changes []Change
	Caveats []string
}

// Compare returns a conservative semantic comparison of two compiled sets.
func Compare(before, after *wsdlcompile.Set) Report {
	report := Report{Caveats: []string{
		"Compatibility is structural and does not prove wire-level behavior.",
		"Extension semantics and application-specific policy are classified as unknown.",
		"Schema additions can still constrain consumers through substitution or derivation.",
	}}
	compareInterfaces(before, after, &report)
	compareBindings(before, after, &report)
	compareServices(before, after, &report)
	compareSchemas(before, after, &report)
	sortChanges(report.Changes)
	return report
}

func sortChanges(changes []Change) {
	sort.Slice(changes, func(i, j int) bool {
		if changes[i].Path != changes[j].Path {
			return cmp.Compare(changes[i].Path, changes[j].Path) == -1
		}
		return cmp.Compare(changes[i].Kind, changes[j].Kind) == -1
	})
}

func compareInterfaces(before, after *wsdlcompile.Set, report *Report) {
	left := interfaceMap(before)
	right := interfaceMap(after)
	for name, value := range left {
		other, exists := right[name]
		path := "/interfaces/" + formatQName(name)
		if !exists {
			add(report, path, ChangeRemoved, CompatibilityBreaking, "present", "absent")
			continue
		}
		compareQNameSet(path+"/extends", value.Extends, other.Extends, report)
		compareOperations(path, value.Operations, other.Operations, report)
		compareQNameSet(path+"/faults", value.Faults, other.Faults, report)
	}
	for name := range right {
		if _, exists := left[name]; !exists {
			add(report, "/interfaces/"+formatQName(name), ChangeAdded, CompatibilityNonBreaking, "absent", "present")
		}
	}
}

func compareOperations(
	path string,
	before []wsdlcompile.Operation,
	after []wsdlcompile.Operation,
	report *Report,
) {
	left := make(map[string]wsdlcompile.Operation, len(before))
	right := make(map[string]wsdlcompile.Operation, len(after))
	identities := make(map[string]map[string]struct{})
	for _, value := range before {
		key := operationKey(value)
		left[key] = value
		if identities[value.Name] == nil {
			identities[value.Name] = make(map[string]struct{})
		}
		identities[value.Name][key] = struct{}{}
	}
	for _, value := range after {
		key := operationKey(value)
		right[key] = value
		if identities[value.Name] == nil {
			identities[value.Name] = make(map[string]struct{})
		}
		identities[value.Name][key] = struct{}{}
	}
	for key, value := range left {
		other, exists := right[key]
		operationPath := path + "/operations/" + operationPathName(value, len(identities[value.Name]) > 1)
		if !exists {
			add(report, operationPath, ChangeRemoved, CompatibilityBreaking, "present", "absent")
			continue
		}
		if value.Pattern != other.Pattern {
			add(report, operationPath+"/pattern", ChangeModified, CompatibilityBreaking, value.Pattern, other.Pattern)
		}
		if value.Style != other.Style {
			add(report, operationPath+"/style", ChangeModified, CompatibilityBreaking, value.Style, other.Style)
		}
		if value.Safe != other.Safe {
			add(
				report, operationPath+"/safe", ChangeModified,
				CompatibilityBreaking, strconv.FormatBool(value.Safe),
				strconv.FormatBool(other.Safe),
			)
		}
		compareStringSet(operationPath+"/styles", value.Styles, other.Styles, report)
		beforeSignature := rpcSignatureString(value.RPCSignature)
		afterSignature := rpcSignatureString(other.RPCSignature)
		if value.RPCSignatureSet != other.RPCSignatureSet || beforeSignature != afterSignature {
			add(
				report, operationPath+"/rpc-signature", ChangeModified,
				CompatibilityBreaking, beforeSignature, afterSignature,
			)
		}
		if len(value.Inputs) <= 1 && len(other.Inputs) <= 1 {
			compareMessage(operationPath+"/input", value.Input, other.Input, report)
		} else {
			compareMessages(operationPath+"/inputs", value.Inputs, other.Inputs, report)
		}
		if len(value.Outputs) <= 1 && len(other.Outputs) <= 1 {
			compareMessage(operationPath+"/output", value.Output, other.Output, report)
		} else {
			compareMessages(operationPath+"/outputs", value.Outputs, other.Outputs, report)
		}
		compareFaults(operationPath+"/faults", value.Faults, other.Faults, report)
	}
	for key, value := range right {
		if _, exists := left[key]; !exists {
			add(
				report,
				path+"/operations/"+operationPathName(value, len(identities[value.Name]) > 1),
				ChangeAdded, CompatibilityNonBreaking, "absent", "present",
			)
		}
	}
}

func rpcSignatureString(values []wsdl.RPCSignatureParameter20) string {
	items := make([]string, 0, len(values))
	for _, value := range values {
		items = append(items, formatQName(value.Name)+" "+string(value.Direction))
	}
	return strings.Join(items, " ")
}

func operationKey(value wsdlcompile.Operation) string {
	input, output := "", ""
	if value.Input != nil {
		input = value.Input.Label
	}
	if value.Output != nil {
		output = value.Output.Label
	}
	return value.Name + "\x00" + input + "\x00" + output
}

func operationPathName(value wsdlcompile.Operation, overloaded bool) string {
	if !overloaded {
		return value.Name
	}
	input, output := "", ""
	if value.Input != nil {
		input = value.Input.Label
	}
	if value.Output != nil {
		output = value.Output.Label
	}
	return value.Name + "[" + input + "|" + output + "]"
}

func compareMessages(
	path string,
	before []wsdlcompile.Message,
	after []wsdlcompile.Message,
	report *Report,
) {
	left := indexedMessages(before)
	right := indexedMessages(after)
	for key, value := range left {
		other, exists := right[key]
		messagePath := path + "/" + key
		if !exists {
			add(report, messagePath, ChangeRemoved, CompatibilityBreaking, "present", "absent")
			continue
		}
		compareMessage(messagePath, &value, &other, report)
	}
	for key := range right {
		if _, exists := left[key]; !exists {
			add(report, path+"/"+key, ChangeAdded, CompatibilityBreaking, "absent", "present")
		}
	}
}

func indexedMessages(values []wsdlcompile.Message) map[string]wsdlcompile.Message {
	result := make(map[string]wsdlcompile.Message, len(values))
	for index, value := range values {
		key := value.Label
		if key == "" {
			key = fmt.Sprintf("%06d", index)
		}
		result[key] = value
	}
	return result
}

func compareMessage(
	path string,
	before *wsdlcompile.Message,
	after *wsdlcompile.Message,
	report *Report,
) {
	if before == nil && after == nil {
		return
	}
	if before == nil {
		add(report, path, ChangeAdded, CompatibilityBreaking, "absent", "present")
		return
	}
	if after == nil {
		add(report, path, ChangeRemoved, CompatibilityBreaking, "present", "absent")
		return
	}
	properties := []struct {
		name, before, after string
	}{
		{name: "label", before: before.Label, after: after.Label},
		{name: "message", before: formatQName(before.Name), after: formatQName(after.Name)},
		{name: "element", before: formatQName(before.Element), after: formatQName(after.Element)},
		{name: "content-model", before: string(before.ContentModel), after: string(after.ContentModel)},
	}
	for _, property := range properties {
		if property.before != property.after {
			add(report, path+"/"+property.name, ChangeModified, CompatibilityBreaking, property.before, property.after)
		}
	}
	compareParts(path+"/parts", before.Parts, after.Parts, report)
}

func compareParts(
	path string,
	before []wsdlcompile.Part,
	after []wsdlcompile.Part,
	report *Report,
) {
	left := make(map[string]wsdlcompile.Part, len(before))
	right := make(map[string]wsdlcompile.Part, len(after))
	for _, value := range before {
		left[value.Name] = value
	}
	for _, value := range after {
		right[value.Name] = value
	}
	for name, value := range left {
		other, exists := right[name]
		partPath := path + "/" + name
		if !exists {
			add(report, partPath, ChangeRemoved, CompatibilityBreaking, "present", "absent")
			continue
		}
		if value.Element != other.Element {
			add(report, partPath+"/element", ChangeModified, CompatibilityBreaking, formatQName(value.Element), formatQName(other.Element))
		}
		if value.Type != other.Type {
			add(report, partPath+"/type", ChangeModified, CompatibilityBreaking, formatQName(value.Type), formatQName(other.Type))
		}
	}
	for name := range right {
		if _, exists := left[name]; !exists {
			add(report, path+"/"+name, ChangeAdded, CompatibilityBreaking, "absent", "present")
		}
	}
}

func compareFaults(
	path string,
	before []wsdlcompile.Fault,
	after []wsdlcompile.Fault,
	report *Report,
) {
	left := make(map[string]wsdlcompile.Fault, len(before))
	right := make(map[string]wsdlcompile.Fault, len(after))
	key := func(value wsdlcompile.Fault) string {
		return value.Direction + "/" + formatQName(value.Name) + "/" + value.Label
	}
	for _, value := range before {
		left[key(value)] = value
	}
	for _, value := range after {
		right[key(value)] = value
	}
	for name, value := range left {
		other, exists := right[name]
		faultPath := path + "/" + name
		if !exists {
			add(report, faultPath, ChangeRemoved, CompatibilityBreaking, "present", "absent")
			continue
		}
		beforeValue := formatQName(value.Message) + " " + formatQName(value.Element) + " " + string(value.ContentModel)
		afterValue := formatQName(other.Message) + " " + formatQName(other.Element) + " " + string(other.ContentModel)
		if beforeValue != afterValue {
			add(report, faultPath, ChangeModified, CompatibilityBreaking, beforeValue, afterValue)
		}
	}
	for name := range right {
		if _, exists := left[name]; !exists {
			add(report, path+"/"+name, ChangeAdded, CompatibilityBreaking, "absent", "present")
		}
	}
}

func compareBindings(before, after *wsdlcompile.Set, report *Report) {
	left := bindingMap(before)
	right := bindingMap(after)
	for name, value := range left {
		other, exists := right[name]
		path := "/bindings/" + formatQName(name)
		if !exists {
			add(report, path, ChangeRemoved, CompatibilityBreaking, "present", "absent")
			continue
		}
		if value.Interface != other.Interface || value.Type != other.Type {
			add(
				report, path, ChangeModified, CompatibilityBreaking,
				formatQName(value.Interface)+" "+value.Type,
				formatQName(other.Interface)+" "+other.Type,
			)
		}
		compareStringSet(
			path+"/operations",
			operationReferenceStrings(value),
			operationReferenceStrings(other),
			report,
		)
	}
	for name := range right {
		if _, exists := left[name]; !exists {
			add(report, "/bindings/"+formatQName(name), ChangeAdded, CompatibilityNonBreaking, "absent", "present")
		}
	}
}

func operationReferenceStrings(value wsdlcompile.Binding) []string {
	if len(value.OperationReferences) == 0 {
		return value.Operations
	}
	result := make([]string, 0, len(value.OperationReferences))
	for _, reference := range value.OperationReferences {
		result = append(result, reference.Name+"|"+reference.Input+"|"+reference.Output)
	}
	return result
}

func compareServices(before, after *wsdlcompile.Set, report *Report) {
	left := serviceMap(before)
	right := serviceMap(after)
	for name, value := range left {
		other, exists := right[name]
		path := "/services/" + formatQName(name)
		if !exists {
			add(report, path, ChangeRemoved, CompatibilityBreaking, "present", "absent")
			continue
		}
		if value.Interface != other.Interface {
			add(report, path+"/interface", ChangeModified, CompatibilityBreaking, formatQName(value.Interface), formatQName(other.Interface))
		}
		compareEndpoints(path, value.Endpoints, other.Endpoints, report)
	}
	for name := range right {
		if _, exists := left[name]; !exists {
			add(report, "/services/"+formatQName(name), ChangeAdded, CompatibilityNonBreaking, "absent", "present")
		}
	}
}

func compareEndpoints(
	path string,
	before []wsdlcompile.Endpoint,
	after []wsdlcompile.Endpoint,
	report *Report,
) {
	left := make(map[string]wsdlcompile.Endpoint, len(before))
	right := make(map[string]wsdlcompile.Endpoint, len(after))
	for _, value := range before {
		left[value.Name] = value
	}
	for _, value := range after {
		right[value.Name] = value
	}
	for name, value := range left {
		other, exists := right[name]
		endpointPath := path + "/endpoints/" + name
		if !exists {
			add(report, endpointPath, ChangeRemoved, CompatibilityBreaking, "present", "absent")
		} else if value.Binding != other.Binding || value.Address != other.Address {
			add(report, endpointPath, ChangeModified, CompatibilityBreaking, formatQName(value.Binding)+" "+value.Address, formatQName(other.Binding)+" "+other.Address)
		}
	}
	for name := range right {
		if _, exists := left[name]; !exists {
			add(report, path+"/endpoints/"+name, ChangeAdded, CompatibilityNonBreaking, "absent", "present")
		}
	}
}

func compareSchemas(before, after *wsdlcompile.Set, report *Report) {
	var leftElements, rightElements []xsd.QName
	var leftTypes, rightTypes []xsd.QName
	if before != nil && before.Schemas() != nil {
		leftElements = before.Schemas().ElementNames()
		leftTypes = append(before.Schemas().SimpleTypeNames(), before.Schemas().ComplexTypeNames()...)
	}
	if after != nil && after.Schemas() != nil {
		rightElements = after.Schemas().ElementNames()
		rightTypes = append(after.Schemas().SimpleTypeNames(), after.Schemas().ComplexTypeNames()...)
	}
	compareXSDQNameSet("/schemas/elements", leftElements, rightElements, report)
	compareXSDQNameSet("/schemas/types", leftTypes, rightTypes, report)
}

func compareQNameSet(path string, before, after []wsdl.QName, report *Report) {
	left := make(map[string]struct{}, len(before))
	right := make(map[string]struct{}, len(after))
	for _, value := range before {
		left[formatQName(value)] = struct{}{}
	}
	for _, value := range after {
		right[formatQName(value)] = struct{}{}
	}
	compareKeys(path, left, right, report)
}

func compareXSDQNameSet(path string, before, after []xsd.QName, report *Report) {
	left := make(map[string]struct{}, len(before))
	right := make(map[string]struct{}, len(after))
	for _, value := range before {
		left[formatQName(wsdl.QName{Namespace: value.Namespace, Local: value.Local})] = struct{}{}
	}
	for _, value := range after {
		right[formatQName(wsdl.QName{Namespace: value.Namespace, Local: value.Local})] = struct{}{}
	}
	compareKeys(path, left, right, report)
}

func compareStringSet(path string, before, after []string, report *Report) {
	left := make(map[string]struct{}, len(before))
	right := make(map[string]struct{}, len(after))
	for _, value := range before {
		left[value] = struct{}{}
	}
	for _, value := range after {
		right[value] = struct{}{}
	}
	compareKeys(path, left, right, report)
}

func compareKeys(path string, before, after map[string]struct{}, report *Report) {
	for name := range before {
		if _, exists := after[name]; !exists {
			add(report, path+"/"+name, ChangeRemoved, CompatibilityBreaking, "present", "absent")
		}
	}
	for name := range after {
		if _, exists := before[name]; !exists {
			add(report, path+"/"+name, ChangeAdded, CompatibilityNonBreaking, "absent", "present")
		}
	}
}

func interfaceMap(set *wsdlcompile.Set) map[wsdl.QName]wsdlcompile.Interface {
	result := make(map[wsdl.QName]wsdlcompile.Interface)
	if set != nil {
		for _, value := range set.Interfaces() {
			result[value.Name] = value
		}
	}
	return result
}

func bindingMap(set *wsdlcompile.Set) map[wsdl.QName]wsdlcompile.Binding {
	result := make(map[wsdl.QName]wsdlcompile.Binding)
	if set != nil {
		for _, value := range set.Bindings() {
			result[value.Name] = value
		}
	}
	return result
}

func serviceMap(set *wsdlcompile.Set) map[wsdl.QName]wsdlcompile.Service {
	result := make(map[wsdl.QName]wsdlcompile.Service)
	if set != nil {
		for _, value := range set.Services() {
			result[value.Name] = value
		}
	}
	return result
}

func add(
	report *Report,
	path string,
	kind ChangeKind,
	compatibility Compatibility,
	before string,
	after string,
) {
	report.Changes = append(report.Changes, Change{
		Path: path, Kind: kind, Compatibility: compatibility,
		Before: before, After: after,
	})
}

func formatQName(value wsdl.QName) string {
	return fmt.Sprintf("{%s}%s", value.Namespace, value.Local)
}
