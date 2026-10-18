# Migrating from Atlassian Statuspage

The migration command is the product. It never mutates your Statuspage
account: `--dry-run` is the default behavior and the only implemented mode for
inspection — you always see the exact report before anything is imported.

```bash
outpage migrate --from=statuspage --dry-run --format=json \
  --input statuspage-export.json
```

`--input` defaults to `testdata/fixtures/statuspage/sample-export.json`
(a synthetic fixture, no real emails).

## What the report tells you

Every exported entity is classified into exactly one of three results:

- `mapped` — the entity moves to an Outpage equivalent with full fidelity.
- `partial` — the entity moves, but some fields have no Outpage equivalent and
  are dropped (named per-field).
- `unmapped` — the entity has no Outpage equivalent and is not imported.

Plus a `risks` list: things that *look* migrated but need operator attention
(for example, webhook signing secrets never appear in a Statuspage export and
must be re-entered per subscriber).

Field-by-field mapping and honest gap list:
[fidelity/statuspage.md](fidelity/statuspage.md).
