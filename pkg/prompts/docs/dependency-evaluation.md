# Interactive engine evaluation

This evaluation was captured on 2026-07-22 with Go 1.26.5. Version evidence
came from `go list -m -versions`, module metadata, and the projects' GitHub API
repository and release records. Reproducible comparative pseudo-terminal and
minimum-import binary measurements are recorded in [Benchmarks](benchmarks.md).
They are evidence boundaries, not comparative performance claims.

| Candidate | Current stable | Maintenance evidence | Decision |
| --- | --- | --- | --- |
| Huh | `charm.land/huh/v2 v2.0.3` | Released 2026-03-10; repository activity 2026-07-06; MIT | Comparative adapter only; rejected for core execution |
| Bubble Tea | `charm.land/bubbletea/v2 v2.0.8` | Released 2026-07-03; repository activity 2026-07-20; MIT | Transitive engine only |
| Bubbles | `charm.land/bubbles/v2 v2.1.1` | Released 2026-07-04; repository activity 2026-07-20; MIT | Adapter implementation detail only |
| Survey | `github.com/AlecAivazis/survey/v2 v2.3.7` | Repository archived; last release 2023-06-13 | Rejected for maintenance |
| PromptUI | `github.com/manifoldco/promptui v0.9.0` | Last release 2021-10-30; last repository push 2024-08-06 | Rejected for maintenance and narrower forms |
| PromptKit | `github.com/erikgeiser/promptkit v0.12.0` | Released and pushed 2026-07-17; MIT | Credible comparison candidate; narrower field set |
| nao1215/prompt | `github.com/nao1215/prompt v0.0.11` | Released and pushed 2026-07-07; MIT | Credible editor comparison; command-prompt focus |

Huh v2 has the broadest maintained form surface among the evaluated stable
candidates, an explicit accessible mode, context-aware execution, and a
current Bubble Tea v2 base. Source inspection of v2.0.3 also found that
`NewForm` installs `os.Stderr`, reads `TERM`, and obtains identifiers from
package-global mutable state. Those behaviors contradict the explicit-resource
and no-global-state core contracts even when `WithInput` and `WithOutput` are
called afterward.

Huh therefore remains a pinned comparison and possible explicit application
adapter, not the core executor. The owned inline engine must implement the
authoritative model. Any Huh adapter must document the ambient behavior it
cannot suppress and cannot be used to substantiate the core safety claims.

Survey is archived. PromptUI is not archived but its release age and narrower
API make it unsuitable as a new foundation. PromptKit and nao1215/prompt are
maintained credible successors for comparative editing and prompt evidence,
but neither currently displaces Huh for the initial complete form adapter.

No evaluated alternative materially combines Huh's maintained form breadth
with the explicit-resource contract. The package therefore owns the core
inline engine rather than weakening the contract to match an upstream engine.

The core renderer uses `github.com/rivo/uniseg v0.4.7` for Unicode grapheme
segmentation and display-width calculation. It is a focused Unicode dependency,
not an interactive engine, and does not introduce terminal I/O or global
terminal state.

Static search uses `golang.org/x/text v0.40.0` for pinned NFKC normalization
and Unicode case folding. Search semantics do not depend on the host locale.

The optional real-terminal subpackage uses `golang.org/x/term v0.45.0` and its
`golang.org/x/sys v0.47.0` platform primitives for raw state, restoration, and
echo changes. Core prompt definitions and semantic execution do not import the
adapter. `github.com/creack/pty v1.1.24` is test-only evidence for raw-mode and
restoration behavior without touching a developer terminal.

## Primary sources

- <https://github.com/charmbracelet/huh>
- <https://github.com/charmbracelet/bubbletea>
- <https://github.com/charmbracelet/bubbles>
- <https://github.com/AlecAivazis/survey>
- <https://github.com/manifoldco/promptui>
- <https://github.com/erikgeiser/promptkit>
- <https://github.com/nao1215/prompt>
