# Repository Instructions

- Do not add unit tests unless explicitly requested by the user.

## Implementation style
- Prefer simple, compact code when it does not reduce readability.
- Avoid repeating the same operation across multiple branches; derive the varying value first, then perform the shared operation once.
- Refactor obvious duplication during implementation, not as a separate cleanup step.
- Keep control flow tight: fewer branches, fewer repeated side effects, fewer repeated return paths when behavior can be expressed clearly in one place.
- Choose the implementation with the lowest unnecessary code volume that still makes the intent obvious.
- Do not expand code for explicitness if the same clarity can be achieved with a smaller, well-structured form.
