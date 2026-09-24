# Screening catalog seed

`internal/catalog/seed.json` is editable product data embedded into the binary. `catalog.Load()` validates keys, required text, references, template membership and unique skill/level requirements. Each call returns independent values. New professions and levels require data changes, not Go enums.

The seed contains 236 skills: Engineering Core 16, Backend 52, QA 42, DevOps/SRE 42, Frontend/Fullstack 42 and Analytics 42. Each skill includes an actionable description and an observable evidence signal. The 24 fact types follow the specification. Eight templates select shared core plus relevant professional skills. DevOps and SRE have different selections; Data, Product and System Analyst templates also have different selections. Fullstack selects frontend skills and a focused subset of backend skills.

Junior, Middle and Senior are **editorial starting points**, not validated employer promotion policies. Each template contains per-skill requirements at these levels, with increasing independence and scope. These levels and critical flags must be reviewed with the organization before consequential personnel decisions. Missing evidence means an evidence gap, not proof that a person lacks a skill.

Examples:

- Backend / Idempotency: demonstrate that concurrent replay with the same key returns one committed business effect.
- QA / Concurrency testing: use a controlled interleaving to reproduce or prevent a lost update.
- SRE / Backup verification: record achieved restoration time and the data-loss window from a restore drill.
- Product Analyst / AB test analysis: include effect size, uncertainty and sample-ratio checks.
- System Analyst / Interface specification: cover successful exchanges, validation failures and unavailable dependencies.

Critical flags identify a small role-specific starting set. They are not inferred from missing evidence or automatically assigned by an AI model. Requirement wording is a reusable assessment prompt; employee-specific assessment must reference actual facts and evidence.

The catalog is an initial seed. Persisted organization customization should be versioned by the service and should not be overwritten by startup reseeding. The loader deliberately does not encode category counts, job titles or level names as runtime validation rules; tests verify completeness of this shipped seed.
