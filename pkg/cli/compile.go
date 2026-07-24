package cli

import (
	"fmt"
	"unicode"
	"unicode/utf8"
)

// Application is an immutable compiled command graph safe for concurrent
// metadata reads.
type Application struct {
	root      *compiledCommand
	commands  map[int]*compiledCommand
	limits    Limits
	exitCodes ExitCodePolicy
}

// Limits bounds hostile construction, argv, completion, and generation work.
type Limits struct {
	MaximumCommandDepth        int
	MaximumCommands            int
	MaximumOptionsPerCommand   int
	MaximumArgumentsPerCommand int
	MaximumArguments           int
	MaximumArgvBytes           int
	MaximumMetadataBytes       int
	MaximumCompletionResults   int
	MaximumCompletionBytes     int
}

// CompileOption configures immutable application compilation.
type CompileOption func(*compileConfiguration) error

type compileConfiguration struct {
	limits    Limits
	exitCodes ExitCodePolicy
}

// WithLimits overrides non-zero fields in the auditable default limits.
func WithLimits(limits Limits) CompileOption {
	return func(configuration *compileConfiguration) error {
		configuration.limits = mergeLimits(configuration.limits, limits)
		return validateLimits(configuration.limits)
	}
}

// ExitCodePolicy maps stable terminal classifications to portable statuses.
// Zero fields retain the documented defaults.
type ExitCodePolicy struct {
	Usage    int
	Command  int
	Canceled int
	Deadline int
	Internal int
}

// WithExitCodePolicy configures application-specific portable exit statuses.
func WithExitCodePolicy(policy ExitCodePolicy) CompileOption {
	return func(configuration *compileConfiguration) error {
		configuration.exitCodes = mergeExitCodePolicy(configuration.exitCodes, policy)
		return validateExitCodePolicy(configuration.exitCodes)
	}
}

type compiledCommand struct {
	id            int
	name          string
	version       string
	aliases       []string
	summary       string
	description   string
	examples      []string
	documentation string
	hidden        bool
	experimental  bool
	deprecated    string
	replacement   string
	children      []*compiledCommand
	arguments     []argumentSpec
	options       []optionSpec
	effective     []optionSpec
	handler       Handler
	validations   []Validation
	middlewares   []Middleware
	preRun        []Handler
	postRun       []Handler
	cleanup       []Handler
	interaction   Interaction
	groups        []optionGroupSpec
}

type compilationState struct {
	nodes       map[*Command]uint8
	nextCommand int
	nextBinding int
	bindings    map[any]string
	limits      Limits
	commands    int
	metadata    int
}

type optionGroupSpec struct {
	kind     optionGroupKind
	bindings []any
	names    []string
}

// Compile validates and snapshots an explicit command graph.
func Compile(root *Command, options ...CompileOption) (*Application, error) {
	if root == nil {
		return nil, newInternalError("compile a nil root command", nil)
	}
	configuration := compileConfiguration{
		limits: defaultLimits(), exitCodes: defaultExitCodePolicy(),
	}
	for _, option := range options {
		if option == nil {
			continue
		}
		if err := option(&configuration); err != nil {
			return nil, err
		}
	}

	state := &compilationState{
		nodes: make(map[*Command]uint8), bindings: make(map[any]string), limits: configuration.limits,
	}
	compiled, err := compileCommand(root, nil, nil, nil, state, 1)
	if err != nil {
		return nil, err
	}

	commands := make(map[int]*compiledCommand)
	annotateOptions(compiled, compiled.name, nil)
	indexCommands(compiled, commands)

	return &Application{
		root: compiled, commands: commands, limits: configuration.limits,
		exitCodes: configuration.exitCodes,
	}, nil
}

