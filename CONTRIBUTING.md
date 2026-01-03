# Contributing to gitrespect

Thanks for your interest in contributing! This document outlines the process for contributing to gitrespect.

## How Contributions Work

1. **Fork the repository** to your own GitHub account
2. **Create a feature branch** from `main`
3. **Make your changes** with clear, atomic commits
4. **Submit a Pull Request** back to this repository
5. **Code review** - maintainers will review your PR
6. **Merge** - once approved, your changes will be merged

## Development Setup

```bash
# Clone your fork
git clone https://github.com/YOUR_USERNAME/gitrespect.git
cd gitrespect

# Build
make build

# Run tests
make test

# Test locally
./gitrespect --help
```

## Pull Request Guidelines

### Before Submitting

- [ ] Code compiles without errors (`make build`)
- [ ] Tests pass (`make test`)
- [ ] Code follows existing style patterns
- [ ] New features include tests when applicable
- [ ] README is updated if adding new features

### PR Title Format

Use conventional commit format:
- `feat: add weekly breakdown option`
- `fix: handle empty git repos gracefully`
- `docs: improve installation instructions`
- `refactor: simplify date parsing logic`

### Commit Messages

- Use lowercase
- Be descriptive but concise
- Reference issues when applicable: `fix: handle binary files (#12)`

## Code Style

- Follow standard Go conventions
- Run `gofmt` before committing
- Keep functions focused and small
- Add comments for non-obvious logic

## Reporting Issues

Found a bug or have a feature request? Open an issue with:

- Clear description of the problem or request
- Steps to reproduce (for bugs)
- Expected vs actual behavior
- Your environment (OS, Go version)

## Questions?

Open a discussion or issue if anything is unclear. We're happy to help!

## License

By contributing, you agree that your contributions will be licensed under the MIT License.
