# Contributing to VRCove

Thanks for helping out. This page covers how to branch, commit and open a pull request. Be decent to other contributors and reviewers.

## Branches

`main` is always releasable. Don't commit to it directly; work on a branch and open a pull request.

Name branches `type/short-description`, lowercase with hyphens:

| Prefix      | Use for                                  | Example                        |
| ----------- | ---------------------------------------- | ------------------------------ |
| `feature/`  | New features                             | `feature/avatar-collections`   |
| `fix/`      | Bug fixes                                | `fix/login-redirect-loop`      |
| `docs/`     | Documentation only                       | `docs/desktop-build-steps`     |
| `refactor/` | Code changes with no behaviour change    | `refactor/router-cleanup`      |
| `chore/`    | Tooling, dependencies, CI                | `chore/bump-vite`              |

Start from an up-to-date `main`:

```sh
git switch main
git pull
git switch -c feature/my-feature
```

Keep one topic per branch. If your branch falls behind, rebase it on `main` rather than merging `main` into it.

## Commits

- Write the subject in the imperative, under about 72 characters: "Add avatar search filters", not "added filters".
- Explain *why* in the body when it isn't obvious.
- Each commit should build and pass tests on its own where practical.

## Before you open a PR

Run the checks that apply to what you changed:

```sh
npm run lint
npm run typecheck
npm run format:check
npm run test:frontend
go test ./...
```

For the desktop app, also run `go test ./...` inside `companion/`.

## Pull requests

- Keep them small and focused. Large, mixed pull requests that take forever to review will be ignored or closed.
- Use a clear title and describe what changed, why, and how you tested it. Add screenshots for UI changes.
- Link any related issue (`Closes #123`).
- Open a draft PR if you want early feedback.
- Respond to review comments with new commits; we squash when merging.
- Don't include unrelated formatting changes, secrets, databases or encryption keys.

## AI-assisted contributions

Allowed. Review what you give the tool and everything it produces before you submit it. You are responsible for every line in your pull request.