func compileCommand(
	command *Command,
	inheritedNames map[string]string,
	inheritedShort map[rune]string,
	inheritedOptions []optionSpec,
	state *compilationState,
	depth int,
) (*compiledCommand, error) {
	if command == nil {
		return nil, newInternalError("nil command in command graph", nil)
	}
	if depth > state.limits.MaximumCommandDepth {
		return nil, newInternalError("command tree exceeds maximum depth", nil)
	}
	switch state.nodes[command] {
	case 1:
		return nil, newInternalError("command cycle detected at "+safeToken(command.name), nil)
	case 2:
		return nil, newInternalError("reused command node "+safeToken(command.name), nil)
	}
	state.nodes[command] = 1
	state.commands++
	if state.commands > state.limits.MaximumCommands {
		return nil, newInternalError("command tree exceeds maximum command count", nil)
	}
	defer func() { state.nodes[command] = 2 }()
	if err := state.addMetadata(
		append(
			[]string{
				command.name, command.version, command.summary, command.description,
				command.documentation, command.deprecated, command.replacement,
			},
			append(cloneStrings(command.aliases), command.examples...)...,
		)...,
	); err != nil {
		return nil, err
	}

	if err := validateName("command name", command.name); err != nil {
		return nil, err
	}
	if err := validateAliases(command); err != nil {
		return nil, err
	}
	if len(command.arguments) > state.limits.MaximumArgumentsPerCommand {
		return nil, newInternalError("command exceeds maximum argument definitions", nil)
	}
	if len(command.options) > state.limits.MaximumOptionsPerCommand {
		return nil, newInternalError("command exceeds maximum option definitions", nil)
	}
	if command.interaction > InteractionForbidden {
		return nil, newInternalError("invalid interaction capability", nil)
	}
	if err := validateLifecycle(command); err != nil {
		return nil, err
	}
	arguments, err := compileArguments(command.arguments, state)
	if err != nil {
		return nil, fmt.Errorf("command %s: %w", safeToken(command.name), err)
	}
	options, childNames, childShort, err := compileOptions(command.options, inheritedNames, inheritedShort, state)
	if err != nil {
		return nil, fmt.Errorf("command %s: %w", safeToken(command.name), err)
	}
	if err := validateChildIdentities(command.children); err != nil {
		return nil, fmt.Errorf("command %s: %w", safeToken(command.name), err)
	}
	if len(arguments) > 0 && len(command.children) > 0 {
		return nil, newInternalError(
			"ambiguous command arguments and subcommands at "+safeToken(command.name),
			nil,
		)
	}

	commandID := state.nextCommand
	state.nextCommand++
	effective := append(append([]optionSpec(nil), inheritedOptions...), options...)
	groups, err := compileGroups(command.groups, effective)
	if err != nil {
		return nil, fmt.Errorf("command %s: %w", safeToken(command.name), err)
	}
	if err := validateGroupSatisfiability(groups, effective); err != nil {
		return nil, fmt.Errorf("command %s: %w", safeToken(command.name), err)
	}
	childInherited := append([]optionSpec(nil), inheritedOptions...)
	for _, option := range options {
		if option.persistent {
			childInherited = append(childInherited, option)
		}
	}
	compiled := &compiledCommand{
		id:            commandID,
		name:          command.name,
		version:       command.version,
		aliases:       cloneStrings(command.aliases),
		summary:       command.summary,
		description:   command.description,
		examples:      cloneStrings(command.examples),
		documentation: command.documentation,
		hidden:        command.hidden,
		experimental:  command.experimental,
		deprecated:    command.deprecated,
		replacement:   command.replacement,
		arguments:     arguments,
		options:       options,
		effective:     effective,
		handler:       command.handler,
		validations:   append([]Validation(nil), command.validations...),
		middlewares:   append([]Middleware(nil), command.middlewares...),
		preRun:        append([]Handler(nil), command.preRun...),
		postRun:       append([]Handler(nil), command.postRun...),
		cleanup:       append([]Handler(nil), command.cleanup...),
		interaction:   command.interaction,
		groups:        groups,
		children:      make([]*compiledCommand, 0, len(command.children)),
	}
	for _, child := range command.children {
		compiledChild, compileErr := compileCommand(
			child,
			childNames,
			childShort,
			childInherited,
			state,
			depth+1,
		)
		if compileErr != nil {
			return nil, compileErr
		}
		compiled.children = append(compiled.children, compiledChild)
	}

	return compiled, nil
}

