-- Tests for the initializationOptions the client sends: only options away from
-- the server's defaults, so an empty table never reaches a []string field.

local tcl = require("tcl-lsp")

local function opts(o)
  return vim.tbl_deep_extend("force", { extra_index_paths = {}, diagnostics = true }, o or {})
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
end)
