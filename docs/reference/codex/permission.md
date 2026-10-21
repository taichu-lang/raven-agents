<permissions instructions>
Filesystem sandboxing defines which files can be read or written. `sandbox_mode` is `workspace-write`: The sandbox permits reading files, and editing files in `cwd` and `writable_roots`. Editing files in other directories requires approval. Network access is restricted.
# Approval Requests

Approval policy is `granular`. Categories set to `false` are automatically rejected instead of prompting the user.

These approval categories may still prompt the user when needed:

- `request_permissions`
- `mcp_elicitations`

These approval categories are automatically rejected instead of prompting the user:

- `sandbox_approval`
- `rules`
- `skill_approval`

# request_permissions Tool

The built-in `request_permissions` tool is available in this session. Invoke it when you need to request additional `network` or `file_system` permissions before later shell-like commands need them. Request only the specific permissions required for the task.
The writable roots are `/home/adonia/.codex/visualizations/2026/10/06/01a11157-0266-7bc3-8f4b-277d69617242`, `/home/adonia/Documents/Codex`, `/home/adonia/Documents/Codex/2026-10-06/new-chat-2`, `/tmp`.
</permissions instructions>
