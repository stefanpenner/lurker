# Security Ideas

Lurker executes AI agents that modify code and interact with GitHub. This document captures security considerations and planned mitigations.

## Current Risks

### Untrusted Issue Authors

Anyone who can open an issue on a watched repo can influence what Claude does. A malicious issue could contain:

- Prompt injection in the issue body
- Instructions to exfiltrate secrets or modify unrelated code
- Social engineering to bypass Claude's safety measures

### Unrestricted Processing

Currently, processing is user-initiated (you press Space), but there's no validation of the issue content or author before Claude starts working.

### Claude's Capabilities

Claude has scoped but significant access:

- Read/Write/Edit files in the cloned workdir
- Run `bazel test/build` and `git` commands
- No network access beyond what those tools provide

## Planned Mitigations

### 1. Thumbs-Up Gating (Priority: High)

**The repo owner must thumbs-up react an issue before lurker will process it.**

- After discovering a new issue, check its reactions via `gh api`
- Only enable the "start" action if a maintainer has reacted with thumbs-up
- Re-check on each poll cycle
- The thumbs-up acts as a human-in-the-loop approval that the issue is legitimate and safe to process

This is the single most important security measure — it ensures a human reviews every issue before AI execution begins.

### 2. Author Allowlist (Priority: High)

Maintain a list of trusted issue authors per repo:

- Issues from trusted authors skip the thumbs-up requirement
- Issues from unknown authors require manual approval
- Configurable via `LURKER.toml` or per-repo config
- Default: empty (all issues require approval)

### 3. Label-Based Gating (Priority: Medium)

Require a specific label (e.g., `lurker:approved`) before processing:

- Only maintainers/collaborators can add labels
- Provides a clear audit trail in the GitHub UI
- Can combine with thumbs-up for defense in depth
- Label name configurable per repo

### 4. Prompt Injection Hardening (Priority: Medium)

- Wrap issue body in structured delimiters to help Claude distinguish instructions from user content
- Add a preamble warning Claude about potential prompt injection
- Sanitize or truncate extremely long issue bodies
- Consider a two-phase approach: Claude first analyzes the issue for safety, then implements

### 5. Sandboxed Execution (Priority: Medium)

- Run Claude in a container or VM (integration with Docker/Podman)
- Use a dedicated GitHub token with minimal permissions (no admin, no delete)
- Separate workdirs from any sensitive paths
- Use git worktrees to limit disk footprint
- Consider network isolation during Claude execution

### 6. Code Review Before Merge (Priority: High, Already Implemented)

Lurker never auto-merges. The workflow is:

1. Claude makes changes on a branch
2. Human reviews in the TUI, lazygit, or Claude interactive mode
3. Human explicitly approves and creates PR
4. Standard PR review process applies

This is already the default behavior and should never change.

### 7. Rate Limiting (Priority: Low)

- Limit concurrent Claude sessions (default: 1)
- Limit issues processed per repo per hour
- Exponential backoff on repeated failures
- Configurable limits per repo

### 8. Audit Logging (Priority: Low)

- Log all Claude actions to a structured audit log (JSONL)
- Record which issues triggered which code changes
- Include timestamps, costs, and tool usage
- Make it easy to review what lurker did and when

## Other Ideas

- **Dry-run mode** — Claude analyzes but doesn't write files, produces a plan only
- **Diff preview** — Show the full diff in the TUI before allowing PR creation
- **Cost caps** — Stop Claude if estimated cost exceeds a threshold
- **Token allowlist** — Only use specific GitHub tokens with scoped permissions
- **Webhook mode** — Instead of polling, receive webhook events (reduces API calls, enables faster response)
- **Issue templates** — Encourage structured issue formats that are harder to inject into

## Recommendation

Start with **thumbs-up gating** (#1) as the minimum viable security. It's simple to implement, effective, and provides a human checkpoint before any AI execution. Combine with **author allowlists** (#2) for trusted contributors.

The mantra: **Never let an AI process an issue that a maintainer hasn't explicitly approved.**
