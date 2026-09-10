# Claude Code Configuration

## Behavioral Rules (Always Enforced)

- Do what has been asked; nothing more, nothing less.
- NEVER create files unless they're absolutely necessary for achieving your goal.
- ALWAYS prefer editing an existing file to creating a new one.
- NEVER proactively create documentation files (\*.md) or README files unless explicitly requested.
- NEVER save working files, text/mds, or tests to the root folder.
- ALWAYS read a file before editing it.
- NEVER commit secrets, credentials, or .env files.
- NEVER write comments in Chinese in code or configuration files; always use English.

## Project Architecture

- Follow Domain-Driven Design with bounded contexts.
- Keep files under 500 lines.
- Use typed interfaces for all public APIs.

## File Organization

Dependency rule: `interfaces → application → domain ← infrastructure`. Infrastructure and interfaces never import each other.

## Code Style

- Using `slog` as the logger interface.
- Repository list functions must return an empty slice (not `nil`) when no rows are found; initialize with `make([]*T, 0)`.
- Naming: Avoid uncommon abbreviations; prioritize clarity and accurate understanding over brevity.
- Use number (the timestamp in seconds) as the type of database field if it's a datetime, rather than the builtin `timestamp with time zone`. DO NOT invoke database function to get the time, do it in the application layer.
- Each provider's log calls to include a `provider` tag, ex: `slog.With("provider", "openai")`.
- Naming: Use `ClientID` rather than `ClientId`.
