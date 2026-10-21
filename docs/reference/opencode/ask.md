- 模型会回复一次 function call 的响应. 如:

  ```json
  {
    "role": "user",
    "content": "i want to know what happened last month"
  },
  {
    "role": "assistant",
    "content": null,
    "tool_calls": [
        {
            "id": "call_00_vy3n153zkqks69kqa3eqp58f",
            "type": "function",
            "function": {
                "name": "read",
                "arguments": "{\"path\":\"/home/adonia/03_doc/Default Project\"}"
            }
        },
        {
            "id": "call_01_bosnjxw27u3j8gb4tiycakjq",
            "type": "function",
            "function": {
                "name": "question",
                "arguments": "{\"questions\":[{\"question\":\"\\\"Last month\\\" could mean a few different things — what should I dig into?\",\"header\":\"What to research\",\"options\":[{\"label\":\"News in general\",\"description\":\"A roundup of major events from last month (Sep 14 – Oct 14, 2026)\"},{\"label\":\"A specific topic\",\"description\":\"Tell me the topic and I'll research what happened with it over the last 30 days\"},{\"label\":\"This project's activity\",\"description\":\"Look at what changed in files here recently — though this folder isn't a git repo\"}]}]}"
            }
        }
    ],
    "reasoning_content": "The user says \"i want to know what happened last month\". This is vague. But there's a skill \"last30days\" that researches what people actually say about any topic in the last 30 days. Hmm, \"what happened last month\" — ambiguous topic. Let me ask clarifying questions.\n\nActually the skill is about a topic. The user hasn't given a topic. \"what happened last month\" could mean news in general, or something about their project. Given the working directory is \"Default Project\", maybe they mean their project's git history? But it's not a git repo.\n\nI should ask for clarification. But maybe I should first check the environment for anything relevant. It's not a git repo. Let me explore the directory quickly and also ask.\n\nLet me do a quick read of the directory to see context, then ask a clarifying question."
  }
  ```

- question tool 的定义:

  ```json
  {
    "type": "function",
    "function": {
      "name": "question",
      "description": "Use this tool when you need to ask the user questions during execution. This allows you to:\n1. Gather user preferences or requirements\n2. Clarify ambiguous instructions\n3. Get decisions on implementation choices as you work\n4. Offer choices to the user about what direction to take.\n\nUsage notes:\n- A \"Type your own answer\" option is added automatically; don't include a separate option for free form answers\n- Set `multiple: true` to allow selecting more than one option\n- If you recommend a specific option, make that the first option in the list and add \"(Recommended)\" at the end of the label",
      "parameters": {
        "type": "object",
        "properties": {
          "questions": {
            "type": "array",
            "items": {
              "type": "object",
              "properties": {
                "question": {
                  "type": "string",
                  "description": "Complete question"
                },
                "header": {
                  "type": "string",
                  "description": "Very short label (max 30 chars)"
                },
                "options": {
                  "type": "array",
                  "items": {
                    "type": "object",
                    "properties": {
                      "label": {
                        "type": "string",
                        "description": "Display text (1-5 words, concise)"
                      },
                      "description": {
                        "type": "string",
                        "description": "Explanation of choice"
                      }
                    },
                    "required": ["label", "description"],
                    "additionalProperties": false
                  },
                  "description": "Available choices"
                },
                "multiple": {
                  "type": "boolean"
                }
              },
              "required": ["question", "header", "options"],
              "additionalProperties": false
            },
            "minItems": 1,
            "description": "Questions to ask"
          }
        },
        "required": ["questions"],
        "additionalProperties": false
      },
      "strict": false
    }
  }
  ```
