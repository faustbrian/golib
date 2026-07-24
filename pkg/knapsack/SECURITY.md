# Security policy

Report vulnerabilities through GitHub private vulnerability reporting. Include
the affected commit, a minimized non-sensitive input, the configured limits,
and whether the issue causes invalid feasibility, panic, hang, leak, race, or
unbounded resource use. Do not publish exploits before coordinated disclosure.

All serialized requests and plans, identifiers, metadata, and visualization
labels are untrusted. Configure explicit conservative limits before accepting
public input. The package never executes expressions or loads plugins from
serialized data.

Custom callbacks are trusted application code, not an untrusted-input
boundary. They run synchronously in the caller's process and Go cannot
preempt arbitrary callback code that ignores context. Do not install callbacks
from untrusted parties; isolate such code in a separately sandboxed process.
