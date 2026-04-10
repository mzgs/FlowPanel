# Repository Instructions

- Do not add unit tests unless explicitly requested by the user.

## Implementation style
- Prefer simple, compact code when it does not reduce readability.
- Avoid repeating the same operation across multiple branches; derive the varying value first, then perform the shared operation once.
- Refactor obvious duplication during implementation, not as a separate cleanup step.
- Keep control flow tight: fewer branches, fewer repeated side effects, fewer repeated return paths when behavior can be expressed clearly in one place.
- Choose the implementation with the lowest unnecessary code volume that still makes the intent obvious.
- Do not expand code for explicitness if the same clarity can be achieved with a smaller, well-structured form.


## UI Change Rules

- For any UI bug or behavior change, trace the full user-visible flow before editing.
- Inspect all states that the UI can enter, including:
  initial render, loading, empty, success, error, disabled, closing, reopened, and async-updated states.
- Do not patch only the first matching render/update path. Check every code path that can change the same UI element, component, window, panel, modal, popup, tooltip, toast, or layout region.
- For async UI flows, verify whether the UI is created once and updated multiple times, or recreated across stages.
- When fixing layout or sizing issues, check the first visible render and every later update that can affect size, position, visibility, or content.
- When fixing close/cancel behavior, ensure in-flight async work cannot reopen, overwrite, or re-update dismissed UI.
- If a UI element appears to change “by itself,” identify the exact state transitions and all callsites that can mutate it before making the fix.
- After any UI change, verify the full interaction flow end-to-end, not only the specific line changed.
- In the final response, mention which UI states or transitions were verified.
