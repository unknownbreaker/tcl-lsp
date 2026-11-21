-- The options reach the server through each client's real wiring: Neovim's
-- setup() into vim.lsp.config, and the classic-Vim plugin into
-- lsp#register_server (vim-lsp, stubbed here).

local tcl = require("tcl-lsp")

describe("tcl-lsp setup() wiring", function()
  local notify = vim.notify
  after_each(function()
    vim.notify = notify
  end)

  it("hands the init options to vim.lsp.config", function()
    tcl.setup({ cmd = { "true" }, checks = { ["unreachable-code"] = false } })
    assert.are.same({ checks = { ["unreachable-code"] = false } }, vim.lsp.config["tcl_lsp"].init_options)
  end)

  it("warns about a checks that is not a table, and does not crash", function()
    local warned
    vim.notify = function(msg, level)
      if level == vim.log.levels.WARN and msg:find("checks", 1, true) then
        warned = true
      end
    end
    local ok, err = pcall(tcl.setup, { cmd = { "true" }, checks = "unreachable-code" })
    assert.is_true(ok, tostring(err))
    assert.is_true(warned)
  end)

  it("warns about checks written as a list of ids", function()
    local warned
    vim.notify = function(msg, level)
      if level == vim.log.levels.WARN and msg:find("checks", 1, true) then
        warned = true
      end
    end
    tcl.setup({ cmd = { "true" }, checks = { "unreachable-code" } })
    assert.is_true(warned)
  end)
end)

describe("classic-Vim plugin wiring", function()
  it("registers g:tcl_lsp_checks as initialization_options", function()
    local root = vim.fn.fnamemodify(debug.getinfo(1, "S").source:sub(2), ":p:h:h:h")
    local stub = vim.fn.tempname()
    vim.fn.mkdir(stub .. "/autoload", "p")
    vim.fn.writefile({
      "function! lsp#register_server(server) abort",
      "  let g:tcl_lsp_test_registered = a:server",
      "endfunction",
    }, stub .. "/autoload/lsp.vim")
    vim.opt.runtimepath:prepend(stub)
    vim.g.tcl_lsp_cmd = vim.fn.exepath("true")
    vim.cmd([[let g:tcl_lsp_checks = {'unreachable-code': 0}]])
    vim.g.loaded_tcl_lsp_vim = nil
    vim.cmd("source " .. vim.fn.fnameescape(root .. "/editors/vim/tcl-lsp.vim"))
    vim.cmd("doautocmd User lsp_setup")
    local reg = vim.g.tcl_lsp_test_registered
    vim.opt.runtimepath:remove(stub)
    for _, g in ipairs({ "tcl_lsp_cmd", "tcl_lsp_checks", "tcl_lsp_test_registered" }) do
      vim.g[g] = nil
    end
    vim.api.nvim_del_augroup_by_name("tcl_lsp_vim_lsp")
    assert.are.same("tcl-lsp", reg.name)
    assert.are.same({ checks = { ["unreachable-code"] = false } }, reg.initialization_options)
  end)
end)
