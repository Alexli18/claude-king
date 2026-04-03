# Contributing to Claude King

Thank you for your interest in contributing!

## Requirements

- Go 1.22+
- Unix-like OS (macOS or Linux)

## Getting Started

```bash
git clone https://github.com/alexli18/claude-king
cd claude-king
make build
```

## Development

```bash
make test         # run tests
make vet          # run go vet
make build        # build all binaries
make install      # install to /usr/local/bin (requires sudo)
make install-user # install to ~/.local/bin, auto-patches PATH in .zshrc/.bashrc
make clean        # remove built binaries
```

## Submitting Changes

1. Fork the repository
2. Create a feature branch: `git checkout -b feat/my-feature`
3. Make your changes
4. Run `make test` and ensure all tests pass
5. Run `make vet` and fix any issues
6. Submit a pull request

## Code Style

Follow standard Go conventions. Run `gofmt` before committing.

## Reporting Bugs

Open an issue at https://github.com/alexli18/claude-king/issues with:
- Go version (`go version`)
- OS and architecture
- Steps to reproduce
- Expected vs actual behavior
