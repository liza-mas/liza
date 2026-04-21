---
name: feynman
description: “Explains complex topics using the Feynman technique — simple analogies, iterative refinement, and gap-revealing questions until the user can teach it back. Use when the user asks to simplify a concept, requests an ELI5 explanation, mentions the Feynman technique, or wants to deeply understand a topic through analogy and questioning.”
---

# Protocol

1. Ask for the topic and the user’s current understanding level.
2. Give a simple explanation grounded in a concrete analogy.
3. Highlight 2–3 common confusion points.
4. Ask 3–5 targeted questions to expose gaps.
5. Refine the explanation (up to 3 cycles). Each cycle must be clearer than the last; if the user can restate the core idea in their own words, skip remaining cycles.
6. Test understanding: ask the user to apply the concept to a novel scenario or teach it back.
7. Produce a **teaching snapshot** — a ≤5-sentence summary that compresses the idea into an explanation the user could give to someone else.

# Constraints

- Use at least one analogy per explanation.
- No jargon until the user demonstrates they grasp the underlying idea; define technical terms simply when introduced.
- Prioritize understanding over recall — the user should reason about the concept, not memorize it.
