-- tcl-lsp client spec for Neovim 0.11+ / lazy.nvim.
--
-- Install: copy this file to ~/.config/nvim/lua/plugins/tcl-lsp.lua and restart.
-- lazy.nvim clones the repo, then (because of `opts`) calls
-- require("tcl-lsp").setup(opts). All the real work -- fetching the server
-- binary and wiring it into Neovim's native LSP -- lives in the plugin's
-- lua/tcl-lsp module, so this spec stays tiny.
--
-- No toolchain needed: on first load the plugin downloads the prebuilt server
-- binary for your OS/arch from a GitHub Release (needs curl or wget), verifies
-- its SHA-256, and caches it under stdpath("cache")/tcl-lsp/. Every launch after
-- is instant. `go` + `make` are only a fallback (unsupported platform, offline,
-- or local development). After a plugin update that bumps the pinned version,
-- :LspRestart swaps in the freshly downloaded server.

return {
  {
    -- MODE A (default): lazy.nvim manages the clone. No `build` directive -- the
    -- server binary is downloaded on first load, not compiled here.
    "unknownbreaker/tcl-lsp",

    -- Load only when you open a TCL/RVT buffer (the idiomatic lazy pattern for a
    -- filetype-scoped LSP). vim.lsp.enable doesn't spawn the server until a
    -- buffer attaches, so deferring the plugin's Lua is the only thing this saves
    -- -- but it keeps startup clean and signals intent.
    ft = { "tcl", "rvt" },

    -- `init` runs at startup (even though the plugin is lazy), so the .rvt
    -- extension is mapped before any file opens -- otherwise the `ft` trigger
    -- above could never fire for a .rvt buffer. (.tcl is a built-in filetype;
    -- we set it too for self-containment.)
    init = function()
      vim.filetype.add({ extension = { tcl = "tcl", rvt = "rvt" } })
    end,

    -- Defaults shown; everything is optional. Uncomment a line to change it.
    -- `opts` makes lazy.nvim call require("tcl-lsp").setup(opts) on load.
    opts = {
      -- filetypes    = { "tcl", "rvt" },           -- buffers the server attaches to
      -- root_markers = { ".git", "pkgIndex.tcl" },  -- project root first (order = priority);
      --                                             -- .git must win so one server indexes the
      --                                             -- whole repo (.tcl defs + .rvt call sites).
      -- cmd          = nil,   -- path to a server binary to use instead of the
      --                       -- downloaded one (string or list); nil = download
      -- auto_build   = true,  -- fall back to a source build (needs go+make) if
      --                       -- the prebuilt download is unavailable

      -- Keymaps, set buffer-local when the server attaches (only in tcl/rvt
      -- buffers; never clobbers your other maps). Default: none.
      -- keymaps = {
      --   definition      = "gd",
      --   references      = "grr",
      --   document_symbol = "gO",
      --   incoming_calls  = "<leader>ci",
      --   outgoing_calls  = "<leader>co",
      --   -- also: declaration, type_definition, workspace_symbol, hover
      -- },
      -- keys = {  -- lazy.nvim-style escape hatch for arbitrary maps
      --   -- { "<leader>cx", function() ... end, desc = "...", mode = "n" },
      -- },

      -- folding = true,  -- enable LSP code folding for tcl/rvt (window-local,
      --                  -- new splits included; other filetypes untouched)

      -- extra_index_paths = { "/opt/fa/tcl-lib" },  -- external package sources,
      --                  -- statically indexed at startup (goto-def reaches in)
    },
  },

  -- MODE B (developing the LSP itself): point at your working clone AND set `cmd`
  -- to your locally-built binary, so your edits -- not the released download --
  -- drive the server. Build it first with `make -C server build` (or `make watch`
  -- in server/ for a rebuild-on-save loop), then :LspRestart to reload after a
  -- rebuild. Comment out Mode A above and uncomment this.
  --
  -- {
  --   dir = vim.fn.expand("~/Repos/tcl-lsp"), -- your clone (repo ROOT)
  --   name = "tcl-lsp",
  --   ft = { "tcl", "rvt" },
  --   init = function()
  --     vim.filetype.add({ extension = { tcl = "tcl", rvt = "rvt" } })
  --   end,
  --   opts = { cmd = vim.fn.expand("~/Repos/tcl-lsp/server/tcl-lsp") }, -- local build
  -- },
}
