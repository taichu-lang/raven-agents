- 如果是用户选择 skill，在用户的输入中通过 `@<skill>` 的方式引入. 同时会把 skill 的完整内容, 添加到 user prompt 的最前面. skill 内容的模板如下:

  ```xml
  <skill_content name="<name>">
  # Skill: <name>

  <skill.md 的完整内容>

  Base directory for this skill: <skill_installation>
  Relative paths in this skill (e.g., scripts/, reference/) are relative to this base directory.
  Note: file list is sampled.

  <skill_files>
  </skill_files>

  </skill_content>
  ```

  > - `skill_files` 是会列举 skill 目录下的文件的全路径, 最多 10 个.
  > - 无论用户输入时, 引用 skill 的位置是在哪里, 都是把上述模板内容追加在最前面. 比如 `<模板内容> what does @hello-world do?`.

- skill tool 的定义:

  ```json
  {
    "type": "function",
    "function": {
      "name": "skill",
      "description": "Load a specialized skill's instructions and resources into the current conversation when the task at hand matches its description.\n\nThe skill ID must match an available skill or a skill explicitly referenced by the user.",
      "parameters": {
        "type": "object",
        "properties": {
          "id": {
            "type": "string",
            "description": "The ID of an available skill or a skill explicitly referenced by the user"
          }
        },
        "required": ["id"],
        "additionalProperties": false
      },
      "strict": false
    }
  }
  ```

- 在 system prompt 中, 会以如下方式列举出可用的 skills:

  ```plain
  Skills provide specialized instructions and workflows for specific tasks.
  Use the skill tool to load a skill when a task matches its description.
  The user may also invoke a skill directly. When that happens, its instructions appear in the conversation as a <skill_content> block, the same shape the skill tool returns. A skill that is already present this way does not need to be invoked again.
  <available_skills>
    <skill>
      <id>find-skills</id>
      <name>find-skills</name>
      <description>Helps users discover and install agent skills when they ask questions like \"how do I do X\", \"find a skill for X\", \"is there a skill that can...\", or express interest in extending capabilities. This skill should be used when the user is looking for functionality that might exist as an installable skill.</description>
    </skill>
  </available_skills>
  ```

  > - 上述的内容是直接追加到 system prompt 中的; 甚至还有 tool schema 的定义. 同时 `tools` 中会给出所有的 tool call 定义.
