# packages/prompts

Versioned prompt files. See
[`docs/10-ai-features.md`](../../docs/10-ai-features.md) and the "Adding a
prompt" section of [`CONTRIBUTING.md`](../../CONTRIBUTING.md).

**Not implemented yet.** Lands in M2 with the LLM cascade.

Naming is `<task>.v<N>.md` with front-matter declaring tier, output schema,
token budget, and eval set path. Prompts are versioned rather than edited in
place because a prompt change can silently degrade classification across the
whole corpus — so it gets the same review as code, and an eval diff attached to
the PR.
