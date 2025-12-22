# Contributing

Thanks for contributing! Please:
- Run `make check` (shfmt + shellcheck) before pushing
- Keep scripts POSIX/Bash portable where possible (Debian 13 target)
- Keep prompts minimal and safe for non-interactive runs
- Update docs (README, AGENTS.md, COPILOT.md) when behavior changes

Development quickstart
- Fork and clone
- Create a branch
- Make changes and run `make check`
- Open a PR; CI will run shellcheck and shfmt