func defaultLimits() Limits {
	return Limits{
		MaximumCommandDepth:        64,
		MaximumCommands:            4096,
		MaximumOptionsPerCommand:   1024,
		MaximumArgumentsPerCommand: 1024,
		MaximumArguments:           4096,
		MaximumArgvBytes:           1 << 20,
		MaximumMetadataBytes:       1 << 20,
		MaximumCompletionResults:   100,
		MaximumCompletionBytes:     64 << 10,
	}
}

func mergeLimits(defaults, overrides Limits) Limits {
	if overrides.MaximumCommandDepth != 0 {
		defaults.MaximumCommandDepth = overrides.MaximumCommandDepth
	}
	if overrides.MaximumCommands != 0 {
		defaults.MaximumCommands = overrides.MaximumCommands
	}
	if overrides.MaximumOptionsPerCommand != 0 {
		defaults.MaximumOptionsPerCommand = overrides.MaximumOptionsPerCommand
	}
	if overrides.MaximumArgumentsPerCommand != 0 {
		defaults.MaximumArgumentsPerCommand = overrides.MaximumArgumentsPerCommand
	}
	if overrides.MaximumArguments != 0 {
		defaults.MaximumArguments = overrides.MaximumArguments
	}
	if overrides.MaximumArgvBytes != 0 {
		defaults.MaximumArgvBytes = overrides.MaximumArgvBytes
	}
	if overrides.MaximumMetadataBytes != 0 {
		defaults.MaximumMetadataBytes = overrides.MaximumMetadataBytes
	}
	if overrides.MaximumCompletionResults != 0 {
		defaults.MaximumCompletionResults = overrides.MaximumCompletionResults
	}
	if overrides.MaximumCompletionBytes != 0 {
		defaults.MaximumCompletionBytes = overrides.MaximumCompletionBytes
	}
	return defaults
}

func validateLimits(limits Limits) error {
	values := []int{
		limits.MaximumCommandDepth, limits.MaximumCommands,
		limits.MaximumOptionsPerCommand, limits.MaximumArgumentsPerCommand,
		limits.MaximumArguments, limits.MaximumArgvBytes,
		limits.MaximumMetadataBytes,
		limits.MaximumCompletionResults, limits.MaximumCompletionBytes,
	}
	for _, value := range values {
		if value < 1 {
			return newInternalError("limits must be positive", nil)
		}
	}
	return nil
}

func (state *compilationState) addMetadata(values ...string) error {
	for _, value := range values {
		state.metadata += len(value)
		if state.metadata > state.limits.MaximumMetadataBytes {
			return newInternalError("command metadata exceeds maximum bytes", nil)
		}
	}

	return nil
}

func defaultExitCodePolicy() ExitCodePolicy {
	return ExitCodePolicy{Usage: 2, Command: 1, Canceled: 130, Deadline: 124, Internal: 70}
}

func mergeExitCodePolicy(defaults, overrides ExitCodePolicy) ExitCodePolicy {
	if overrides.Usage != 0 {
		defaults.Usage = overrides.Usage
	}
	if overrides.Command != 0 {
		defaults.Command = overrides.Command
	}
	if overrides.Canceled != 0 {
		defaults.Canceled = overrides.Canceled
	}
	if overrides.Deadline != 0 {
		defaults.Deadline = overrides.Deadline
	}
	if overrides.Internal != 0 {
		defaults.Internal = overrides.Internal
	}
	return defaults
}

func validateExitCodePolicy(policy ExitCodePolicy) error {
	for _, code := range []int{
		policy.Usage, policy.Command, policy.Canceled, policy.Deadline, policy.Internal,
	} {
		if code < 1 || code > 255 {
			return newInternalError("exit codes must be between 1 and 255", nil)
		}
	}
	return nil
}

func validateLifecycle(command *Command) error {
	for _, validation := range command.validations {
		if validation == nil {
			return newInternalError("nil command validation", nil)
		}
	}
	for _, middleware := range command.middlewares {
		if middleware == nil {
			return newInternalError("nil command middleware", nil)
		}
	}
	for _, hooks := range [][]Handler{command.preRun, command.postRun, command.cleanup} {
		for _, hook := range hooks {
			if hook == nil {
				return newInternalError("nil command lifecycle hook", nil)
			}
		}
	}

	return nil
}

