# Contributing to Wand

We welcome both issue reports and pull requests! Please follow these guidelines to help maintainers respond effectively.

## Issues

**Before opening a new issue:**

1.  **Search**: Use the search tool to check for existing issues or feature requests.
2.  **Review**: Review existing issues and provide feedback or react to them.
3.  **Language**: Use English for all communications — it is the language all maintainers read and write.
4.  **Discussions**: For questions, configuration, or deployment problems, please use GitHub Discussions.
5.  **Security**: For bug reports involving sensitive security issues, please email the maintainers directly instead of posting publicly.

**Reporting a bug:**

1.  Please provide a clear description of your issue, and a minimal reproducible code example if possible.
2.  Include the Wand version (or commit reference), Go version, and operating system.
3.  Indicate whether you can reproduce the bug and describe steps to do so.
4.  Attach relevant logs if available.

**Feature requests:**

1.  Before opening a request, check that a similar idea hasn’t already been suggested.
2.  Clearly describe your proposed feature and its benefits.

## Pull Requests

Please ensure your pull request meets the following requirements:

1.  **Branch**: Open your pull request against the `main` branch.
2.  **Commits**: Keep your commit history clean. Squash commits if necessary to have logical units of work.
3.  **Tests**: Ensure all tests pass.
    ```bash
    go test ./...
    ```
4.  **Coverage**: Add or modify tests to cover your code changes.
5.  **Documentation**: If your pull request introduces a new feature, please update the usage examples in `README.md`.
6.  **Style**: Follow standard Go idioms and run `go fmt ./...`.

## License

By contributing, you agree that your contributions will be licensed under the MIT License properly.
