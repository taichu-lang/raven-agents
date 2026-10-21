# exec_command

```json
{
  "type": "function",
  "name": "exec_command",
  "description": "Runs a command in a PTY, returning output or a session ID for ongoing interaction.",
  "strict": false,
  "parameters": {
    "type": "object",
    "properties": {
      "cmd": {
        "type": "string",
        "description": "Shell command to execute."
      },
      "justification": {
        "type": "string",
        "description": "User-facing approval question for `require_escalated`; omit otherwise."
      },
      "login": {
        "type": "boolean",
        "description": "True runs the shell with -l/-i semantics; false disables them. Defaults to true."
      },
      "max_output_tokens": {
        "type": "number",
        "description": "Output token budget. Defaults to 10000 tokens; larger requests may be capped by policy."
      },
      "prefix_rule": {
        "type": "array",
        "description": "Reusable approval prefix for `cmd`, only with `sandbox_permissions: \"require_escalated\"`; for example [\"git\", \"pull\"].",
        "items": {
          "type": "string"
        }
      },
      "sandbox_permissions": {
        "type": "string",
        "description": "Per-command sandbox override. Defaults to `use_default`; use `require_escalated` for unsandboxed execution.",
        "enum": ["use_default", "require_escalated"]
      },
      "shell": {
        "type": "string",
        "description": "Shell binary to launch. Defaults to the user's default shell."
      },
      "tty": {
        "type": "boolean",
        "description": "True allocates a PTY for the command; false or omitted uses plain pipes."
      },
      "workdir": {
        "type": "string",
        "description": "Working directory for the command. Defaults to the turn cwd."
      },
      "yield_time_ms": {
        "type": "number",
        "description": "Wait before yielding output. Defaults to 10000 ms; effective range is 250-30000 ms."
      }
    },
    "required": ["cmd"],
    "additionalProperties": false
  }
}
```

# list_mcp_resources

```json
{
  "type": "function",
  "name": "list_mcp_resources",
  "description": "Lists resources provided by MCP servers. Resources allow servers to share data that provides context to language models, such as files, database schemas, or application-specific information. Prefer resources over web search when possible.",
  "strict": false,
  "parameters": {
    "type": "object",
    "properties": {
      "cursor": {
        "type": "string",
        "description": "Opaque cursor from a previous list_mcp_resources call; omit for the first page."
      },
      "server": {
        "type": "string",
        "description": "MCP server name. Omit to list resources from every configured server."
      }
    },
    "additionalProperties": false
  }
}
```

# list_mcp_resource_templates

```json
{
  "type": "function",
  "name": "list_mcp_resource_templates",
  "description": "Lists resource templates provided by MCP servers. Parameterized resource templates allow servers to share data that takes parameters and provides context to language models, such as files, database schemas, or application-specific information. Prefer resource templates over web search when possible.",
  "strict": false,
  "parameters": {
    "type": "object",
    "properties": {
      "cursor": {
        "type": "string",
        "description": "Opaque cursor from a previous list_mcp_resource_templates call; omit for the first page."
      },
      "server": {
        "type": "string",
        "description": "MCP server name. Omit to list resource templates from every configured server."
      }
    },
    "additionalProperties": false
  }
}
```

# read_mcp_resource

```json
{
  "type": "function",
  "name": "read_mcp_resource",
  "description": "Read a specific resource from an MCP server given the server name and resource URI.",
  "strict": false,
  "parameters": {
    "type": "object",
    "properties": {
      "server": {
        "type": "string",
        "description": "MCP server name exactly as configured. Must match the 'server' field returned by list_mcp_resources."
      },
      "uri": {
        "type": "string",
        "description": "Resource URI to read. Must be one of the URIs returned by list_mcp_resources."
      }
    },
    "required": ["server", "uri"],
    "additionalProperties": false
  }
}
```

# request_user_input

```json
{
  "type": "function",
  "name": "request_user_input",
  "description": "Request user input for one to three short questions and wait for the response. This tool is only available in Plan mode.",
  "strict": false,
  "parameters": {
    "type": "object",
    "properties": {
      "questions": {
        "type": "array",
        "description": "Questions to show the user. Prefer 1 and do not exceed 3",
        "items": {
          "type": "object",
          "properties": {
            "header": {
              "type": "string",
              "description": "Short header label shown in the UI (12 or fewer chars)."
            },
            "id": {
              "type": "string",
              "description": "Stable identifier for mapping answers (snake_case)."
            },
            "options": {
              "type": "array",
              "description": "Provide 2-3 mutually exclusive choices. Put the recommended option first and suffix its label with \"(Recommended)\". Do not include an \"Other\" option in this list; the client will add a free-form \"Other\" option automatically.",
              "items": {
                "type": "object",
                "properties": {
                  "description": {
                    "type": "string",
                    "description": "One short sentence explaining impact/tradeoff if selected."
                  },
                  "label": {
                    "type": "string",
                    "description": "User-facing label (1-5 words)."
                  }
                },
                "required": ["label", "description"],
                "additionalProperties": false
              }
            },
            "question": {
              "type": "string",
              "description": "Single-sentence prompt shown to the user."
            }
          },
          "required": ["id", "header", "question", "options"],
          "additionalProperties": false
        }
      }
    },
    "required": ["questions"],
    "additionalProperties": false
  }
}
```

# request_permissions

```json
{
  "type": "function",
  "name": "request_permissions",
  "description": "Request additional filesystem or network permissions from the user and wait for the client to grant a subset of the requested permission profile. Use environment_id to target a specific attached environment; omit it to use the primary environment. Relative filesystem paths resolve against the selected environment cwd. Granted permissions apply automatically to later shell-like commands in the current turn, or for the rest of the session if the client approves them at session scope.",
  "strict": false,
  "parameters": {
    "type": "object",
    "properties": {
      "environment_id": {
        "type": "string",
        "description": "Environment id from <environment_context>. Omit to use the primary environment."
      },
      "permissions": {
        "type": "object",
        "description": "Filesystem or network access request.",
        "properties": {
          "file_system": {
            "type": "object",
            "description": "Filesystem access request.",
            "properties": {
              "read": {
                "type": "array",
                "description": "Absolute paths to grant read access; omit when none are needed.",
                "items": {
                  "type": "string"
                }
              },
              "write": {
                "type": "array",
                "description": "Absolute paths to grant write access; omit when none are needed.",
                "items": {
                  "type": "string"
                }
              }
            },
            "additionalProperties": false
          },
          "network": {
            "type": "object",
            "description": "Network access request.",
            "properties": {
              "enabled": {
                "type": "boolean",
                "description": "True requests network access; false or omitted requests none."
              }
            },
            "additionalProperties": false
          }
        },
        "additionalProperties": false
      },
      "reason": {
        "type": "string",
        "description": "Optional short explanation for why additional permissions are needed."
      }
    },
    "required": ["permissions"],
    "additionalProperties": false
  }
}
```

# tool_search

```json
{
  "type": "tool_search",
  "execution": "client",
  "description": "# Tool discovery\n\nSearches over deferred tool metadata with BM25 and exposes matching tools for the next model call.\n\nYou have access to tools from the following sources:\n- Multi-agent tools: Spawn and manage sub-agents.\n- node_repl: Use `js` for `node_repl` execution with persistent, redeclarable top-level bindings, `js_reset` to clear bindings, and `js_add_node_module_dir` to add package directories.\n- openai_artifact_template_picker\nSome of the tools may not have been provided to you upfront, and you should use this tool (`tool_search`) to search for the required tools. For MCP tool discovery, always use `tool_search` instead of `list_mcp_resources` or `list_mcp_resource_templates`.",
  "parameters": {
    "type": "object",
    "properties": {
      "limit": {
        "type": "number",
        "description": "Maximum number of tools to return. Defaults to 8."
      },
      "query": {
        "type": "string",
        "description": "Search query for deferred tools."
      }
    },
    "required": ["query"],
    "additionalProperties": false
  }
}
```

# web_search

```json
{
  "type": "web_search",
  "external_web_access": false,
  "search_content_types": ["text", "image"]
}
```
