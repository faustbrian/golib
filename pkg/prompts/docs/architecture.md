# Architecture

The core module owns prompt definitions, typed results, semantic rendering,
interaction policy, errors, and deterministic execution. It accepts every
ambient resource through `Execution`; it does not inspect the process
environment or standard streams.

The owned inline engine is the core executor. Implementation-time evaluation
retained Huh as comparative evidence because its ambient environment reads and
package-global state conflict with the explicit-resource contract. Any future
Huh, Bubble Tea, Bubbles, or styling adapter must remain dependency-isolated
and absent from the exported core API.

Terminal acquisition is a separate caller-supplied capability. Interaction
requires both caller authority and the capabilities required by the selected
policy. Terminal detection never grants permission by itself. Headless
resolution occurs before any input read or terminal mutation.

Full-screen components, if later justified, belong in a dependency-isolated
nested module. The core module remains suitable for inline prompts, plain
linear fallbacks, and non-interactive use without a TUI dependency.
