# Forms

`Form` groups heterogeneous typed prompts without erasing their execution or
result contracts. `AsField` wraps a prompt, `When` adds a condition evaluated
against prior answers, and `RunForm` executes enabled fields in declaration
order.

```go
form, err := prompts.NewForm(prompts.FormConfig{
    ID: "setup",
    Fields: []prompts.FormField{
        prompts.AsField(namePrompt),
        prompts.AsField(advancedPrompt),
        prompts.When(prompts.AsField(detailsPrompt), func(result prompts.FormResult) bool {
            advanced, ok := prompts.FormValue[bool](result, "advanced")
            return ok && advanced
        }),
    },
})
if err != nil {
    return err
}

result, err := prompts.RunForm(ctx, form, execution)
if err != nil {
    return err
}
name, ok := prompts.FormValue[string](result, "name")
```

Field identities must be unique. Conditions only observe completed earlier
fields and cannot mutate result storage. Every `FormValue` call returns a
defensive copy when the underlying prompt defines clone semantics, including
multi-select slices and byte-oriented secrets. Conditions and cross-field
validators are panic-contained and checked for cancellation after each call.

`FormConfig.Dependencies` supplies form-local validation dependencies;
`Execution.Dependencies` overrides it for a particular run. Cross-field
validators return field-addressable `ValidationIssue` values. If a validator
message contains a stored `SecretValue` or `SecretBytes` value, the form
replaces its code and message with a generic safe issue while retaining the
relevant field identities.

`FormResult.DestroySecrets` best-effort destroys byte-secret wrappers owned by
the result. Previously returned `FormValue` copies remain caller-owned and must
be destroyed separately. String-backed values retain the Go memory limitations
described in [Secret handling](secrets.md).

Forms reuse the supplied execution resources for each field. The current
implementation acquires and releases the explicit terminal controller once per
interactive field; a later session adapter may optimize acquisition while
preserving the same cleanup boundary and typed results.

In interactive forms, Tab submits the current field and advances in declaration
order. Shift-Tab returns to the nearest enabled prior field. Text, selection,
search, and byte-secret drafts are execution-local and restored when revisited;
byte draft copies are destroyed when the form returns. Revising an earlier
answer removes downstream results and reevaluates conditional fields, so hidden
answers cannot survive a changed condition. Navigation state is never retained
by the reusable `Form` or prompt definitions.
