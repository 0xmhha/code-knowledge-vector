# Policy Files

Policy YAML files classify code chunks by path and attach
`ModificationGuidance` (also_review, required_tests, watch_out) that
travels with every search hit. The agent reads these hints to know
which neighbouring areas it should inspect, which tests to add, and
what risks to watch out for when modifying the chunk.

## Files

| File | Use |
|------|-----|
| `stablenet.yaml` | Categories for the `go-stablenet` repo |

To use a policy file during build / reindex:

```sh
ckv build   --src ../go-stablenet --out ./ckv-data \
            --policy policy/stablenet.yaml
ckv reindex --src ../go-stablenet --out ./ckv-data \
            --policy policy/stablenet.yaml
```

Search responses then carry `category` and `guidance` on every hit
(MCP `schema_version: "1.1"`).

## Authoring a category

```yaml
- name: <short-name>          # required, unique
  paths:                      # required, ≥1 glob
    - "consensus/**"
    - "miner/**"
  also_review:                # optional, free-form names
    - "state"
  required_tests:             # optional, ≥1 string per test suite
    - "consensus integration"
  watch_out:                  # optional, ≥1 risk line
    - "validator set change requires hard-fork coordination"
```

### Glob rules

- `*` matches one path segment
- `**` matches zero or more segments
- Patterns are anchored to the chunk's repo-relative `File` field

### Match order

Categories are evaluated top-down; the first match wins. Place the
most specific rules first. The convention is:

1. `test` first (so `_test.go` does not pick up production guidance)
2. Sensitivity-critical categories (`consensus`, `state`, `crypto`)
3. Functional areas (`p2p`, `txpool`, `rpc`)
4. Configuration (`params`, `systemcontracts`)
5. Catch-all (`cli`, `docs`)

### Coverage target

Unclassified ratio should stay ≤ 30%. Run

```sh
ckv build --src <repo> --out /tmp/ckv-coverage \
          --policy policy/stablenet.yaml
sqlite3 /tmp/ckv-coverage/vector.db \
  "SELECT category, COUNT(*) FROM chunks GROUP BY category ORDER BY 2 DESC;"
```

after major edits to verify the distribution still looks reasonable.

## When to update

- New top-level directory in the target repo → add a `paths` entry to
  the most specific category, or create a new one if the directory
  has a unique sensitivity profile.
- New required test class or post-mortem learning → extend
  `required_tests` / `watch_out` of the matching category.
- Sensitivity reclassification (e.g. an area becomes consensus-critical
  after a refactor) → move its globs to the higher-sensitivity category
  and rebuild the index so existing chunks update.
