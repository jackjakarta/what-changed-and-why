# `wcaw --json` schema

Stable wire format emitted when `wcaw` is invoked with `--json`. Schema is versioned via the top-level `schema_version` integer; a bump signals a breaking change. The current version is **`1`**.

See `internal/render/testdata/sample.json` for a complete example.

## Top-level shape

```json
{
  "schema_version": 1,
  "symbol":  { ... },
  "summary": { ... },
  "groups":  [ ... ],
  "owner":   { ... } | null
}
```

| Key              | Type     | Notes                                                          |
|------------------|----------|----------------------------------------------------------------|
| `schema_version` | integer  | Always present. `1` for the current format.                    |
| `symbol`         | object   | The resolved input symbol; see [Symbol](#symbol).              |
| `summary`        | object   | Aggregate counts; see [Summary](#summary).                     |
| `groups`         | array    | One element per PR bucket, **oldest-first**. See [Group](#group). |
| `owner`          | object   | Effective owner; `null` when no commit touched the symbol.     |

`groups` is empty (`[]`) when there is no history (e.g. an untracked working-tree file). `owner` is `null` in the same case.

## Symbol

```json
"symbol": {
  "name": "validateToken",
  "kind": "function",
  "path": "src/auth/login.ts",
  "start_line": 14,
  "end_line": 32,
  "start_byte": 220,
  "end_byte": 612
}
```

| Field          | Type    | Notes                                                                                       |
|----------------|---------|---------------------------------------------------------------------------------------------|
| `name`         | string  | Symbol name as resolved from the working-tree file.                                         |
| `kind`         | string  | One of `function`, `method`, `arrow-const`, `unknown`.                                      |
| `path`         | string  | Repo-relative path (forward slashes).                                                       |
| `start_line`   | integer | 1-indexed inclusive.                                                                        |
| `end_line`     | integer | 1-indexed inclusive.                                                                        |
| `start_byte`   | integer | 0-indexed inclusive.                                                                        |
| `end_byte`     | integer | 0-indexed exclusive (matches tree-sitter convention).                                       |

## Summary

```json
"summary": {
  "introduced_at":    "2024-08-12T10:00:00Z",
  "touching_commits": 11,
  "total_commits":    13,
  "pr_count":         6
}
```

| Field              | Type           | Notes                                                                                                  |
|--------------------|----------------|--------------------------------------------------------------------------------------------------------|
| `introduced_at`    | string \| null | RFC 3339 timestamp of the oldest commit in the symbol's traced history. `null` when history is empty. |
| `touching_commits` | integer        | Count of commits whose `class` is not `unrelated`/`unknown`.                                          |
| `total_commits`    | integer        | All commits in `groups[*].commits` combined.                                                          |
| `pr_count`         | integer        | Number of groups with a non-null `pull`.                                                              |

## Group

```json
{
  "pull":       { ... } | null,
  "summary":    "tightened JWT expiry tolerance after SEC-44 incident",
  "commits":    [ ... ],
  "test_files": [ "..." ]
}
```

| Field        | Type                  | Notes                                                                                          |
|--------------|-----------------------|------------------------------------------------------------------------------------------------|
| `pull`       | object \| null        | `null` for the "no-PR" bucket. See [Pull](#pull).                                              |
| `summary`    | string                | Optional one-line LLM-generated summary of the PR (or no-PR bucket). `""` when the summariser is disabled, unavailable, or failed. Always present. |
| `commits`    | array&lt;Commit&gt;   | Commits in this group, **oldest-first** (chronological).                                       |
| `test_files` | array&lt;string&gt;   | Repo-relative paths of test files touched by any commit in the group. Always present (may be empty). |

### Pull

```json
"pull": {
  "number":    142,
  "title":     "Initial JWT validation",
  "author":    "maria",
  "url":       "https://github.com/o/r/pull/142",
  "merged_at": "2024-08-15T08:32:00Z",
  "state":     "closed",
  "merge_sha": "deadbeef…",
  "issues":    [ ... ]
}
```

| Field       | Type                       | Notes                                                                       |
|-------------|----------------------------|-----------------------------------------------------------------------------|
| `number`    | integer                    | PR number (GitHub).                                                         |
| `title`     | string                     | PR title.                                                                   |
| `author`    | string                     | Login, no leading `@`. May be `""` if the API returned no author.           |
| `url`       | string                     | HTML URL of the PR.                                                         |
| `merged_at` | string \| null             | RFC 3339; `null` if unmerged.                                               |
| `state`     | string                     | `open` or `closed`.                                                         |
| `merge_sha` | string                     | Merge commit SHA when known; otherwise `""`.                                |
| `issues`    | array&lt;IssueRef&gt;      | Extracted from title + body. Always present (may be empty).                 |

The PR `body` field is **not** emitted by design — it can be large and consumers can fetch it from `url` when needed. May change in a future schema version.

#### IssueRef

```json
{ "raw": "SEC-44", "project": "SEC", "number": 44 }
```

| Field     | Type    | Notes                                                                                       |
|-----------|---------|---------------------------------------------------------------------------------------------|
| `raw`     | string  | Exact substring matched, e.g. `#91` or `SEC-44`.                                            |
| `project` | string  | `""` for GitHub-style `#N` refs; the project key otherwise (e.g. `SEC`).                    |
| `number`  | integer | Issue number.                                                                               |

### Commit

```json
{
  "hash":    "a709906f…",
  "date":    "2024-08-12T10:00:00Z",
  "author":  "maria",
  "subject": "feat: phase 2",
  "class":   "introduced",
  "symbol":  { ... } | null
}
```

| Field     | Type                   | Notes                                                                            |
|-----------|------------------------|----------------------------------------------------------------------------------|
| `hash`    | string                 | Full SHA-1.                                                                      |
| `date`    | string                 | Author date, RFC 3339.                                                           |
| `author`  | string                 | Git author name (free-form, not a login).                                        |
| `subject` | string                 | First line of the commit message.                                                |
| `class`   | string                 | One of `introduced`, `modified`, `renamed`, `moved-from`, `unrelated`, `unknown`. |
| `symbol`  | object \| null         | Per-commit symbol position; `null` when the symbol didn't change at this commit. |

#### CommitSymbol

```json
{
  "name":        "validateToken",
  "prev_name":   "",
  "source_file": "",
  "start_line":  14,
  "end_line":    32
}
```

| Field         | Type    | Notes                                                                                |
|---------------|---------|--------------------------------------------------------------------------------------|
| `name`        | string  | Symbol name at this commit.                                                          |
| `prev_name`   | string  | Set on `renamed` (or `moved-from` with a rename); empty otherwise.                   |
| `source_file` | string  | Repo-relative path of the originating file; set on `moved-from`, empty otherwise.    |
| `start_line`  | integer | 1-indexed.                                                                           |
| `end_line`    | integer | 1-indexed.                                                                           |

## Owner

```json
"owner": {
  "name":         "maria",
  "commits":      7,
  "total":        11,
  "percent":      64,
  "last_touched": "2025-04-12T00:00:00Z"
}
```

`owner` is `null` when no commit qualifies (empty history, or every commit is `unrelated`/`unknown`).

| Field         | Type    | Notes                                                                                |
|---------------|---------|--------------------------------------------------------------------------------------|
| `name`        | string  | Author name with the most touching commits (tie-break: latest-touched then a→z).     |
| `commits`     | integer | Touching commits attributed to this author.                                          |
| `total`       | integer | Total touching commits (denominator).                                                |
| `percent`     | integer | `round(commits * 100 / total)`.                                                      |
| `last_touched`| string  | Author's most recent touching commit, RFC 3339.                                      |

## Stability

The `schema_version` value is **`1`** today. Any of the following constitutes a breaking change and bumps the version:

- Removing a field or changing its type.
- Renaming a field.
- Changing the meaning of an existing enum value, or removing one.
- Reordering elements where the order is part of the contract (e.g. `groups`).

Additive changes (new optional fields, new enum values appended) do **not** bump the version. Consumers should ignore unknown fields.
