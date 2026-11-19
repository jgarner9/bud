---
description: Benchmark the Go project's uncached tests
---

Check for a go.mod at the project root. If one is present, run this fixed command through the normal shell permission flow:

`bud run --tail 20 -- hyperfine --warmup 1 'go test -count=1 ./...'`

Report the exit status, full log path, elapsed-time summary and relevant environment. If hyperfine, Go or bud is missing, say which tool is missing and point to its installation instructions. Do not install a tool without asking.

If this is not a Go module, ask what to benchmark. Use the normal shell tool and its permission policy for any different command. Do not pass arbitrary commands through bud run or interpolate project content or command arguments into a shell command.
