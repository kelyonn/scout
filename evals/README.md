# evals

Golden sets and eval harnesses. See
[`docs/17-testing-qa.md`](../docs/17-testing-qa.md).

**Not implemented yet.** Wired into CI as a gate in M2.

Planned suites: `classification`, `dedup`, `ranking`, `paid-inference`,
`advocacy-classification`.

The gate exists because these failures are silent. A threshold change that drops
dedup precision from 0.99 to 0.94 crashes nothing and breaks the product — CI
gates dedup precision at 0.99 and recall at 0.92 for exactly that reason.

**Every production quality bug becomes a golden-set entry before it is fixed.**
