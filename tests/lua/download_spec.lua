-- Tests for the prebuilt-binary download/verify/cache logic. IO is exercised
-- against a temp cache dir; the network and checksum are dependency-injected.

local dl = require("tcl-lsp.download")

describe("tcl-lsp download: platform mapping", function()
  it("maps supported os/arch to release asset tokens", function()
    assert.equals("darwin-arm64", dl._platform({ sysname = "Darwin", machine = "arm64" }))
    assert.equals("darwin-arm64", dl._platform({ sysname = "Darwin", machine = "aarch64" }))
    assert.equals("darwin-amd64", dl._platform({ sysname = "Darwin", machine = "x86_64" }))
    assert.equals("linux-amd64", dl._platform({ sysname = "Linux", machine = "x86_64" }))
    assert.equals("linux-arm64", dl._platform({ sysname = "Linux", machine = "aarch64" }))
  end)
  it("returns nil for unsupported platforms", function()
    assert.is_nil(dl._platform({ sysname = "Windows_NT", machine = "x86_64" }))
    assert.is_nil(dl._platform({ sysname = "Linux", machine = "riscv64" }))
  end)
end)

describe("tcl-lsp download: SHA256SUMS parsing", function()
  it("parses '<hash>  <file>' lines", function()
    local sums = dl._parse_sums("abc123  tcl-lsp-darwin-arm64\ndef456  tcl-lsp-linux-amd64\n")
    assert.equals("abc123", sums["tcl-lsp-darwin-arm64"])
    assert.equals("def456", sums["tcl-lsp-linux-amd64"])
  end)
  it("handles the binary marker '*' and CRLF, lowercasing", function()
    local sums = dl._parse_sums("ABCD *tcl-lsp-linux-arm64\r\n")
    assert.equals("abcd", sums["tcl-lsp-linux-arm64"])
  end)
end)

describe("tcl-lsp download: fetch/verify/cache", function()
  local tmp
  before_each(function()
    tmp = vim.fn.tempname()
  end)

  -- deps that download a fake asset whose sha256 is injected to match.
  local function deps(over)
    local d = {
      platform = "linux-amd64",
      cache_dir = tmp,
      repo = "owner/repo",
      notify = function() end,
      sha256 = function(_)
        return "deadbeef01" -- hex, as real sha256 output is
      end,
      get = function(url)
        if url:match("SHA256SUMS$") then
          return "deadbeef01  tcl-lsp-linux-amd64\n"
        end
        return "BINARYDATA"
      end,
    }
    return vim.tbl_extend("force", d, over or {})
  end

  it("downloads, verifies, caches, and returns an executable path", function()
    local bin, err = dl.download("v1.2.3", deps())
    assert.is_nil(err)
    assert.is_not_nil(bin)
    assert.equals(1, vim.fn.filereadable(bin))
    local f = io.open(bin, "rb")
    local data = f:read("*a")
    f:close()
    assert.equals("BINARYDATA", data)
    assert.equals(1, vim.fn.executable(bin))
  end)

  it("serves a cache hit without touching the network", function()
    local bin = dl.download("v1.2.3", deps())
    local fetched = false
    local bin2 = dl.download("v1.2.3", deps({
      get = function()
        fetched = true
        return nil, "must not fetch on a cache hit"
      end,
    }))
    assert.equals(bin, bin2)
    assert.is_false(fetched)
  end)

  it("rejects a checksum mismatch (no file written)", function()
    local bin, err = dl.download("v1.2.3", deps({
      sha256 = function()
        return "0badc0de99" -- hex, but not the expected hash
      end,
    }))
    assert.is_nil(bin)
    assert.matches("checksum mismatch", err)
  end)

  it("errors on an unsupported platform", function()
    local bin, err = dl.download("v1.2.3", {
      cache_dir = tmp,
      notify = function() end,
      uname = { sysname = "Windows_NT", machine = "x86_64" },
    })
    assert.is_nil(bin)
    assert.matches("no prebuilt", err)
  end)

  it("surfaces a download failure", function()
    local bin, err = dl.download("v1.2.3", deps({
      get = function()
        return nil, "network down"
      end,
    }))
    assert.is_nil(bin)
    assert.matches("network down", err)
  end)
end)
