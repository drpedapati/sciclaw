---
name: beautiful-mermaid
description: Build publication-grade Mermaid diagrams with consistent styling and SVG-first export for manuscript assets.
---

# Beautiful Mermaid

Use this skill when authoring architecture, workflow, or methods diagrams.

## When to use

- "make this mermaid look professional"
- "add system/workflow diagram"
- "export mermaid to svg"

## Workflow

1. Keep diagram semantics simple: clear nodes, directional flow, minimal crossing edges.
2. Use consistent naming and grouping for methods sections.
3. Prefer SVG outputs for manuscript-quality rendering.
4. Validate diagram text compiles in Quarto render.

## Quarto snippet

````markdown
```{mermaid}
flowchart LR
  A[Question] --> B[Plan]
  B --> C[Execution]
  C --> D[Evidence + Logs]
  D --> E[Manuscript Update]
```
````
