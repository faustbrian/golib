# API compatibility baseline

`v1.export` is module export data generated with the pinned `apidiff` version:

```sh
go run golang.org/x/exp/cmd/apidiff@v0.0.0-20260709172345-9ea1abe57597 \
  -m -w api/v1.export github.com/faustbrian/golib/pkg/openrpc
```

`make api` rejects incompatible exported API changes relative to this baseline.
