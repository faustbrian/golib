// Package validate provides deterministic, resource-bounded OpenRPC document
// validation with stable machine-readable diagnostics.
package validate

import (
	"context"
	"encoding/json"
	"errors"
	"net/mail"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	openrpc "github.com/faustbrian/golib/pkg/openrpc"
	"github.com/faustbrian/golib/pkg/openrpc/expression"
	"github.com/faustbrian/golib/pkg/openrpc/jsonvalue"
	referencevalue "github.com/faustbrian/golib/pkg/openrpc/reference"
)

// Code is a stable machine-readable diagnostic identifier.
type Code string

const (
	CodeDuplicateMethodName      Code = "method.name.duplicate"
	CodeDuplicateParameterName   Code = "method.parameter.name.duplicate"
	CodeRequiredParameterOrder   Code = "method.parameter.required_order"
	CodeDuplicateErrorCode       Code = "method.error.code.duplicate"
	CodeErrorMessageNotConcise   Code = "error.message.not_concise"
	CodeUnresolvedLinkMethod     Code = "link.method.unresolved"
	CodeInvalidRuntimeExpression Code = "runtime_expression.invalid"
	CodeMissingServerVariable    Code = "server.variable.missing"
	CodeInvalidReference         Code = "reference.invalid"
	CodeInvalidFormat            Code = "format.invalid"
	CodeCanceled                 Code = "validation.canceled"
	CodeInvalidOptions           Code = "validation.options.invalid"
	CodeResourceLimit            Code = "validation.resource_limit"
)

// Severity classifies diagnostic impact.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// Mode controls whether validation collects diagnostics or stops after the
// first rule failure.
type Mode uint8

const (
	Collect Mode = iota
	FailFast
)

// Options controls validation execution.
type Options struct {
	Mode           Mode
	MaxDiagnostics int
	MaxMethods     int
}

// DefaultOptions returns deterministic collect mode with a finite bound.
func DefaultOptions() Options {
	return Options{Mode: Collect, MaxDiagnostics: 1_000, MaxMethods: 10_000}
}

// Diagnostic is one safe validation finding.
type Diagnostic struct {
	Code          Code
	Pointer       string
	Severity      Severity
	Specification string
	Message       string
}

// Report is an immutable validation result.
type Report struct {
	diagnostics []Diagnostic
	truncated   bool
}

// Diagnostics returns an owned result snapshot.
func (report Report) Diagnostics() []Diagnostic {
	return append([]Diagnostic(nil), report.diagnostics...)
}

// Truncated reports that the configured diagnostic bound stopped validation.
func (report Report) Truncated() bool { return report.truncated }

// Valid reports whether validation found no error-severity diagnostics.
func (report Report) Valid() bool {
	for _, diagnostic := range report.diagnostics {
		if diagnostic.Severity == SeverityError {
			return false
		}
	}
	return true
}

