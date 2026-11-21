-- Tests for the initializationOptions the client sends: only options away from
-- the server's defaults, so an empty table never reaches a []string field.

local tcl = require("tcl-lsp")

local function opts(o)
  return vim.tbl_deep_extend("force", { extra_index_paths = {}, diagnostics = true, checks = {} }, o or {})
end

describe("tcl-lsp init options", function()
  it("sends nothing when every option is at its default", function()
    assert.is_nil(tcl._init_options(opts()))
  end)

  it("sends extraIndexPaths when paths are configured", function()
    assert.are.same({ extraIndexPaths = { "/a" } }, tcl._init_options(opts({ extra_index_paths = { "/a" } })))
  end)

  it("sends diagnostics = false when diagnostics are turned off", function()
    assert.are.same({ diagnostics = false }, tcl._init_options(opts({ diagnostics = false })))
  end)

  it("treats diagnostics = 0 as off", function()
    assert.are.same({ diagnostics = false }, tcl._init_options(opts({ diagnostics = 0 })))
  end)

  it("sends both together", function()
    assert.are.same(
      { extraIndexPaths = { "/a" }, diagnostics = false },
      tcl._init_options(opts({ extra_index_paths = { "/a" }, diagnostics = false }))
    )
  end)

  it("sends the Checks turned off, as false, and nothing for those left on", function()
    assert.are.same(
      { checks = { ["self-assignment"] = false, ["unreachable-code"] = false } },
      tcl._init_options(opts({
        checks = { ["self-assignment"] = false, ["unreachable-code"] = 0, ["self-comparison"] = true },
      }))
    )
  end)

  it("sends nothing when every Check is left on", function()
    assert.is_nil(tcl._init_options(opts({ checks = { ["self-assignment"] = true, ["constant-query"] = 1 } })))
  end)

  it("treats only false and 0 as off", function()
    assert.is_nil(tcl._init_options(opts({ checks = { ["self-assignment"] = "false" } })))
  end)

  it("sends checks beside the other options", function()
    assert.are.same(
      { extraIndexPaths = { "/a" }, diagnostics = false, checks = { ["self-assignment"] = false } },
      tcl._init_options(opts({
        extra_index_paths = { "/a" },
        diagnostics = false,
        checks = { ["self-assignment"] = false },
      }))
    )
  end)
end)

-- The classic-Vim client builds the same options from its globals.
describe("tcl_lsp#init_options (vimscript)", function()
  local globals = { "tcl_lsp_extra_index_paths", "tcl_lsp_diagnostics", "tcl_lsp_checks" }
  local function init_options(g)
    for _, name in ipairs(globals) do
      vim.g[name] = nil
    end
    for name, v in pairs(g or {}) do
      vim.g[name] = v
    end
    local ok, got = pcall(vim.fn["tcl_lsp#init_options"])
    for _, name in ipairs(globals) do
      vim.g[name] = nil
    end
    assert(ok, got)
    return got
  end

  it("sends nothing when no global is set", function()
    assert.is_true(vim.tbl_isempty(init_options()))
  end)

  it("sends extraIndexPaths and the diagnostics switch", function()
    assert.are.same(
      { extraIndexPaths = { "/a" }, diagnostics = false },
      init_options({ tcl_lsp_extra_index_paths = { "/a" }, tcl_lsp_diagnostics = 0 })
    )
  end)

  it("sends the Checks g:tcl_lsp_checks turns off, as false", function()
    assert.are.same(
      { checks = { ["self-assignment"] = false, ["unreachable-code"] = false } },
      init_options({
        tcl_lsp_checks = { ["self-assignment"] = 0, ["unreachable-code"] = false, ["self-comparison"] = 1 },
      })
    )
  end)

  it("sends nothing for Checks left on, or set to a non-boolean", function()
    assert.is_true(vim.tbl_isempty(init_options({
      tcl_lsp_checks = { ["self-assignment"] = 1, ["constant-query"] = true, ["self-comparison"] = "0" },
    })))
  end)

  it("ignores a g:tcl_lsp_checks that is not a dict", function()
    assert.is_true(vim.tbl_isempty(init_options({ tcl_lsp_checks = "unreachable-code" })))
    assert.is_true(vim.tbl_isempty(init_options({ tcl_lsp_checks = { "unreachable-code" } })))
  end)

  it("still reads g:tcl_lsp_diagnostics as Vim does, so '0' is off", function()
    assert.are.same({ diagnostics = false }, init_options({ tcl_lsp_diagnostics = "0" }))
    assert.are.same({ diagnostics = false }, init_options({ tcl_lsp_diagnostics = false }))
    assert.is_true(vim.tbl_isempty(init_options({ tcl_lsp_diagnostics = 1 })))
  end)
end)
