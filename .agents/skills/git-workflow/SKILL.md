---
name: git-workflow
description: Use when creating branches, staging changes, committing, pushing, or preparing a pull request. Apply semantic commits, the repository's branch model, explicit authorization, and dirty-worktree safety.
---

# Git Workflow

## Establish the current state

```sh
git status --short
git branch --show-current
git branch -a
git remote -v
```

Preserve existing work. Do not assume the remote name, base branch, shared testing branch, or deployment behavior.

## Branches

- Without `develop`, use `main` as the base. With `develop`, use it for features and ordinary fixes; hotfixes start from `main` and must be brought back to `develop` through the agreed review flow.
- Use descriptive kebab-case names with `feature/`, `bugfix/`, or `hotfix/`.
- Reuse the intended working branch or existing PR when appropriate. Do not create a separate branch merely because a fix has a different topic.
- Do not switch a shared dirty checkout. Use a worktree when another branch is necessary, without overwriting existing work.
- Ask when the intended base or working branch is ambiguous.

## Preserve user changes

- Never discard, reset, restore, or overwrite changes without explicit authorization for that exact operation.
- Inspect the diff and stage only paths belonging to the requested work.
- Prefer explicit `git add path/to/file path/to/test` over `git add .`.
- Do not rebase, amend, force-push, merge, or change shared history on your own initiative.
- Leave unrelated changes untouched and disclose their exclusion when committing.

## Commits and delivery

- Do not commit, push, or open a PR without explicit user authorization.
- Write Conventional Commits in English: `<type>(<scope>): <imperative summary>`.
- Use `feat`, `fix`, `refactor`, `test`, `docs`, `style`, `build`, `ci`, `perf`, or `chore` as appropriate.
- Include an existing issue/ticket reference when relevant; do not invent one.
- Run the local `quality-gate` immediately before committing. Do not rely on cached or partial checks.
- Inspect `git diff --cached` before the commit, and report the resulting hash afterward.
- Push only the authorized working branch and use the correct PR base. Do not merge a PR or push directly to a shared branch unless explicitly authorized.

Examples:

```text
refactor(checkout): model request state explicitly
test(transport): cover malformed status responses
ci: verify formatting and integration tests
```