// Document validates semantic rules that do not require external resolution.
// It performs no filesystem or network access.
func Document(ctx context.Context, document openrpc.Document, options Options) Report {
	if ctx == nil || options.MaxDiagnostics <= 0 || options.MaxMethods <= 0 ||
		(options.Mode != Collect && options.Mode != FailFast) {
		return Report{diagnostics: []Diagnostic{invalidOptionsDiagnostic()}}
	}
	if document.MethodCount() > options.MaxMethods {
		return Report{
			diagnostics: []Diagnostic{resourceLimitDiagnostic(
				"#/methods", "method validation limit exceeded",
			)},
			truncated: true,
		}
	}
	engine := validator{ctx: ctx, options: options}
	if engine.canceled() {
		return engine.report()
	}

	methods := document.Methods()
	methodCounts := countMethodNames(methods)
	engine.validateMetadata(document)
	engine.validateSchemas(document)
	engine.validateMethodReferences(methods)
	if servers, present := document.Servers(); present {
		for index, server := range servers {
			engine.validateServer(pointer("servers", index), server)
		}
	}
	seenMethods := make(map[string]bool, len(methodCounts))
	for index, union := range methods {
		if engine.stop || engine.canceled() {
			return engine.report()
		}
		method, ok := union.Method()
		if !ok {
			continue
		}
		if seenMethods[method.Name()] {
			engine.add(Diagnostic{
				Code:          CodeDuplicateMethodName,
				Pointer:       pointer("methods", index, "name"),
				Severity:      SeverityError,
				Specification: methodSpecification,
				Message:       "method names must be unique",
			})
		}
		seenMethods[method.Name()] = true
	}

	for index, union := range methods {
		if engine.stop || engine.canceled() {
			return engine.report()
		}
		method, ok := union.Method()
		if !ok {
			continue
		}
		engine.validateParameters(index, method)
		engine.validateErrors(index, method)
		engine.validateLinks("#/methods/"+itoa(index)+"/links", method.Links, methodCounts)
		if servers, present := method.Servers(); present {
			for serverIndex, server := range servers {
				engine.validateServer(pointer("methods", index, "servers", serverIndex), server)
			}
		}
	}

	if components, ok := document.Components(); ok && !engine.stop {
		if errorsMap, present := components.Errors(); present {
			for _, name := range sortedNames(errorsMap) {
				engine.validateErrorMessage(
					"#/components/errors/"+escape(name)+"/message", errorsMap[name],
				)
			}
		}
		if links, present := components.Links(); present {
			names := sortedNames(links)
			for _, name := range names {
				if engine.stop || engine.canceled() {
					break
				}
				engine.validateLink(
					"#/components/links/"+escape(name),
					links[name],
					methodCounts,
				)
			}
		}
		if pairings, present := components.ExamplePairings(); present {
			for _, name := range sortedNames(pairings) {
				engine.validateExamplePairingReferences(
					"#/components/examplePairings/"+escape(name),
					pairings[name],
				)
			}
		}
	}

	return engine.report()
}

func (validator *validator) validateMetadata(document openrpc.Document) {
	info := document.Info()
	if contact, present := info.Contact(); present {
		if email, present := contact.Email(); present && !validEmail(email) {
			validator.add(formatDiagnostic("#/info/contact/email", "contact email must be a valid address"))
		}
		if uri, present := contact.URL(); present && !validAbsoluteURI(uri) {
			validator.add(formatDiagnostic("#/info/contact/url", "contact URL must be an absolute URI"))
		}
	}
	if uri, present := info.TermsOfService(); present && !validAbsoluteURI(uri) {
		validator.add(formatDiagnostic("#/info/termsOfService", "terms of service must be an absolute URI"))
	}
	if license, present := info.License(); present {
		if uri, present := license.URL(); present && !validAbsoluteURI(uri) {
			validator.add(formatDiagnostic("#/info/license/url", "license URL must be an absolute URI"))
		}
	}
	if docs, present := document.ExternalDocs(); present && !validAbsoluteURI(docs.URL()) {
		validator.add(formatDiagnostic("#/externalDocs/url", "external documentation URL must be an absolute URI"))
	}
	for methodIndex, union := range document.Methods() {
		method, inline := union.Method()
		if !inline {
			continue
		}
		if docs, present := method.ExternalDocs(); present && !validAbsoluteURI(docs.URL()) {
			validator.add(formatDiagnostic(pointer("methods", methodIndex, "externalDocs", "url"), "external documentation URL must be an absolute URI"))
		}
		if tags, present := method.Tags(); present {
			for tagIndex, union := range tags {
				tag, inline := union.Tag()
				if !inline {
					continue
				}
				if docs, present := tag.ExternalDocs(); present && !validAbsoluteURI(docs.URL()) {
					validator.add(formatDiagnostic(pointer("methods", methodIndex, "tags", tagIndex, "externalDocs", "url"), "external documentation URL must be an absolute URI"))
				}
			}
		}
	}
	if components, present := document.Components(); present {
		if tags, present := components.Tags(); present {
			for _, name := range sortedNames(tags) {
				if docs, present := tags[name].ExternalDocs(); present && !validAbsoluteURI(docs.URL()) {
					validator.add(formatDiagnostic("#/components/tags/"+escape(name)+"/externalDocs/url", "external documentation URL must be an absolute URI"))
				}
			}
		}
	}
}

func validEmail(input string) bool {
	address, err := mail.ParseAddress(input)
	return err == nil && address.Address == input
}