func validateAliases(command *Command) error {
	seen := map[string]struct{}{command.name: {}}
	for _, alias := range command.aliases {
		if err := validateName("command alias", alias); err != nil {
			return err
		}
		if _, exists := seen[alias]; exists {
			return newInternalError("duplicate command alias "+safeToken(alias), nil)
		}
		seen[alias] = struct{}{}
	}

	return nil
}

func validateChildIdentities(children []*Command) error {
	names := make(map[string]struct{}, len(children))
	for _, child := range children {
		if child == nil {
			return newInternalError("nil command in command graph", nil)
		}
		if _, exists := names[child.name]; exists {
			return newInternalError("duplicate command name "+safeToken(child.name), nil)
		}
		names[child.name] = struct{}{}
	}
	identities := make(map[string]struct{}, len(names))
	for name := range names {
		identities[name] = struct{}{}
	}
	for _, child := range children {
		for _, alias := range child.aliases {
			if _, exists := identities[alias]; exists {
				return newInternalError("duplicate command alias "+safeToken(alias), nil)
			}
			identities[alias] = struct{}{}
		}
	}

	return nil
}

func compileArguments(
	definitions []ArgumentDefinition,
	state *compilationState,
) ([]argumentSpec, error) {
	arguments := make([]argumentSpec, 0, len(definitions))
	seen := make(map[string]struct{}, len(definitions))
	optionalSeen := false
	for index, definition := range definitions {
		if definition == nil {
			return nil, newInternalError("nil argument definition", nil)
		}
		spec := definition.argumentSpecification()
		if err := state.addMetadata(append(
			[]string{spec.name, spec.valueType, spec.description, spec.format}, spec.allowed...,
		)...); err != nil {
			return nil, err
		}
		if err := validateValueContract(spec.valueType, spec.format, spec.hasFormat); err != nil {
			return nil, err
		}
		if err := validateAllowedValues(spec.allowed, nil); err != nil {
			return nil, err
		}
		if spec.parse == nil {
			return nil, newInternalError("argument has a nil parser", nil)
		}
		if spec.secret && spec.completion != nil {
			return nil, newInternalError("secret argument cannot have dynamic completion", nil)
		}
		if spec.binding == nil {
			return nil, newInternalError("argument has no typed binding", nil)
		}
		if owner, exists := state.bindings[spec.binding]; exists {
			return nil, newInternalError("reused argument binding from "+owner, nil)
		}
		state.bindings[spec.binding] = spec.name
		spec.key = state.nextBinding
		state.nextBinding++
		if err := validateName("argument name", spec.name); err != nil {
			return nil, err
		}
		if _, exists := seen[spec.name]; exists {
			return nil, newInternalError("duplicate argument name "+safeToken(spec.name), nil)
		}
		seen[spec.name] = struct{}{}
		switch spec.cardinality {
		case ArgumentRequired:
			if optionalSeen {
				return nil, newInternalError("required argument follows an optional argument", nil)
			}
		case ArgumentOptional:
			optionalSeen = true
		case ArgumentRepeated:
			if index != len(definitions)-1 {
				return nil, newInternalError("repeated argument must be final", nil)
			}
		case ArgumentRemainder:
			if index != len(definitions)-1 {
				return nil, newInternalError("remainder argument must be final", nil)
			}
		default:
			return nil, newInternalError("invalid argument cardinality", nil)
		}
		arguments = append(arguments, spec)
	}

	return arguments, nil
}

