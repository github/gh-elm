# Contributing

Hi there! We're thrilled that you'd like to contribute to `gh-elm`. Your help
is essential for keeping it great.

Contributions to this project are
[released](https://docs.github.com/site-policy/github-terms/github-terms-of-service#6-contributions-under-repository-license)
to the public under the project's [MIT license](LICENSE).

This project is released with a [Contributor Code of Conduct](CODE_OF_CONDUCT.md).
By participating in this project, you agree to abide by its terms.

## Bugs, features, and questions

Use [GitHub Issues](https://github.com/github/gh-elm/issues) to report
reproducible bugs or propose features. Search existing issues before opening a
new one to avoid duplicates.

For usage questions and general feedback, use
[GitHub Discussions](https://github.com/github/gh-elm/discussions). For
customer-specific migration or GitHub Enterprise Server assistance, contact
[GitHub Enterprise Support](https://support.github.com).

## Prerequisites

Before building and testing the project, install:

- [Go](https://go.dev/doc/install), using the version declared in [`go.mod`](go.mod).
- [GitHub CLI](https://cli.github.com/).

The repository's lint script downloads the configured version of
`golangci-lint` when needed.

## Submitting a pull request

1. [Fork](https://github.com/github/gh-elm/fork) and clone the repository.
2. Create a branch for your change.
3. Make a focused change and add or update tests where appropriate.
4. Run `make audit` to format, vet, lint, and test the project.
5. Push your branch and [open a pull request](https://github.com/github/gh-elm/compare).

Keeping unrelated changes in separate pull requests makes them easier to
review and merge.
