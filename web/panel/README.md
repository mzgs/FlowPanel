# FlowPanel React Panel

This directory contains the admin panel source.

## Stack

- `Vite`
- `React`
- `TypeScript`
- `shadcn/ui`-style primitives
- `TanStack Router`
- `TanStack Query`
- `TanStack Table`

## Output

Production builds write static assets into `../dist` so the Go binary can embed and serve them.

## UI intent

The panel is an operator console:

- compact layout
- table-first screens
- strong status handling
- minimal decoration
- fast navigation between domains, jobs, and settings
