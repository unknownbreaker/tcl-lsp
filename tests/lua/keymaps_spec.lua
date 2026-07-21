-- Tests for the keymaps option: turning the `keymaps` (named action -> lhs) and
-- `keys` (lazy.nvim-style) setup options into buffer-local keymap specs.

local tcl = require("tcl-lsp")

local function by_lhs(specs)
  local m = {}
  for _, s in ipairs(specs) do
    m[s.lhs] = s
  end
  return m
end

describe("tcl-lsp keymap specs", function()
  it("maps named actions to a key with a desc and a function", function()
    local specs, unknown = tcl._keymap_specs({ incoming_calls = "<leader>ci", definition = "gd" }, {})
    assert.are.equal(0, #unknown)
    local m = by_lhs(specs)
    assert.is_not_nil(m["<leader>ci"])
    assert.are.equal("n", m["<leader>ci"].mode)
    assert.are.equal("tcl-lsp: incoming_calls", m["<leader>ci"].desc)
    assert.is_function(m["<leader>ci"].rhs)
    assert.is_not_nil(m["gd"])
  end)

  it("warns on an unknown action and binds nothing for it", function()
    local specs, unknown = tcl._keymap_specs({ bogus = "x" }, {})
    assert.are.same({ "bogus" }, unknown)
    assert.are.equal(0, #specs)
  end)

  it("treats out-of-scope LSP actions as unknown (server never advertises them)", function()
    -- hover/declaration/type_definition were once offered but the server has no
    -- such capabilities: binding them produced a key that silently did nothing.
    -- They must now warn like any other unknown action.
    for _, action in ipairs({ "hover", "declaration", "type_definition" }) do
      local specs, unknown = tcl._keymap_specs({ [action] = "K" }, {})
      assert.are.same({ action }, unknown)
      assert.are.equal(0, #specs)
    end
  end)

  it("leaves an action set to false unbound", function()
    local specs = tcl._keymap_specs({ definition = false }, {})
    assert.are.equal(0, #specs)
  end)

  it("supports the lazy-style keys escape hatch", function()
    local fn = function() end
    local specs = tcl._keymap_specs({}, {
      { "K", fn, desc = "Hover", mode = "n" },
      { "gx", fn },
    })
    assert.are.equal(2, #specs)
    local m = by_lhs(specs)
    assert.are.equal("Hover", m["K"].desc)
    assert.are.equal(fn, m["K"].rhs)
    assert.are.equal("n", m["gx"].mode) -- defaults to normal mode
  end)

  it("combines both forms", function()
    local fn = function() end
    local specs = tcl._keymap_specs({ outgoing_calls = "<leader>co" }, { { "K", fn } })
    assert.are.equal(2, #specs)
  end)
end)