func compileOptions(
	definitions []OptionDefinition,
	inheritedNames map[string]string,
	inheritedShort map[rune]string,
	state *compilationState,
) ([]optionSpec, map[string]string, map[rune]string, error) {
	names := cloneStringMap(inheritedNames)
	shorts := cloneRuneMap(inheritedShort)
	localNames := make(map[string]struct{}, len(definitions))
	localShorts := make(map[rune]struct{}, len(definitions))
	options := make([]optionSpec, 0, len(definitions))
	for _, definition := range definitions {
		if definition == nil {
			return nil, nil, nil, newInternalError("nil option definition", nil)
		}
		spec := definition.optionSpecification()
		if err := state.addMetadata(append(
			[]string{spec.name, spec.valueType, spec.description, spec.format}, spec.allowed...,
		)...); err != nil {
			return nil, nil, nil, err
		}
		if err := validateValueContract(spec.valueType, spec.format, spec.hasFormat); err != nil {
			return nil, nil, nil, err
		}
		if err := validateAllowedValues(spec.allowed, func(value string) bool {
			defaultValue, valid := spec.defaultVal.(string)
			return !spec.hasDefault || valid && defaultValue == value
		}); err != nil {
			return nil, nil, nil, err
		}
		if spec.parse == nil {
			return nil, nil, nil, newInternalError("option has a nil parser", nil)
		}
		if spec.secret && spec.completion != nil {
			return nil, nil, nil, newInternalError("secret option cannot have dynamic completion", nil)
		}
		if spec.binding == nil {
			return nil, nil, nil, newInternalError("option has no typed binding", nil)
		}
		if owner, exists := state.bindings[spec.binding]; exists {
			return nil, nil, nil, newInternalError("reused option binding from "+owner, nil)
		}
		state.bindings[spec.binding] = spec.name
		spec.key = state.nextBinding
		state.nextBinding++
		if err := validateOptionName(spec.name); err != nil {
			return nil, nil, nil, err
		}
		if spec.name == "help" || spec.name == "version" || spec.short == 'h' {
			return nil, nil, nil, newInternalError("option uses a reserved help or version name", nil)
		}
		if _, exists := localNames[spec.name]; exists {
			return nil, nil, nil, newInternalError("duplicate option name --"+safeToken(spec.name), nil)
		}
		if _, inherited := inheritedNames[spec.name]; inherited {
			return nil, nil, nil, newInternalError("option --"+safeToken(spec.name)+" shadows inherited option", nil)
		}
		localNames[spec.name] = struct{}{}
		if spec.short != 0 {
			if !isASCIIAlphaNumeric(spec.short) {
				return nil, nil, nil, newInternalError("invalid option shorthand "+safeToken(string(spec.short)), nil)
			}
			if _, exists := localShorts[spec.short]; exists {
				return nil, nil, nil, newInternalError("duplicate option shorthand -"+string(spec.short), nil)
			}
			if _, inherited := inheritedShort[spec.short]; inherited {
				return nil, nil, nil, newInternalError("option -"+string(spec.short)+" shadows inherited option", nil)
			}
			localShorts[spec.short] = struct{}{}
		}
		if spec.persistent {
			names[spec.name] = spec.name
			if spec.short != 0 {
				shorts[spec.short] = spec.name
			}
		}
		options = append(options, spec)
	}

	return options, names, shorts, nil
}

func compileGroups(
	definitions []optionGroupDefinition,
	effective []optionSpec,
) ([]optionGroupSpec, error) {
	available := make(map[any]optionSpec, len(effective))
	for _, option := range effective {
		available[option.binding] = option
	}
	groups := make([]optionGroupSpec, 0, len(definitions))
	for _, definition := range definitions {
		if len(definition.options) < 2 {
			return nil, newInternalError("option group requires at least two options", nil)
		}
		group := optionGroupSpec{kind: definition.kind}
		seen := make(map[any]struct{}, len(definition.options))
		for _, declared := range definition.options {
			if declared == nil {
				return nil, newInternalError("option group contains nil", nil)
			}
			spec := declared.optionSpecification()
			availableSpec, exists := available[spec.binding]
			if !exists {
				return nil, newInternalError("option group contains an unregistered option", nil)
			}
			if _, duplicate := seen[spec.binding]; duplicate {
				return nil, newInternalError("option group contains a duplicate option", nil)
			}
			seen[spec.binding] = struct{}{}
			group.bindings = append(group.bindings, availableSpec.binding)
			group.names = append(group.names, availableSpec.name)
		}
		if definition.kind != optionGroupExclusive && definition.kind != optionGroupTogether {
			return nil, newInternalError("invalid option group kind", nil)
		}
		groups = append(groups, group)
	}

	return groups, nil
}

