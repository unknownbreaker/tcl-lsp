---
paths:
  - "lua/**"
  - "tests/lua/**"
---

# Lua/Neovim Conventions

## Module Pattern

```lua
local M = {}

-- Private functions (local)
local function helper() end

-- Public functions
function M.public_function() end

return M
```

## Plugin Entry

- `init.lua`: `setup(opts)`, keymaps, folding, LSP start
- `download.lua`: fetch and SHA-256-verify the prebuilt server
- `build.lua`: source-build fallback
- `version.lua`: the pinned release tag, shared with the Vim client

## Testing with Plenary

- Test files end with `_spec.lua`
- Use `describe` and `it` blocks
- Mock vim APIs when needed
- Run via `make test-unit`

## Neovim API Usage

- Use `vim.api.nvim_*` for buffer/window operations
- Use `vim.notify` for user messages
- Use `vim.schedule` or `vim.defer_fn` for async operations
- Handle errors gracefully, never crash the editor

## Filetypes

Support both `.tcl` and `.rvt` (Rivet templates)
