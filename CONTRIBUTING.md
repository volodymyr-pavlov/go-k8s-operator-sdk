# Contributing

Bug fixes are welcome at any time. For anything bigger, open an issue first, so that we can agree on the approach
before you spend time on it.

## How to send a change

1. Fork the project on GitHub and make a branch.
2. Make the change and run the checks of the workflow: `make fmt vet manifests generate test lint staticcheck`. It needs
   Go 1.27. After a change to the types in `api/`, run `make manifests generate` and commit the generated files.
3. Write the commit message in the [Conventional Commits](https://www.conventionalcommits.org/) style. `fix:` makes a
   patch release, `feat:` a minor release, and `feat!:` or a `BREAKING CHANGE:` footer a major release. The messages
   decide the next version number.
4. Open a pull request against `main`. Say what you changed and why.

## How the code is written

- A doc comment follows the godoc style. It starts with the name that it documents, uses whole sentences and wraps at
  120 columns. Comments inside functions are rare and say why, not what.
- A reconcile is idempotent. It reads the state of AWS, compares it with the spec and changes only the difference.
- The operator never changes or deletes a bucket that it did not create.
- A composite literal gives each field a line of its own. In a test, this holds for literals of keyed fields, so that
  tables of test data stay compact. `make check-style` finds the literals that break it, and `make fix-style` rewrites
  them.
- Every error is declared in the `errors.go` of its package, each with its own `var`, and each package-level `var`
  stands on its own, not in a `var ( ... )` group. `make check-style` checks both.
- Do not silence a linter with a `//nolint` comment. Change the code, or change `.golangci.yml` in a pull request of
  its own.
- Tests use small hand-written stand-ins for AWS, not generated mocks.

## Git hooks

`make hooks` installs the hooks of `lefthook.yml`. Before a commit, they run `gofmt`, `make check-style` and `make lint` on
the staged files, in parallel. They also check that the first line of the commit message starts with a type, such as
`feat(controller): ...`, because semantic-release reads it. `lefthook` is built from the `tools` module, so nothing else
needs to be installed. The workflow runs the same checks again, so a hook that was skipped with `--no-verify` is found
there.

## The workflow

The workflow in `.github/workflows/ci.yml` runs on every pull request and on `main`. It builds and deploys nothing.

| Job         | Command                              | What it checks                                                    |
| ----------- | ------------------------------------ | ----------------------------------------------------------------- |
| `fmt-vet`   | `make fmt vet`                       | `gofmt` and `go vet`.                                             |
| `generated` | `make manifests generate`            | That the CRD, the RBAC rules and the generated code are current.  |
| `test`      | `make test`                          | The tests, with envtest.                                          |
| `integration` | `go test -tags integration`      | The S3 client against the Floci S3 emulator, started as a service. |
| `lint`      | `make lint`                          | `golangci-lint`, with the rules in `.golangci.yml`.               |
| `staticcheck` | `make staticcheck`                 | `staticcheck`.                                                    |
| `style`       | `make check-style`                   | A line for each field of a literal, no `var` groups, and every error in `errors.go`. |
| `vulncheck` | `govulncheck`                        | Known vulnerabilities in the code that the operator calls.        |
| `release`   | `semantic-release`                   | On `main` only. See below.                                        |

`golangci-lint` and `staticcheck` are dependencies of a separate Go module, `tools`, so they do not touch the `go.sum`
of the operator. To update one, run `go get -modfile=tools/go.mod <module>@<version>`.

## Releases

Merging to `main` is all it takes. The `release` job runs semantic-release. It writes the notes of the release into
`CHANGELOG.md`, commits that file to `main` as `chore(release): X.Y.Z`, creates the `vX.Y.Z` tag, and creates a GitHub
Release with the same notes. The release commit starts no workflow of its own. Nothing is deployed and no image is
published.

The job uses the workflow's own `GITHUB_TOKEN`, which may push the commit and create the release. Do not edit
`CHANGELOG.md` by hand. The commit messages generate it.

`.releaserc.yml` names the plugins in the order that they run. `release/package.json` pins semantic-release and every
plugin to an exact version, and `release/package-lock.json` locks the rest. To bump a version, change it in
`release/package.json`, run `npm install --prefix release --package-lock-only`, and commit both files.

## Pinned images and actions

The S3 emulator image of the workflow is pinned to an exact digest. To update it, run
`docker buildx imagetools inspect <image>:<tag>` and replace the tag and the digest together. The actions of the workflow
are pinned to a commit the same way: `git ls-remote --tags https://github.com/<action> refs/tags/<tag>` gives the commit.

## Licensing

By contributing, you agree that your changes are published under the MIT License in `LICENSE`.
