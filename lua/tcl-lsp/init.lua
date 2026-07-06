-- tcl-lsp: Neovim client for the bundled TCL/RVT language server.
--
-- This module is the whole client. A plugin spec only needs to call
-- `require("tcl-lsp").setup(opts)` (lazy.nvim does that for you when you pass
-- `opts`). Everything else -- locating/building the bundled Go server, wiring
-- it into Neovim's native LSP, and a rebuild command -- lives here, so the
-- user-facing config stays tiny. See editors/nvim/tcl-lsp.lua for the spec.
--
-- Requires Neovim 0.11+ (native vim.lsp.config / vim.lsp.enable).

local M = {}

-- plugin_root resolves this file's install dir up to the repo root
-- (<root>/lua/tcl-lsp/init.lua -> <root>), so the bundled server binary and
-- Makefile are found wherever the plugin was cloned -- no hardcoded paths.
local function plugin_root()
  local src = debug.getinfo(1, "S").source:sub(2) -- strip leading "@"
  return vim.fn.fnamemodify(src, ":p:h:h:h")
end

-- Defaults. Every field is overridable via the `opts` table a user passes to
-- setup() (see editors/nvim/tcl-lsp.lua for documented examples).
local defaults = {
  filetypes = { "tcl", "rvt" }, -- buffers the server attaches to
  -- How the project root is detected. ORDER IS PRIORITY (Neovim 0.11+): the
  -- repo root (.git) MUST come first. The server indexes its root's whole
  -- subtree, and cross-file resolution (.tcl proc <-> .rvt call site) only works
  -- when both files share one server/index. TCL projects put a pkgIndex.tcl in
  -- every package dir; if that marker wins, each package spawns its own server
  -- rooted at a narrow subtree that excludes the .rvt pages, so find-references
  -- from a .tcl silently misses .rvt call sites (goto-def the other way still
  -- works, which is the confusing asymmetry). pkgIndex.tcl stays only as a
  -- fallback for non-git checkouts.
  root_markers = { ".git", "pkgIndex.tcl" },
  cmd = nil, -- override the server binary (string or list); nil = bundled binary
  auto_build = true, -- build the bundled Go server on first use if missing
  -- Buffer-local keymaps, set on LspAttach for tcl/rvt buffers only. Two forms,
  -- both optional (default: none, so existing keymaps are never clobbered):
  --   keymaps = { incoming_calls = "<leader>ci", definition = "gd", ... }
  --     a named LSP action -> the key that triggers it (the plugin owns the
  --     function). Set an action to false to leave it unbound.
  --   keys = { { "<leader>cx", fn, desc = "...", mode = "n" }, ... }
  --     a lazy.nvim-style escape hatch to bind arbitrary keys to any function.
  keymaps = {},
  keys = {},
  -- When true, the plugin enables LSP code folding (textDocument/foldingRange)
  -- for tcl/rvt buffers: it sets foldmethod=expr and foldexpr=vim.lsp.foldexpr()
  -- *window-local* on attach (and on every window that displays such a buffer,
  -- so new splits keep the folds). Other filetypes are untouched, so your global
  -- foldexpr (e.g. Treesitter) stays the default everywhere else. Default off;
  -- you still choose foldlevel/foldcolumn/etc. yourself.
  folding = false,
  -- Directories (or files) of EXTERNAL TCL sources -- company package checkouts,
  -- tcllib dirs -- statically indexed at startup in addition to the workspace,
  -- so goto-def/references/semantic tokens reach into them. Read-only: nothing
  -- is executed, no artifact is written, missing paths are skipped (safe to
  -- share one config across machines). Example:
  --   extra_index_paths = { "/opt/fa/tcl-lib", vim.fn.expand("~/Repos/fa-tcl") }
  extra_index_paths = {},
}

-- action_fns maps a `keymaps` action name to the vim.lsp.buf function it triggers.
local action_fns = {
  definition = vim.lsp.buf.definition,
  declaration = vim.lsp.buf.declaration,
  type_definition = vim.lsp.buf.type_definition,
  references = vim.lsp.buf.references,
  document_symbol = vim.lsp.buf.document_symbol,
  workspace_symbol = function()
    vim.lsp.buf.workspace_symbol()
  end,
  incoming_calls = vim.lsp.buf.incoming_calls,
  outgoing_calls = vim.lsp.buf.outgoing_calls,
  hover = vim.lsp.buf.hover,
}

-- _keymap_specs flattens the `keymaps` (named action -> lhs) and `keys`
-- (lazy-style specs) options into a list of { mode, lhs, rhs, desc } to set
-- buffer-local, plus the list of unknown action names to warn about. Pure (no
-- editor state); exposed on M for testing.
function M._keymap_specs(keymaps, keys)
  local specs, unknown = {}, {}
  for action, lhs in pairs(keymaps or {}) do
    local fn = action_fns[action]
    if not fn then
      table.insert(unknown, action)
    elseif lhs then
      table.insert(specs, { mode = "n", lhs = lhs, rhs = fn, desc = "tcl-lsp: " .. action })
    end
  end
  for _, spec in ipairs(keys or {}) do
    local lhs, rhs = spec[1], spec[2]
    if lhs and rhs then
      table.insert(specs, { mode = spec.mode or "n", lhs = lhs, rhs = rhs, desc = spec.desc })
    end
  end
  return specs, unknown
