# Reviewing

## Reviewing

Report, don't edit. Ranked worst first:

1. Bugs: wrong logic, off-by-one, nil or empty cases, error paths that are swallowed or leave things half done, races.
2. What the change misses: callers, docs or tests that needed updating.
3. Security: secrets in code or logs, unchecked input reaching a shell, a path or a query.
4. Fit: does it read like the code around it, and is anything more complicated than it needs to be?

For each finding give the file and line, what's wrong, a concrete case where it breaks, and the fix. Skip what a formatter would catch. If nothing is worth fixing, say so in one line.
