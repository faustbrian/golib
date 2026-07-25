# Cookbook

## Custom errors

Pass `WithNotFound` and `WithMethodNotAllowed` to `New`. The router sets `Allow`
before calling the 405 handler. Keep responses minimal and content negotiation
inside those explicit handlers.

## Public and private groups

Attach authentication middleware to a private group and use distinct metadata
such as `visibility=private`. Metadata collisions fail, so a nested route
cannot silently relabel inherited policy.

## Excluding generic middleware

Give router middleware stable names and list an inherited name in
`Route.ExcludeMiddleware`. Exclusion is resolved at compilation and appears in
the route table; no global alias registry is consulted.

## Exact roots and subtrees

Use `/health` for one exact literal, `/docs/{$}` for an exact slash root, and
`/assets/{path...}` for a non-empty subtree. Use `Mount` when ownership should
remain with another handler.

## Testing

Use `routertest.MustCompile`, `Serve`, `AssertStatus`, and `RouteTable` for
small consumer tests. Differential, fuzz, race, and security fixtures in this
repository demonstrate deeper package tests.