func validateAllowedValues(allowed []string, defaultMatches func(string) bool) error {
	if allowed == nil {
		return nil
	}
	if len(allowed) == 0 {
		return newInternalError("enum requires at least one allowed value", nil)
	}
	seen := make(map[string]struct{}, len(allowed))
	matchedDefault := defaultMatches == nil
	for _, value := range allowed {
		if !utf8.ValidString(value) {
			return newInternalError("enum contains invalid UTF-8", nil)
		}
		for _, character := range value {
			if isUnsafeTerminalRune(character) {
				return newInternalError("enum contains an unsafe control character", nil)
			}
		}
		if _, duplicate := seen[value]; duplicate {
			return newInternalError("enum contains a duplicate value", nil)
		}
		seen[value] = struct{}{}
		if defaultMatches != nil && defaultMatches(value) {
			matchedDefault = true
		}
	}
	if !matchedDefault {
		return newInternalError("enum default is not in the allowed set", nil)
	}

	return nil
}

func validateValueContract(valueType, format string, hasFormat bool) error {
	if err := validateName("value type", valueType); err != nil {
		return err
	}
	if !hasFormat {
		return nil
	}
	if format == "" || !utf8.ValidString(format) {
		return newInternalError("invalid time format", nil)
	}
	for _, character := range format {
		if isUnsafeTerminalRune(character) {
			return newInternalError("invalid time format", nil)
		}
	}
	return nil
}

func validateGroupSatisfiability(groups []optionGroupSpec, options []optionSpec) error {
	parents := make(map[any]any, len(options))
	forced := make(map[any]bool, len(options))
	for _, option := range options {
		parents[option.binding] = option.binding
		forced[option.binding] = option.required || option.hasDefault
	}
	find := func(binding any) any {
		root := binding
		for parents[root] != root {
			root = parents[root]
		}
		for parents[binding] != binding {
			next := parents[binding]
			parents[binding] = root
			binding = next
		}
		return root
	}
	for _, group := range groups {
		if group.kind != optionGroupTogether {
			continue
		}
		root := find(group.bindings[0])
		for _, binding := range group.bindings[1:] {
			other := find(binding)
			if root != other {
				parents[other] = root
			}
		}
	}
	forcedComponents := make(map[any]bool, len(options))
	for binding, required := range forced {
		if required {
			forcedComponents[find(binding)] = true
		}
	}
	for _, group := range groups {
		if group.kind != optionGroupExclusive {
			continue
		}
		seen := make(map[any]struct{}, len(group.bindings))
		forcedCount := 0
		for _, binding := range group.bindings {
			root := find(binding)
			if _, exists := seen[root]; exists {
				if forcedComponents[root] {
					return newInternalError("option groups cannot be satisfied", nil)
				}
				continue
			}
			seen[root] = struct{}{}
			if forcedComponents[root] {
				forcedCount++
			}
		}
		if forcedCount > 1 {
			return newInternalError("option groups cannot be satisfied", nil)
		}
	}

	return nil
}

func indexCommands(command *compiledCommand, commands map[int]*compiledCommand) {
	commands[command.id] = command
	for _, child := range command.children {
		indexCommands(child, commands)
	}
}

func annotateOptions(command *compiledCommand, path string, inherited []optionSpec) {
	for index := range command.options {
		command.options[index].origin = path
	}
	command.effective = append(append([]optionSpec(nil), inherited...), command.options...)
	childInherited := append([]optionSpec(nil), inherited...)
	for _, option := range command.options {
		if option.persistent {
			childInherited = append(childInherited, option)
		}
	}
	for _, child := range command.children {
		annotateOptions(child, path+" "+child.name, childInherited)
	}
}

