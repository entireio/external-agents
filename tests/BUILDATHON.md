# Safe Change Agent

## One-sentence summary

**Safe Change Agent is a multi-agent coding assistant that uses Entire checkpoints and code-structure evidence to help developers understand, implement, validate, and remember safer changes to an existing codebase.**

---

## Problem, intended user and why it matters

### Problem

When developers modify an existing codebase, understanding the impact of a change can be difficult.

A seemingly small request such as:

- "Add a new API field"
- "Change the validation rule"
- "Fix this calculation"
- "Rename this function"

can affect multiple files, functions, tests, APIs, or downstream components.

Traditional AI coding assistants can generate or modify code, but they may not have enough context about:

- Why the existing code was designed that way
- What other parts of the system depend on it
- What tests already protect the behavior
- What previous developers changed
- Why a previous change was made
- Whether the new change was actually validated

This can lead to incorrect changes, broken functionality, and repeated investigation work.

### Intended user

The primary user is a **software developer working on an existing codebase**, especially when the developer needs to modify functionality without unintentionally breaking existing behavior.

The system is designed for situations where another developer may later need to understand the same code and the reasoning behind previous changes.

### Why it matters

The goal is not simply to make AI-generated code.

The goal is to make AI-assisted software changes **more explainable, traceable, and safer**.

Safe Change Agent connects:

**Task → Code understanding → Change → Validation → Engineering memory**

Instead of remembering only the final code, the system preserves evidence about:

- What changed
- Why it changed
- What code was affected
- What tests were executed
- What decisions were made
- What the developer should know when continuing the work later

---

## Selected Entire track and why Entire is essential

### Selected track

**Entire Track: Engineering Memory / AI-assisted software development**

### Why Entire is essential

Git tells us what changed in a repository.

However, a code diff alone does not always explain:

- Why the change was made
- What problem the developer was solving
- What alternatives were considered
- What tests were performed
- What the developer discovered during investigation
- What decisions were made before implementing the change

Safe Change Agent uses **Entire checkpoints** as an engineering memory layer.

The workflow records meaningful development states throughout the task instead of treating the final Git commit as the only source of truth.

This allows a future developer or AI agent to reconstruct the development context.

The key idea is:

> **Git remembers the code. Entire helps remember the engineering context around the code.**

This makes Entire an important part of the system rather than simply an additional integration.

---

## Architecture and main workflow

### High-level architecture

```text
                    ┌──────────────────────┐
                    │      Developer       │
                    │   Gives a task       │
                    └──────────┬───────────┘
                               │
                               ▼
                    ┌──────────────────────┐
                    │    Analyst Agent     │
                    │                      │
                    │ Understands task     │
                    │ Inspects code        │
                    │ Uses Entire Graph    │
                    │ Identifies impact    │
                    └──────────┬───────────┘
                               │
                               ▼
                    ┌──────────────────────┐
                    │  Implementation      │
                    │      Plan            │
                    └──────────┬───────────┘
                               │
                               ▼
                    ┌──────────────────────┐
                    │  Implementer Agent   │
                    │                      │
                    │ Modifies code        │
                    │ Creates/updates tests│
                    └──────────┬───────────┘
                               │
                               ▼
                    ┌──────────────────────┐
                    │   Validator Agent    │
                    │                      │
                    │ Runs tests           │
                    │ Checks failures      │
                    │ Verifies behavior    │
                    └──────────┬───────────┘
                               │
                               ▼
                    ┌──────────────────────┐
                    │  Entire Checkpoint   │
                    │                      │
                    │ What changed         │
                    │ Why                  │
                    │ Evidence             │
                    │ Tests                │
                    │ Decisions            │
                    └──────────┬───────────┘
                               │
                               ▼
                    ┌──────────────────────┐
                    │ Engineering Memory   │
                    │ for future developers│
                    └──────────────────────┘