# Custom vocabularies, keywords, and formats

All extensions are registered through `NewCompiler` options and copied into
that compiler. There is no global registry.

`WithVocabulary` associates an absolute vocabulary URI with keyword
compilers. For 2019-09 and 2020-12, a registered vocabulary activates only
when the selected meta-schema declares it. Unknown required vocabularies fail
with `ErrUnsupportedVocabulary`; the declaration's boolean controls unknown
vocabulary handling, not recognized-vocabulary behavior. Historical drafts,
which have no vocabulary negotiation, activate registered keywords directly.
Vocabulary policy is computed per schema resource in a compound document, so
an embedded custom dialect does not affect standard siblings and an embedded
standard dialect does not inherit custom keywords from its parent resource.

Keyword compilation receives a context, dialect, and immutable exact `Value`
for the keyword. It returns an immutable evaluator. Evaluation receives the
request context and an exact immutable instance `Value`, then returns validity
and an optional exact JSON annotation. Compiler and evaluator callbacks must
honor cancellation and must not mutate retained application state. Compile,
call, annotation-byte, output, and total-operation budgets apply.

Panics from keyword compilers, keyword evaluators, and format checkers are
contained at the package boundary and classified as `ErrCallbackPanic` without
including the recovered value in the error text. Resource loaders, supplied
filesystems, and `json.Marshaler` values use the same boundary. If the callback
cancels its context before panicking, cancellation remains the returned error.
Ordinary callback errors remain available to `errors.Is` and `errors.As`, but
their potentially sensitive text is not copied into package diagnostics.

`WithFormat` registers a context-aware `FormatChecker`. Format is an
annotation unless `WithFormatAssertion` or a recognized format-assertion
vocabulary activates assertion. Format callbacks share a dedicated check
budget and should perform deterministic bounded work.

Built-in keyword and vocabulary names cannot be replaced. A keyword name
cannot be registered by two vocabularies in one compiler. Nil interfaces,
duplicate registrations, malformed vocabulary URIs, nil evaluators, invalid
annotations, callback errors, and budget exhaustion are rejected explicitly.
