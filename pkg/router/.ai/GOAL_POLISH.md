# Goal: Add Fair Router Comparisons

## Objective

Add correctness-gated router comparison evidence while preserving `net/http`
as the implemented public runtime model.

## Required Work

- Compare `http.ServeMux`, Chi, Gin router, Echo router, httprouter, BunRouter,
  and Gorilla mux using identical route tables and supported semantics.
- Separate static, parameter, wildcard, miss, method, group, mount, middleware,
  URL generation, startup compilation, and realistic route-table tracks.
- Prove matched routes, path values, methods, redirects, HEAD/OPTIONS, errors,
  and output before accepting timing results.
- Keep Fiber/fasthttp in a separately labeled architecture track including
  adapter, context, transport, and lifecycle costs.
- Never present router-only dispatch as a complete framework comparison.
- Store pinned versions, harness source, fixtures, raw output, profiles,
  environment metadata, and statistical analysis.

## Completion Criteria

- Comparative claims are reproducible and semantically fair.
- Fasthttp results are not ranked as direct `net/http` equivalents.
- Performance docs explain measured advantages and intentional boundaries.