func validAbsoluteURI(input string) bool {
	parsed, err := url.Parse(input)
	return err == nil && utf8.ValidString(input) && parsed.IsAbs()
}

func formatDiagnostic(pointer string, message string) Diagnostic {
	return Diagnostic{
		Code:          CodeInvalidFormat,
		Pointer:       pointer,
		Severity:      SeverityError,
		Specification: "https://spec.open-rpc.org/#openrpc-object",
		Message:       message,
	}
}

func (validator *validator) validateMethodReferences(methods []openrpc.MethodOrReference) {
	for methodIndex, union := range methods {
		base := pointer("methods", methodIndex)
		if reference, ok := union.Reference(); ok {
			validator.validateReference(base+"/$ref", reference)
			continue
		}
		method, ok := union.Method()
		if !ok {
			continue
		}
		if tags, present := method.Tags(); present {
			for index, value := range tags {
				if reference, ok := value.Reference(); ok {
					validator.validateReference(base+"/tags/"+itoa(index)+"/$ref", reference)
				}
			}
		}
		for index, value := range method.Params() {
			if reference, ok := value.Reference(); ok {
				validator.validateReference(base+"/params/"+itoa(index)+"/$ref", reference)
			}
		}
		if result, present := method.Result(); present {
			if reference, ok := result.Reference(); ok {
				validator.validateReference(base+"/result/$ref", reference)
			}
		}
		if values, present := method.Errors(); present {
			for index, value := range values {
				if reference, ok := value.Reference(); ok {
					validator.validateReference(base+"/errors/"+itoa(index)+"/$ref", reference)
				}
			}
		}
		if values, present := method.Links(); present {
			for index, value := range values {
				if reference, ok := value.Reference(); ok {
					validator.validateReference(base+"/links/"+itoa(index)+"/$ref", reference)
				}
			}
		}
		if values, present := method.Examples(); present {
			for index, value := range values {
				pairingBase := base + "/examples/" + itoa(index)
				if reference, ok := value.Reference(); ok {
					validator.validateReference(pairingBase+"/$ref", reference)
				} else if pairing, ok := value.ExamplePairing(); ok {
					validator.validateExamplePairingReferences(pairingBase, pairing)
				}
			}
		}
	}
}

func (validator *validator) validateExamplePairingReferences(base string, pairing openrpc.ExamplePairing) {
	for index, value := range pairing.Params() {
		if reference, ok := value.Reference(); ok {
			validator.validateReference(base+"/params/"+itoa(index)+"/$ref", reference)
		}
	}
	if result, present := pairing.Result(); present {
		if reference, ok := result.Reference(); ok {
			validator.validateReference(base+"/result/$ref", reference)
		}
	}
}

func (validator *validator) validateReference(pointer string, value openrpc.Reference) {
	if validator.stop {
		return
	}
	if _, err := referencevalue.Parse(value.Ref(), referencevalue.DefaultPolicy()); err != nil {
		validator.add(Diagnostic{
			Code:          CodeInvalidReference,
			Pointer:       pointer,
			Severity:      SeverityError,
			Specification: "https://spec.open-rpc.org/#reference-object",
			Message:       "reference must be a valid URI reference",
		})
	}
}

const (
	methodSpecification = "https://spec.open-rpc.org/#method-object"
	linkSpecification   = "https://spec.open-rpc.org/#link-object"
)

type validator struct {
	ctx         context.Context
	options     Options
	diagnostics []Diagnostic
	truncated   bool
	stop        bool
}

func (validator *validator) validateParameters(methodIndex int, method openrpc.Method) {
	seen := make(map[string]bool)
	optionalSeen := false
	for index, union := range method.Params() {
		if validator.stop || validator.canceled() {
			return
		}
		descriptor, ok := union.Descriptor()
		if !ok {
			continue
		}
		if seen[descriptor.Name()] {
			validator.add(Diagnostic{
				Code:          CodeDuplicateParameterName,
				Pointer:       pointer("methods", methodIndex, "params", index, "name"),
				Severity:      SeverityError,
				Specification: methodSpecification,
				Message:       "parameter names must be unique",
			})
		}
		seen[descriptor.Name()] = true
		if descriptor.RequiredOrDefault() {
			if optionalSeen {
				validator.add(Diagnostic{
					Code:          CodeRequiredParameterOrder,
					Pointer:       pointer("methods", methodIndex, "params", index, "required"),
					Severity:      SeverityError,
					Specification: methodSpecification,
					Message:       "required parameters must precede optional parameters",
				})
			}
		} else {
			optionalSeen = true
		}
	}
}