func validateOptionName(name string) error {
	if err := validateName("option name", name); err != nil {
		return err
	}
	for _, character := range name {
		if !isASCIIAlphaNumeric(character) && character != '-' {
			return newInternalError("invalid option name "+safeToken(name), nil)
		}
	}

	return nil
}

func validateName(kind, name string) error {
	if name == "" || !utf8.ValidString(name) || name[0] == '-' {
		return newInternalError("invalid "+kind+" "+safeToken(name), nil)
	}
	for _, character := range name {
		if unicode.IsSpace(character) || isUnsafeTerminalRune(character) {
			return newInternalError("invalid "+kind+" "+safeToken(name), nil)
		}
	}

	return nil
}

func isASCIIAlphaNumeric(character rune) bool {
	return character >= 'a' && character <= 'z' ||
		character >= 'A' && character <= 'Z' ||
		character >= '0' && character <= '9'
}

func safeToken(token string) string {
	if token == "" {
		return "<empty>"
	}

	return fmt.Sprintf("%q", token)
}

func cloneStrings(values []string) []string {
	return append([]string(nil), values...)
}

func cloneStringMap(source map[string]string) map[string]string {
	clone := make(map[string]string, len(source))
	for key, value := range source {
		clone[key] = value
	}

	return clone
}

func cloneRuneMap(source map[rune]string) map[rune]string {
	clone := make(map[rune]string, len(source))
	for key, value := range source {
		clone[key] = value
	}

	return clone
}

// CommandMetadata is an immutable view of a compiled command.
type CommandMetadata struct {
	command *compiledCommand
}

// Root returns a read-only view of the root command.
func (application *Application) Root() CommandMetadata {
	if application == nil {
		return CommandMetadata{}
	}

	return CommandMetadata{command: application.root}
}

// Name returns the stable command token.
func (metadata CommandMetadata) Name() string {
	if metadata.command == nil {
		return ""
	}

	return metadata.command.name
}

// Aliases returns a copy of alternate command tokens.
func (metadata CommandMetadata) Aliases() []string {
	if metadata.command == nil {
		return nil
	}

	return cloneStrings(metadata.command.aliases)
}

// Summary returns the one-line summary.
func (metadata CommandMetadata) Summary() string {
	if metadata.command == nil {
		return ""
	}

	return metadata.command.summary
}

// Description returns the long description.
func (metadata CommandMetadata) Description() string {
	if metadata.command == nil {
		return ""
	}

	return metadata.command.description
}

// Examples returns a copy of complete examples.
func (metadata CommandMetadata) Examples() []string {
	if metadata.command == nil {
		return nil
	}

	return cloneStrings(metadata.command.examples)
}

// Documentation returns the related documentation link.
func (metadata CommandMetadata) Documentation() string {
	if metadata.command == nil {
		return ""
	}

	return metadata.command.documentation
}

// Hidden reports whether discovery surfaces omit the command.
func (metadata CommandMetadata) Hidden() bool {
	return metadata.command != nil && metadata.command.hidden
}

// Experimental reports whether compatibility is not yet stable.
func (metadata CommandMetadata) Experimental() bool {
	return metadata.command != nil && metadata.command.experimental
}

// Deprecated returns the deprecation message.
func (metadata CommandMetadata) Deprecated() string {
	if metadata.command == nil {
		return ""
	}

	return metadata.command.deprecated
}

// Replacement returns the preferred replacement command path.
func (metadata CommandMetadata) Replacement() string {
	if metadata.command == nil {
		return ""
	}

	return metadata.command.replacement
}

// Children returns immutable child views in registration order.
func (metadata CommandMetadata) Children() []CommandMetadata {
	if metadata.command == nil {
		return nil
	}
	children := make([]CommandMetadata, len(metadata.command.children))
	for index, child := range metadata.command.children {
		children[index] = CommandMetadata{command: child}
	}

	return children
}

// Options returns local options in registration order.
func (metadata CommandMetadata) Options() []OptionMetadata {
	if metadata.command == nil {
		return nil
	}
	options := make([]OptionMetadata, len(metadata.command.options))
	for index, spec := range metadata.command.options {
		options[index] = OptionMetadata{spec: spec}
	}

	return options
}
