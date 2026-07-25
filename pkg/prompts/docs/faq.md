# FAQ

## Why is interactivity not automatic?

A terminal detector cannot know whether a command, CI job, JSON mode, or
container is allowed to ask a question. Policy and capability are separate.

## Why not expose Huh or Bubble Tea models?

They would make upstream lifecycle and compatibility choices part of every
consumer's contract. The owned model can use or replace an adapter independently.

## Does masking erase a secret?

No. It prevents ordinary representation. See the security documentation for
the byte-cleanup and Go string limits.

## Why does progress not update itself?

Hidden workers and timers complicate cancellation, slow-writer behavior, and
tests. Updates coalesce in memory and the caller chooses when to render.

## Can a prompt be the only way to configure an operation?

No. Adoption examples should expose flags, configuration, or another explicit
input path and prompt only when the command authorizes interaction.
