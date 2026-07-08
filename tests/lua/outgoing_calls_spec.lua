-- Tests for the outgoing-calls quickfix items. Neovim's deprecated built-in
-- handler pairs the callee's file (to.uri) with fromRanges (caller-file
-- coordinates), so cross-file callees land on the right file at a meaningless
-- line. Our items must jump to the CALLEE's definition instead.

local tcl = require("tcl-lsp")

-- A cross-file outgoing call: caller at line 1 of caller.tcl, callee defined at
-- line 5 of callee.tcl. The built-in handler would emit callee.tcl:2 (caller's
-- line in the callee's file) — the reported bug.
local cross_file_result = {
  {
    to = {
      name = "far_helper",
      detail = "::far_helper",
      uri = "file:///proj/callee.tcl",
      range = { start = { line = 5, character = 5 }, ["end"] = { line = 5, character = 15 } },
      selectionRange = { start = { line = 5, character = 5 }, ["end"] = { line = 5, character = 15 } },
    },
    fromRanges = {
      { start = { line = 1, character = 2 }, ["end"] = { line = 1, character = 12 } },
    },
  },
}

describe("tcl-lsp outgoing-calls quickfix items", function()
  it("jumps to the callee's definition, not fromRanges in the callee's file", function()
    local items = tcl._outgoing_call_items(cross_file_result)
    assert.equals(1, #items)
    assert.equals(vim.uri_to_fname("file:///proj/callee.tcl"), items[1].filename)
    assert.equals(6, items[1].lnum) -- callee def line (0-based 5 -> 1-based 6)
    assert.equals(6, items[1].col)
    -- The regression this guards: the built-in handler would have used the
    -- caller's line (0-based 1 -> qf lnum 2) in the callee's file.
    assert.is_not.equals(2, items[1].lnum)
  end)

  it("collapses multiple call sites into one callee entry with a count", function()
    local result = vim.deepcopy(cross_file_result)
    result[1].fromRanges = {
      { start = { line = 1, character = 2 }, ["end"] = { line = 1, character = 12 } },
      { start = { line = 3, character = 2 }, ["end"] = { line = 3, character = 12 } },
    }
    local items = tcl._outgoing_call_items(result)
    assert.equals(1, #items)
    assert.matches("2 call sites", items[1].text)
  end)

  it("falls back to range when selectionRange is absent, and name when detail is", function()
    local result = vim.deepcopy(cross_file_result)
    result[1].to.selectionRange = nil
    result[1].to.detail = nil
    local items = tcl._outgoing_call_items(result)
    assert.equals(6, items[1].lnum)
    assert.equals("far_helper", items[1].text)
  end)

  it("returns empty for nil or empty results", function()
    assert.equals(0, #tcl._outgoing_call_items(nil))
    assert.equals(0, #tcl._outgoing_call_items({}))
  end)
end)