func (validator *validator) validateErrors(methodIndex int, method openrpc.Method) {
	errorsList, present := method.Errors()
	if !present {
		return
	}
	seen := make(map[string]bool)
	for index, union := range errorsList {
		if validator.stop || validator.canceled() {
			return
		}
		object, ok := union.Error()
		if !ok {
			continue
		}
		validator.validateErrorMessage(
			pointer("methods", methodIndex, "errors", index, "message"), object,
		)
		code := object.Code().String()
		if seen[code] {
			validator.add(Diagnostic{
				Code:          CodeDuplicateErrorCode,
				Pointer:       pointer("methods", methodIndex, "errors", index, "code"),
				Severity:      SeverityError,
				Specification: methodSpecification,
				Message:       "method error codes must be unique",
			})
		}
		seen[code] = true
	}
}

func (validator *validator) validateErrorMessage(pointer string, object openrpc.Error) {
	message := object.Message()
	if conciseSentence(message) {
		return
	}
	validator.add(Diagnostic{
		Code:          CodeErrorMessageNotConcise,
		Pointer:       pointer,
		Severity:      SeverityWarning,
		Specification: "https://spec.open-rpc.org/#error-object",
		Message:       "error message should be one concise sentence",
	})
}

func conciseSentence(message string) bool {
	trimmed := strings.TrimSpace(message)
	if trimmed == "" || len([]rune(trimmed)) > 200 || strings.ContainsAny(trimmed, "\r\n") {
		return false
	}
	for index := range trimmed[:len(trimmed)-1] {
		if !strings.ContainsRune(".!?", rune(trimmed[index])) {
			continue
		}
		remainder := strings.TrimSpace(trimmed[index+1:])
		if remainder != "" && !strings.ContainsRune(".!?", rune(remainder[0])) {
			return false
		}
	}
	return true
}

func (validator *validator) validateLinks(base string, getter func() ([]openrpc.LinkOrReference, bool), methodCounts map[string]int) {
	links, present := getter()
	if !present {
		return
	}
	for index, union := range links {
		if validator.stop || validator.canceled() {
			return
		}
		link, ok := union.Link()
		if !ok {
			continue
		}
		validator.validateLink(base+"/"+itoa(index), link, methodCounts)
	}
}

func (validator *validator) validateLink(base string, link openrpc.Link, methodCounts map[string]int) {
	method, present := link.Method()
	if present && methodCounts[method] != 1 {
		validator.add(Diagnostic{
			Code:          CodeUnresolvedLinkMethod,
			Pointer:       base + "/method",
			Severity:      SeverityError,
			Specification: linkSpecification,
			Message:       "link method must resolve to exactly one method",
		})
	}
	if params, present := link.Params(); present {
		validator.validateRuntimeValue(base+"/params", params)
	}
	if server, present := link.Server(); present {
		validator.validateServer(base+"/server", server)
	}
}

func (validator *validator) validateServer(base string, server openrpc.Server) {
	if !strings.Contains(server.URL(), "${") {
		return
	}
	template, err := expression.Parse(server.URL(), expression.DefaultPolicy())
	if err != nil {
		validator.add(runtimeDiagnostic(CodeInvalidRuntimeExpression, base+"/url", "server URL contains an invalid runtime expression"))
		return
	}
	variables, _ := server.Variables()
	bindings := make(map[string]jsonvalue.Value, len(variables))
	for name, variable := range variables {
		value, parseErr := jsonvalue.Parse([]byte(strconv.Quote(variable.Default())), jsonvalue.DefaultPolicy())
		if parseErr != nil {
			validator.add(runtimeDiagnostic(CodeInvalidRuntimeExpression, base+"/url", "server variable cannot be evaluated"))
			return
		}
		bindings[name] = value
	}
	context, _ := expression.NewContext(bindings)
	if _, err := template.Evaluate(context); errors.Is(err, expression.ErrMissingValue) {
		validator.add(runtimeDiagnostic(CodeMissingServerVariable, base+"/url", "server URL references an undefined variable"))
	} else if err != nil {
		validator.add(runtimeDiagnostic(CodeInvalidRuntimeExpression, base+"/url", "server URL expression cannot be evaluated"))
	}
}

