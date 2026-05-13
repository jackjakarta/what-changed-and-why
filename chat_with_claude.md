A "what changed and why" CLI for a file or function. Point at src/whatever.ts:functionName, get: every PR that touched it, the commit messages summarized, related test changes, who's the de facto owner. Wraps git but presents it like a story. Useful in any codebase older than six months. Go fits this — needs to be fast on big repos.

1. "What changed and why" — why
The shape of it:
$ why src/auth/login.ts:validateToken

validateToken — introduced 14 months ago, 11 commits across 6 PRs

  Aug 2024  PR #142 "Initial JWT validation"  @maria
            ─ 80 lines, alongside test/auth.spec.ts
  Oct 2024  PR #189 "Fix clock-skew tolerance"  @jonas
            ─ linked issue: SEC-44, customer report
  Mar 2025  PR #311 "Refactor for refresh tokens"  @maria
            ─ 3 commits, also touched session.ts
            ─ summary: split into validate + refresh paths
  ...

Effective owner: @maria (62% of changes, last touched 3 weeks ago)

It's git log + tree-sitter + your forge API, stitched together by an AI summarizer that groups commits into "phases" with one-liners. The hard-and-interesting part is function-tracking across moves and renames — git log -L is okay for line ranges but flaky when code moves. Tree-sitter lets you identify the function semantically, then walk back through history matching by AST shape, not line number. That's the part nobody does well.

Go fits perfectly — single binary, libgit2 bindings, tree-sitter has Go bindings, performance matters on big monorepos. Output modes: pretty TUI for humans, --json for piping into other tools (or feeding to Claude). Aggressive caching since old commits don't change.
