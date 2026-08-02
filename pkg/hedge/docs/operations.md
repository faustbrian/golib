# Operations

Choose a bounded resource identity and share one budget across all executions
for it. Monitor logical operations, hedges started, budget denials, winner
ordinal, all-failure outcomes, total deadlines, cleanup failures, latency, and
the ratio of attempts to logical operations. Keep labels bounded.
`attempt_completed` observations include the bounded classification but never
the result or raw error.

An increasing hedge-win rate can indicate a useful policy or a degrading
dependency; correlate it with base latency, saturation, and endpoint diversity.
Sustained budget denial means the policy is containing amplification and needs
capacity or delay review, not that the downstream failed.

During drain, retain reports and wait with a deadline. A blocked wait identifies
an attempt that has not cooperated; never claim all losers stopped merely
because their contexts were canceled.
