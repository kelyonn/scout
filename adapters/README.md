# adapters

One module per source type. See
[`docs/05-source-catalog.md`](../docs/05-source-catalog.md) and the "Adding an
adapter" section of [`CONTRIBUTING.md`](../CONTRIBUTING.md).

**Not implemented yet.** Greenhouse, Lever, and Ashby land in M1.

Planned layout:

```
adapters/<category>/<name>/
├── adapter.go        Fetch, Parse, Validate
├── adapter_test.go
├── fixtures/         recorded responses
└── fixtures/expected/
```

Two rules that are not negotiable:

- **Every outbound request goes through `PolitenessGate.Allow()`.** No direct
  `http.Get`, not even in a test.
- **`Parse` must be pure and deterministic.** That is what makes fixture replay
  possible, and fixture replay is what catches an upstream format change before
  it reaches production.
