# Cookbook

## Distinguish missing and null

Use `Exists(path)` to test presence. Compare `Variable(path)` with
`Literal(Null())` to test explicit null. A missing comparison returns false.

## Match a typed list

Use `Compare(OpContains, Variable(tags), Literal(String("express")))` for an
exact list member. The variable must resolve to a list at runtime.

## Add a derived fact

Set `Rule.Derive` to literal facts and use `CollectAll` when downstream rules
must observe them. Give each derived path one producer. Compilation rejects
static cycles and duplicate producers.

## Resolve sparse facts

Compile the plan normally, then call `EvaluateResolved` with a resolver. The
plan requests only missing paths used by its propositions, in lexical order.

## Cache compiled plans

Create `NewMemoryPlanCache(capacity)` and call `CompileCached`. The canonical
definition hash is available through `Plan.Hash`.