end

-- _fold_opts returns the window-local option values that route folding through
-- this server (vs. a global Treesitter/other foldexpr). Pure; exposed on M for
-- testing so the exact expr string is pinned against typos.
function M._fold_opts()
  return { foldmethod = "expr", foldexpr = "v:lua.vim.lsp.foldexpr()" }
end

-- apply_folding sets the LSP fold options window-local on every window currently
-- displaying buf. Per-window (not per-buffer) because foldexpr/foldmethod are
-- window options that do NOT follow a buffer into a new split -- so we (re)apply
-- to all of buf's windows whenever it attaches or is displayed.
local function apply_folding(buf)
  local o = M._fold_opts()
  for _, win in ipairs(vim.fn.win_findbuf(buf)) do
    vim.wo[win].foldmethod = o.foldmethod
    vim.wo[win].foldexpr = o.foldexpr
  end
end

function M.setup(opts)
  opts = vim.tbl_deep_extend("force", defaults, opts or {})
  local root = plugin_root()

  local cmd = opts.cmd
  if type(cmd) == "string" then
    cmd = { cmd }
  end
  if not cmd then
    -- Prefer a prebuilt release binary (no toolchain needed); fall back to a
    -- source build for developers / offline / unsupported platforms.
    local bin = require("tcl-lsp.download").resolve(root, opts)
    if not bin then
      return -- resolve already told the user what to do
    end
    cmd = { bin }
  end

  local config = {
    cmd = cmd,
    filetypes = opts.filetypes,
    root_markers = opts.root_markers,
  }
  -- Only send init_options when paths are configured: an empty Lua table
  -- JSON-encodes as an object ({}), which would not decode into the server's
  -- []string field.
  if #opts.extra_index_paths > 0 then
    config.init_options = { extraIndexPaths = opts.extra_index_paths }
  end
  vim.lsp.config("tcl_lsp", config)
  vim.lsp.enable("tcl_lsp")

  -- Apply user keymaps buffer-local when our server attaches, so they exist only
  -- in tcl/rvt buffers and never leak into other filetypes. Specs are static, so
  -- build them (and warn about typos) once here, then set them per buffer.
  local specs, unknown = M._keymap_specs(opts.keymaps, opts.keys)
  for _, action in ipairs(unknown) do
    vim.notify("tcl-lsp: unknown keymap action '" .. action .. "'", vim.log.levels.WARN)
  end
  if #specs > 0 then
    vim.api.nvim_create_autocmd("LspAttach", {
      group = vim.api.nvim_create_augroup("TclLspKeymaps", { clear = true }),
      callback = function(args)
        local client = vim.lsp.get_client_by_id(args.data.client_id)
        if not client or client.name ~= "tcl_lsp" then
          return -- only bind for our server, not other LSPs on the buffer
        end
        for _, s in ipairs(specs) do
          vim.keymap.set(s.mode, s.lhs, s.rhs, { buffer = args.buf, desc = s.desc })
        end
      end,
    })
  end

  -- Opt-in LSP folding for tcl/rvt buffers, window-local so it never disturbs
  -- other filetypes' folding. Two triggers cover the two ways a buffer reaches a
  -- window: LspAttach (first open, once our server connects) and BufWinEnter (any
  -- later display, e.g. a new split -- which doesn't inherit window-local opts).
  if opts.folding then
    local group = vim.api.nvim_create_augroup("TclLspFolding", { clear = true })
    vim.api.nvim_create_autocmd("LspAttach", {
      group = group,
      callback = function(args)
        local client = vim.lsp.get_client_by_id(args.data.client_id)
        if client and client.name == "tcl_lsp" then
          apply_folding(args.buf)
        end
      end,
    })
    vim.api.nvim_create_autocmd("BufWinEnter", {
      group = group,
      callback = function(args)
        if not vim.tbl_contains(opts.filetypes, vim.bo[args.buf].filetype) then
          return -- cheap early-out before the client lookup
        end
        -- Only once our server is attached; otherwise leave the user's global
        -- foldexpr in place and let LspAttach apply when it connects.
        if next(vim.lsp.get_clients({ bufnr = args.buf, name = "tcl_lsp" })) then
          apply_folding(args.buf)
        end
      end,
    })
  end

  -- Rebuild after pulling new server code, then :LspRestart to load it.
  vim.api.nvim_create_user_command("TclLspRebuild", function()
    vim.system({ "make", "-C", root .. "/server", "build" }, { text = true }, function(out)
      vim.schedule(function()
        if out.code == 0 then
          vim.notify("tcl-lsp: rebuilt — run :LspRestart to load it.", vim.log.levels.INFO)
        else
          vim.notify("tcl-lsp: rebuild failed:\n" .. (out.stderr or ""), vim.log.levels.ERROR)
        end
      end)
    end)
  end, { desc = "Rebuild the tcl-lsp server binary" })
end

return M
