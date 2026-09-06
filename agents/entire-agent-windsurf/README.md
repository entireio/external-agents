# entire-agent-windsurf

Preview Entire external-agent adapter for Windsurf Cascade.

This initial adapter implements the stable protocol core: discovery metadata,
detection, session identity, session read/write plumbing, and transcript byte
transport. It deliberately does not yet install Windsurf hooks or parse
Windsurf transcripts.

Build with `mise run build` or `go build -o entire-agent-windsurf ./cmd/entire-agent-windsurf`.
