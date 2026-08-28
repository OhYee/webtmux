# Contributing to webtmux

Thanks for helping improve webtmux. Bug reports, documentation fixes, tests,
and focused feature changes are welcome.

## Before opening an issue

- Search existing issues first.
- For bugs, include the webtmux version, operating system, tmux version,
  browser, command line with secrets removed, and reproduction steps.
- Use a focused feature issue for design questions.
- Report security vulnerabilities through the private process in
  [SECURITY.md](SECURITY.md).

## Development setup

webtmux requires Go 1.23 or newer, Node.js 22 or newer, npm, and tmux.

```bash
git clone https://github.com/OhYee/webtmux.git
cd webtmux
make deps
make dev
make test
```

Source frontend files live under `resources/`. The generated
`bindata/static/js/bundle.js` is committed so Go-only builds remain possible.
When frontend files change, include the regenerated bundle in the same pull
request.

## Pull requests

- Keep each pull request focused on one problem.
- Add or update tests for behavioral changes.
- Run `gofmt` on changed Go files and `make test` before submitting.
- Update user-facing documentation when flags or behavior change.
- Do not commit credentials, local configuration, or files under `builds/`.
- Explain the motivation, user-visible effect, and validation in the pull
  request description.

By contributing, you agree that your contribution is licensed under the MIT
License in [LICENSE](LICENSE).
