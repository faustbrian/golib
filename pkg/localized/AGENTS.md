# Codex project rules

The repository rules supplied by the owner govern all work. In summary:

- implementation proceeds in the current repository and current branch unless
  the owner explicitly requests a new branch or worktree;
- branch setup uses the repository branch workflow and names;
- stage only explicit task files, commit early with Conventional Commit
  subjects and required wrapped bodies, and never amend;
- destructive Git operations, force pushes, verification bypasses, and pushes
  to `main` or `develop` require explicit approval;
- behavior changes use focused tests and the most relevant local checks;
- completion reports exact verification commands and outcomes;
- implementation is fully local and offline until finished; Git remotes,
  pushes, PR state, and hosted CI never block the goal. The owner performs
  hosted CI verification last.

RFC 2119 keywords in the owner's full session instructions remain normative if
this summary omits detail.
