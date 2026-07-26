# Adoption examples

## API reads

Cache an API read after authorization inputs are resolved. Include every input
that changes the response in the logical key, including tenant and locale. On a
successful mutation, commit the source transaction and then delete affected
keys. Use plain cache-aside when stale authorization or pricing is unsafe.

## Expensive vendor lookup

Use a short positive TTL, a shorter negative TTL only for authoritative
not-found responses, bounded loader concurrency below the vendor's connection
limit, and refresh jitter. If old vendor data is acceptable during outages,
choose stale-if-error so the caller receives both the old value and error.

```go
result, err := vendors.GetOrLoad(ctx, requestKey, lookupVendor)
if err != nil && result.State != cache.Stale {
	return Response{}, err
}
if err != nil {
	metrics.RecordDegradedVendorResponse()
}
return buildResponse(result.Value), nil
```

## Batch worker

Use `GetMany` to preserve input order and inspect each `BulkResult.Err`. Load
misses with a worker pool whose size is no larger than `MaxConcurrent`; do not
launch an unbounded goroutine per miss. Use `SetMany` for results and retry only
failed mutations. Reject or split input before `MaxBatch` rather than silently
dropping entries.

## Explicit backend bypass

For a reconstructible, non-sensitive read, a use case may fail open:

```go
result, err := objects.Get(ctx, key)
if errors.Is(err, cache.ErrBackend) {
	return source.Get(ctx, key)
}
if err != nil {
	return Object{}, err
}
if result.State == cache.Hit {
	return result.Value, nil
}
return source.Get(ctx, key)
```

Keep bypass code beside the use case. Do not put it in a generic backend wrapper.
