-- Resolve the server binary by downloading a prebuilt release asset (the primary
-- path for consumers -- no toolchain needed), falling back to a source build for
-- developers, offline use, or unsupported platforms.
--
-- The binary for the pinned version is cached under stdpath("cache")/tcl-lsp/
-- <version>/, so only the first launch of a new version hits the network; every
-- launch after is instant. Downloads are verified against the release's
-- SHA256SUMS before use.
--
-- The pure helpers and the IO orchestration take an injectable `deps` table so
-- the logic is unit-testable without a network or a real release.

local M = {}

local REPO = "unknownbreaker/tcl-lsp"

-- platform maps a uname to our asset token `<os>-<arch>`, or nil if unsupported.
-- Exposed for testing.
function M._platform(uname)
  uname = uname or vim.uv.os_uname()
  local os = ({ Darwin = "darwin", Linux = "linux" })[uname.sysname]
  local arch = ({ arm64 = "arm64", aarch64 = "arm64", x86_64 = "amd64", amd64 = "amd64" })[uname.machine]
  if not os or not arch then
    return nil
  end
  return os .. "-" .. arch
end

-- asset_name is the release asset for a platform token (matches server/Makefile).
local function asset_name(plat)
  return "tcl-lsp-" .. plat
end

-- _parse_sums turns a SHA256SUMS body into { [filename] = hexhash }. Exposed for
-- testing. Handles the "<hash>  <file>" and "<hash> *<file>" (binary) forms.
function M._parse_sums(body)
  local sums = {}
  for line in (body or ""):gmatch("[^\r\n]+") do
    local hash, file = line:match("^(%x+)%s+%*?(.+)$")
    if hash and file then
      sums[file] = hash:lower()
    end
  end
  return sums
end

-- default HTTP GET: curl, then wget, each with a hard timeout so a bad network
-- can never hang editor startup. Returns (body, nil) or (nil, err). Binary-safe.
local function http_get(url)
  if vim.fn.executable("curl") == 1 then
    local out = vim.system({ "curl", "-fsSL", "--max-time", "120", url }, { text = false }):wait()
    if out.code == 0 then
      return out.stdout
    end
  end
  if vim.fn.executable("wget") == 1 then
    local out = vim.system({ "wget", "-qO-", "--timeout=120", url }, { text = false }):wait()
    if out.code == 0 then
      return out.stdout
    end
  end
  return nil, "download failed (need curl or wget): " .. url
end

-- download fetches and verifies the release binary for `version`, caching it, and
-- returns its path (or nil, err). A cache hit skips the network entirely.
--
-- deps (all optional): get(url)->body|nil,err ; sha256(data)->hex ;
-- notify(msg,level) ; platform()->token ; repo ; cache_dir.
function M.download(version, deps)
  deps = deps or {}
  local get = deps.get or http_get
  local sha256 = deps.sha256 or vim.fn.sha256
  local notify = deps.notify or vim.notify
  local repo = deps.repo or REPO
  local cache = deps.cache_dir or (vim.fn.stdpath("cache") .. "/tcl-lsp")

  local plat = deps.platform or M._platform(deps.uname)
  if not plat then
    return nil, "no prebuilt binary for this OS/arch"
  end
  local asset = asset_name(plat)
  local dir = cache .. "/" .. version
  local bin = dir .. "/" .. asset
  if vim.fn.executable(bin) == 1 then
    return bin -- cache hit: no network
  end

  notify("tcl-lsp: downloading server " .. version .. " (" .. plat .. ")…", vim.log.levels.INFO)
  local base = "https://github.com/" .. repo .. "/releases/download/" .. version .. "/"

  local sums_body, err = get(base .. "SHA256SUMS")
  if not sums_body then
    return nil, err
  end
  local want = M._parse_sums(sums_body)[asset]
  if not want then
    return nil, "no checksum for " .. asset .. " in release " .. version
  end

  local data, derr = get(base .. asset)
  if not data then
    return nil, derr
  end
  if sha256(data):lower() ~= want then
    return nil, "checksum mismatch for " .. asset .. " (corrupt or tampered download)"
  end

  vim.fn.mkdir(dir, "p")
  local f = io.open(bin, "wb")
  if not f then
    return nil, "cannot write " .. bin
  end
  f:write(data)
  f:close()
  vim.uv.fs_chmod(bin, 493) -- 0o755
  return bin
end

-- resolve returns the server binary path for setup(): try the prebuilt download,
-- then fall back to a source build (dev / offline / unsupported platform). opts
-- carries auto_build, and optionally version/cache_dir/repo for tests.
function M.resolve(root, opts)
  opts = opts or {}
  local version = opts.version or require("tcl-lsp.version")
  local bin, err = M.download(version, { cache_dir = opts.cache_dir, repo = opts.repo })
  if bin then
    return bin
  end

  local ok, build = pcall(require, "tcl-lsp.build")
  if ok then
    local src = build.ensure_built(root, opts.auto_build ~= false)
    if src then
      vim.notify(
        "tcl-lsp: prebuilt binary unavailable (" .. (err or "?") .. "); using a source build.",
        vim.log.levels.WARN
      )
      return src
    end
  end
  vim.notify(
    "tcl-lsp: could not download a server binary (" .. (err or "?") .. ") and cannot build from source.\n"
      .. "Install go + make, or download a release binary manually.",
    vim.log.levels.ERROR
  )
  return nil
end

return M
