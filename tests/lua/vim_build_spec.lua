-- Tests for autoload/tcl_lsp.vim (the Vim build/freshness port), exercised
-- through vim.fn so they run in the existing plenary harness -- no vader needed.
-- These mirror tests/lua/build_spec.lua to keep the Vim and Lua ports in sync.

local uv = vim.uv or vim.loop

local function write(path, content)
  local fd = assert(uv.fs_open(path, "w", 420))
  assert(uv.fs_write(fd, content or ""))
  assert(uv.fs_close(fd))
end

local function set_mtime(path, t)
  assert(uv.fs_utime(path, t, t))
end

local function tmpdir()
  local d = vim.fn.tempname()
  vim.fn.mkdir(d, "p")
  return d
end

describe("tcl_lsp#is_stale (vimscript)", function()
  it("is true when a source is newer than the binary", function()
    local d = tmpdir()
    write(d .. "/a.go")
    set_mtime(d .. "/a.go", 2000)
    assert.equals(1, vim.fn["tcl_lsp#is_stale"](1000, { d .. "/a.go" }))
  end)

  it("is false when every source is older than the binary", function()
    local d = tmpdir()
    write(d .. "/a.go")
    set_mtime(d .. "/a.go", 500)
    assert.equals(0, vim.fn["tcl_lsp#is_stale"](1000, { d .. "/a.go" }))
  end)

  it("ignores missing source files", function()
    assert.equals(0, vim.fn["tcl_lsp#is_stale"](1000, { "/no/such/file.go" }))
  end)
end)

describe("tcl_lsp#_decide (vimscript) — the rebuild decision tree", function()
  local function decide(exists, stale, auto_build, has_tools)
    return vim.fn["tcl_lsp#_decide"](exists, stale, auto_build, has_tools)
  end

  it("uses a fresh existing binary", function()
    assert.equals("use", decide(1, 0, 1, 1))
  end)

  it("builds when the binary is stale and tools are present", function()
    assert.equals("build", decide(1, 1, 1, 1))
  end)

  it("uses the stale binary when go/make are missing", function()
    assert.equals("use", decide(1, 1, 1, 0))
  end)

  it("uses the stale binary when auto_build is off", function()
    assert.equals("use", decide(1, 1, 0, 1))
  end)

  it("builds when the binary is missing and tools are present", function()
    assert.equals("build", decide(0, 0, 1, 1))
  end)

  it("is 'none' when missing and no tools", function()
    assert.equals("none", decide(0, 0, 1, 0))
  end)

  it("is 'none' when missing and auto_build is off", function()
    assert.equals("none", decide(0, 0, 0, 1))
  end)
end)

describe("tcl_lsp#platform (vimscript)", function()
  local function plat(sys, mach)
    return vim.fn["tcl_lsp#platform"](sys, mach)
  end
  it("maps supported os/arch to asset tokens", function()
    assert.equals("darwin-arm64", plat("Darwin", "arm64"))
    assert.equals("darwin-arm64", plat("Darwin", "aarch64"))
    assert.equals("darwin-amd64", plat("Darwin", "x86_64"))
    assert.equals("linux-amd64", plat("Linux", "x86_64"))
    assert.equals("linux-arm64", plat("Linux", "aarch64"))
  end)
  it("returns '' for unsupported platforms", function()
    assert.equals("", plat("Windows_NT", "x86_64"))
    assert.equals("", plat("Linux", "riscv64"))
  end)
end)

describe("tcl_lsp#_parse_sums (vimscript)", function()
  it("parses '<hash>  <file>' and '<hash> *<file>', lowercasing", function()
    local sums = vim.fn["tcl_lsp#_parse_sums"]({
      "abc123  tcl-lsp-darwin-arm64",
      "DEF456 *tcl-lsp-linux-amd64",
    })
    assert.equals("abc123", sums["tcl-lsp-darwin-arm64"])
    assert.equals("def456", sums["tcl-lsp-linux-amd64"])
  end)
end)

describe("tcl_lsp#version (vimscript)", function()
  it("reads the pinned tag from version.lua, ignoring comments", function()
    local d = tmpdir()
    write(d .. "/version.lua", "-- a comment\nreturn \"v9.9.9\"\n")
    assert.equals("v9.9.9", vim.fn["tcl_lsp#version"](d, d .. "/version.lua"))
  end)
  it("returns '' when the file is missing", function()
    assert.equals("", vim.fn["tcl_lsp#version"]("/x", "/no/such/version.lua"))
  end)
end)
