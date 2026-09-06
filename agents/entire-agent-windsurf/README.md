# entire-agent-windsurf

Preview Entire external-agent adapter for Windsurf Cascade.

The adapter implements the stable protocol core plus Windsurf's workspace hook
integration. `entire enable --agent windsurf` installs managed commands into
`.windsurf/hooks.json` for `pre_user_prompt`, `post_write_code`,
`post_cascade_response`, and `post_cascade_response_with_transcript`,
preserving unrelated configuration.

`trajectory_id` is the stable session identity and `execution_id` is retained
as event metadata. The v1 Entire protocol has no code-write event type, so the
exported lifecycle boundary preserves `post_write_code` data for downstream
context extraction without ending a turn. Transcript parsing remains separate.

Build with `mise run build` or `go build -o entire-agent-windsurf ./cmd/entire-agent-windsurf`.
