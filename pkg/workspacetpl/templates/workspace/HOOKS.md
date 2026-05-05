# Hook Instructions

Use this file to describe what should be captured at key runtime events.
Write short, plain-language bullets so non-technical users can maintain it.

## before_turn

- Capture project context needed for reproducibility (goal, dataset, branch, environment assumptions).

## after_turn

- Capture unresolved risks and durable follow-up needs.
- Record updates in plans, audit logs, reports, or output artifacts. Do not copy routine outcomes into `memory/MEMORY.md`.

## before_llm

- Note important constraints or safety boundaries for this request.

## after_llm

- Capture a concise response summary and confidence caveats.

## before_tool

- Record the intent of the tool call and key parameters (redacting secrets).

## after_tool

- Record tool outcomes in hook audit logs or task artifacts. Do not summarize successful tool calls into long-term memory.

## on_error

- Capture failure context, likely cause, and the next recovery step.
