---
name: humanize-text
description: Rewrite AI-generated text to sound naturally human-written. Use when asked to humanize text, make writing sound less AI, bypass AI detection, rewrite for natural tone, or convert robotic/formal text to conversational style. Triggers on "humanize this", "make this sound human", "rewrite naturally", "anti-AI detection", or any request to remove AI tells from writing.
---

# Humanize Text

Rewrite AI-generated text so it reads like a human wrote it. Kill patterns, inject personality, vary rhythm.

## AI Detection Triggers to Eliminate

- Perfect grammar (humans make small mistakes)
- Repetitive sentence structure (AI loves patterns)
- Corporate buzzwords: "leverage," "delve," "landscape," "robust," "streamline," "ensure"
- Overuse of transitions: "moreover," "furthermore," "however"
- Em dashes everywhere (AI's favorite punctuation, a dead giveaway)
- Even pacing (humans speed up and slow down)
- No contractions (we use them constantly)
- Safe, sanitized language (humans have opinions)

## Rewrite Rules

### 1. Vary Rhythm
- Mix short punchy sentences with longer flowing ones
- Some incomplete thoughts. Because that's real.
- Occasional natural run-ons that feel like conversation

### 2. Add Imperfection
- Start sentences with "And" or "But"
- Casual connectors: "Look," "Here's the thing," "Honestly"
- Drop a comma here and there

### 3. Inject Personality
- Specific examples over generic ones
- Personal observations: "I've noticed," "In my experience"
- Mild opinions: "which is insane," "surprisingly effective"
- Rhetorical questions

### 4. Kill AI Phrases

Replace instantly:

| AI Word | Human Replacement |
|---------|------------------|
| Delve | dig into, explore |
| Landscape | space, world |
| Leverage | use |
| Robust | strong, solid |
| Streamline | simplify |
| Moreover | Plus, Also, or drop it |
| Ensure | make sure |
| Utilize | use |
| Facilitate | help, enable |
| Comprehensive | thorough, full |
| Furthermore | And, Plus |
| In order to | To |
| It is important to note | (delete) |
| At the end of the day | (delete or rephrase) |
| Em dash (—) | comma, period, semicolon, or parentheses |

### 5. Kill Em Dashes

Em dashes are one of the strongest AI tells. Never use them. Replace every `—` with:
- A comma (most common fix)
- A period and new sentence
- A semicolon
- Parentheses
- Restructure the sentence entirely

**Before:** "The results were clear — the treatment worked."
**After:** "The results were clear. The treatment worked." or "The results were clear, and the treatment worked."

### 6. Natural Flow
- Humans digress slightly (brief tangents)
- Emphasis with italics, bold, or colons
- Parentheticals (because we think while writing)
- Contractions everywhere (don't, won't, I'm, they're)

## Process

When given text to humanize:

**Step 1: Rewrite**
- Vary sentence length wildly
- Replace 80% of transitions with casual ones
- Add 2-3 personal touches ("I think," "honestly," "look")
- Include 1-2 incomplete sentences or fragments
- Swap formal words for conversational ones
- Add emphasis (italics, bold, colons, parentheses)
- **Eliminate every em dash** (replace with commas, periods, or restructure)

**Step 2: Read-aloud test**
- Would someone actually say this?
- Does it flow like conversation?
- Any word feel too "AI"?

**Step 3: Final pass**
- Remove remaining stiffness
- Check for contractions (don't, won't, I'm, they're)
- Kill repetitive structure
- Add one unexpected comparison or example

## Output Format

```
[Rewritten text]

**Changes made:**
- [3-5 key transformations]

**Detection risk:** [Low/Medium/High] — [brief explanation]
```

## Example

**Before (AI):**
"In order to achieve optimal results in content marketing, it is essential to leverage data-driven insights and ensure consistent engagement with your target audience across multiple platforms."

**After (Human):**
"Want better content marketing results? Use data to guide your decisions and actually engage with your audience. Consistently. Across whatever platforms they're on. Not rocket science, but most people skip the data part."

**Changes made:**
- Killed "in order to," "optimal," "leverage," "ensure"
- Added rhetorical question opening
- Split into short fragments for breathing room
- Added casual observation at end
- Used contractions

**Detection risk:** Low, reads like someone explaining over coffee.
