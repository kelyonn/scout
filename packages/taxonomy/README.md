# packages/taxonomy

Controlled vocabularies: role families, company types, skills, locations. See
[`docs/07-normalization-taxonomy.md`](../../docs/07-normalization-taxonomy.md).

**Not implemented yet.** Populated in M2 alongside the classification cascade.

Planned contents:

| File | Holds |
| --- | --- |
| `roles.yaml` | The 20 role-family leaves, including the `advocacy` family with its technical-evidence gate |
| `companies.yaml` | The curated ~400-employer `company_type` registry (Tier 0 classification) |
| `gcc.yaml` | ~150 `(parent, india_entity, ats_platform, tenant)` entries |
| `skills.yaml` | Skill ontology with `implies` edges |
| `locations.yaml` | Location tiers, aliases, Indian city normalization |

These are hand-maintained data files, and that is the intended design rather
than a shortcut — for the top few hundred employers, a curated list beats
inference on both accuracy and cost.
