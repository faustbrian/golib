# Resolution and catalogs

`resolve.Resolver` is the only compilation I/O boundary. The default resolver
denies every request. `resolve.Memory` is suitable for tests, embedded schemas,
and applications that already control all resource bytes.

`resolve.File` is an opt-in local filesystem capability. It accepts only
hostless absolute `file` URIs beneath one absolute configured root, confines
opens with `os.Root`, rejects symlink and traversal escapes, and caps each
resource with `FileOptions.MaxBytes` (16 MiB by default). Close it when the
compiler no longer needs it:

```go
files, err := resolve.NewFile(resolve.FileOptions{
    Root:     "/srv/schemas",
    MaxBytes: 4 << 20,
})
if err != nil {
    return err
}
defer files.Close()
```

`resolve.Catalog` maps import namespaces to absolute resource identities. It
enables `xs:import` without `schemaLocation` while leaving byte access with an
explicit underlying resolver:

```go
catalog, err := resolve.NewCatalog(map[string]string{
    "urn:orders": "file:///srv/schemas/orders.xsd",
}, files)
if err != nil {
    return err
}
```

Explicit schema locations pass through unchanged. A missing locationless
mapping behaves as an unavailable optional hint; compilation still rejects
any declaration that later depends on unresolved imported components.

Resolvers receive the absolute URI, requested namespace, and reference kind.
They must return bytes with the exact requested resource identity. Compiler
limits bound schemas, references, depth, components, particles, and total
bytes.

There is no built-in HTTP resolver. Applications needing one should implement
`resolve.Resolver`, enforce an allowlist, cap response bytes and redirects,
and avoid forwarding ambient credentials. Use `resolve.File` instead of
mapping an untrusted schema location directly to a filesystem path.