func (validator *validator) validateRuntimeValue(base string, value jsonvalue.Value) {
	var decoded any
	if err := json.Unmarshal(value.Bytes(), &decoded); err != nil {
		validator.add(runtimeDiagnostic(CodeInvalidRuntimeExpression, base, "link parameters contain invalid JSON"))
		return
	}
	validator.walkRuntimeValue(base, decoded)
}

func (validator *validator) walkRuntimeValue(base string, value any) {
	if validator.stop {
		return
	}
	switch typed := value.(type) {
	case string:
		if strings.Contains(typed, "${") {
			if _, err := expression.Parse(typed, expression.DefaultPolicy()); err != nil {
				validator.add(runtimeDiagnostic(CodeInvalidRuntimeExpression, base, "link parameter contains an invalid runtime expression"))
			}
		}
	case []any:
		for index, child := range typed {
			validator.walkRuntimeValue(base+"/"+itoa(index), child)
		}
	case map[string]any:
		for _, name := range sortedNames(typed) {
			validator.walkRuntimeValue(base+"/"+escape(name), typed[name])
		}
	}
}

func runtimeDiagnostic(code Code, pointer string, message string) Diagnostic {
	return Diagnostic{
		Code:          code,
		Pointer:       pointer,
		Severity:      SeverityError,
		Specification: "https://spec.open-rpc.org/#runtime-expression",
		Message:       message,
	}
}

func (validator *validator) canceled() bool {
	select {
	case <-validator.ctx.Done():
		if !validator.stop {
			validator.add(Diagnostic{
				Code:          CodeCanceled,
				Pointer:       "#",
				Severity:      SeverityError,
				Specification: "OpenRPC validation policy",
				Message:       "validation canceled",
			})
		}
		validator.stop = true
		return true
	default:
		return false
	}
}

func (validator *validator) add(diagnostic Diagnostic) {
	if validator.stop {
		return
	}
	if len(validator.diagnostics) >= validator.options.MaxDiagnostics {
		validator.truncated = true
		validator.stop = true
		return
	}
	validator.diagnostics = append(validator.diagnostics, diagnostic)
	if validator.options.Mode == FailFast {
		validator.stop = true
	}
}

func (validator *validator) report() Report {
	return Report{
		diagnostics: append([]Diagnostic(nil), validator.diagnostics...),
		truncated:   validator.truncated,
	}
}

func countMethodNames(methods []openrpc.MethodOrReference) map[string]int {
	counts := make(map[string]int)
	for _, union := range methods {
		if method, ok := union.Method(); ok {
			counts[method.Name()]++
		}
	}
	return counts
}

func invalidOptionsDiagnostic() Diagnostic {
	return Diagnostic{
		Code:          CodeInvalidOptions,
		Pointer:       "#",
		Severity:      SeverityError,
		Specification: "OpenRPC validation policy",
		Message:       "validation options are invalid",
	}
}

func resourceLimitDiagnostic(pointer string, message string) Diagnostic {
	return Diagnostic{
		Code:          CodeResourceLimit,
		Pointer:       pointer,
		Severity:      SeverityError,
		Specification: "OpenRPC validation policy",
		Message:       message,
	}
}

func pointer(parts ...any) string {
	result := "#"
	for _, part := range parts {
		switch value := part.(type) {
		case string:
			result += "/" + escape(value)
		case int:
			result += "/" + itoa(value)
		}
	}
	return result
}

func sortedNames[T any](values map[string]T) []string {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func escape(value string) string {
	value = strings.ReplaceAll(value, "~", "~0")
	return strings.ReplaceAll(value, "/", "~1")
}

func itoa(value int) string {
	return strconv.Itoa(value)
}
