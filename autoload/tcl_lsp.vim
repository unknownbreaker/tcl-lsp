" autoload/tcl_lsp.vim — resolve the tcl-lsp server binary for Vim users:
" download a prebuilt release asset (mirroring lua/tcl-lsp/download.lua), falling
" back to a source build (mirroring lua/tcl-lsp/build.lua). Portable classic
" Vimscript (Vim 8.1+ / Neovim). Keep the two ports' behavior in sync.

let s:repo = 'unknownbreaker/tcl-lsp'

" tcl_lsp#sources: the server files whose change warrants a rebuild — every .go
" file plus the build inputs.
function! tcl_lsp#sources(server_dir) abort
  let l:list = split(globpath(a:server_dir, '**/*.go'), "\n")
  for l:extra in ['go.mod', 'go.sum', 'Makefile']
    call add(l:list, a:server_dir . '/' . l:extra)
  endfor
  return l:list
endfunction

" tcl_lsp#is_stale: 1 if any source is newer than bin_mtime, else 0. getftime()
" returns -1 for a missing file, which is never newer than a real mtime, so
" missing sources are ignored automatically.
function! tcl_lsp#is_stale(bin_mtime, sources) abort
  for l:f in a:sources
    if getftime(l:f) > a:bin_mtime
      return 1
    endif
  endfor
  return 0
endfunction

" tcl_lsp#_decide: the rebuild decision tree as a PURE function (no filesystem,
" no shelling out) so it is unit-testable. All args are 0/1. Returns:
"   'use'   — a usable binary exists (fresh, or stale but unbuildable): run it
"   'build' — (re)build, then run
"   'none'  — no binary and it cannot be built
function! tcl_lsp#_decide(exists, stale, auto_build, has_tools) abort
  if a:exists && !a:stale
    return 'use'
  endif
  if !a:auto_build || !a:has_tools
    return a:exists ? 'use' : 'none'
  endif
  return 'build'
endfunction

" tcl_lsp#ensure_built: return the server binary path, building it from
" <root>/server when missing or stale (sources newer than the binary). Returns
" '' when no binary exists and one cannot be built; a stale binary that cannot be
" rebuilt is returned as-is. Synchronous — a one-time, ~seconds cost, matching
" the Neovim plugin.
function! tcl_lsp#ensure_built(root, auto_build) abort
  let l:server_dir = a:root . '/server'
  let l:bin = l:server_dir . '/tcl-lsp'
  let l:bin_mtime = getftime(l:bin)
  let l:exists = l:bin_mtime >= 0
  let l:stale = l:exists ? tcl_lsp#is_stale(l:bin_mtime, tcl_lsp#sources(l:server_dir)) : 0
  let l:has_tools = executable('go') && executable('make')

  let l:action = tcl_lsp#_decide(l:exists, l:stale, a:auto_build ? 1 : 0, l:has_tools ? 1 : 0)
  if l:action ==# 'use'
    return l:bin
  elseif l:action ==# 'none'
    if a:auto_build && !l:has_tools
      echohl WarningMsg
      echomsg 'tcl-lsp: server binary missing and go/make not found. Build: make -C ' . l:server_dir . ' build'
      echohl None
    endif
    return ''
  endif

  " l:action ==# 'build'
  echomsg l:exists ? 'tcl-lsp: server sources changed — rebuilding…' : 'tcl-lsp: building server (one-time)…'
  let l:out = system('make -C ' . shellescape(l:server_dir) . ' build')
  if v:shell_error != 0
    echohl ErrorMsg
    echomsg 'tcl-lsp: build failed: ' . l:out
    echohl None
    return l:exists ? l:bin : ''
  endif
  echomsg 'tcl-lsp: server built.'
  return l:bin
endfunction

" ---- prebuilt-binary download (mirrors lua/tcl-lsp/download.lua) -------------

" tcl_lsp#platform: the release asset token '<os>-<arch>' for this machine, or ''
" if unsupported. Pass [sysname, machine] to override (for tests).
function! tcl_lsp#platform(...) abort
  let l:sys = a:0 >= 1 ? a:1 : trim(system('uname -s'))
  let l:mach = a:0 >= 2 ? a:2 : trim(system('uname -m'))
  let l:os = {'Darwin': 'darwin', 'Linux': 'linux'}
  let l:arch = {'arm64': 'arm64', 'aarch64': 'arm64', 'x86_64': 'amd64', 'amd64': 'amd64'}
  return (has_key(l:os, l:sys) && has_key(l:arch, l:mach)) ? l:os[l:sys] . '-' . l:arch[l:mach] : ''
endfunction

" tcl_lsp#version: the pinned release tag, read from lua/tcl-lsp/version.lua so
" the Vim and Neovim clients share one source of truth. Pass a path to override.
function! tcl_lsp#version(root, ...) abort
  let l:path = a:0 >= 1 ? a:1 : a:root . '/lua/tcl-lsp/version.lua'
  if !filereadable(l:path)
    return ''
  endif
  for l:line in readfile(l:path)
    let l:tag = matchstr(l:line, '^\s*return\s*"\zs[^"]*\ze"')
    if !empty(l:tag)
      return l:tag
    endif
  endfor
  return ''
endfunction

" tcl_lsp#_parse_sums: { filename: hexhash } from SHA256SUMS lines (handles the
" '<hash>  <file>' and '<hash> *<file>' forms). Exposed for testing.
function! tcl_lsp#_parse_sums(lines) abort
  let l:sums = {}
  for l:line in a:lines
    let l:m = matchlist(l:line, '^\(\x\+\)\s\+\*\?\(.\+\)$')
    if !empty(l:m)
      let l:sums[l:m[2]] = tolower(l:m[1])
    endif
  endfor
  return l:sums
endfunction

function! s:cache_dir() abort
  let l:base = empty($XDG_CACHE_HOME) ? expand('~/.cache') : $XDG_CACHE_HOME
  return l:base . '/tcl-lsp'
endfunction

" s:http_get returns a URL's body as a string (text only), or '' on failure.
function! s:http_get(url) abort
  if executable('curl')
    let l:out = system('curl -fsSL --max-time 120 ' . shellescape(a:url))
    if v:shell_error == 0
      return l:out
    endif
  endif
  if executable('wget')
    let l:out = system('wget -qO- --timeout=120 ' . shellescape(a:url))
    if v:shell_error == 0
      return l:out
    endif
  endif
  return ''
endfunction

" s:http_download writes a URL straight to dest (binary-safe, unlike a string).
function! s:http_download(url, dest) abort
  if executable('curl')
    call system('curl -fsSL --max-time 120 -o ' . shellescape(a:dest) . ' ' . shellescape(a:url))
    if v:shell_error == 0
      return 1
    endif
  endif
  if executable('wget')
    call system('wget -q --timeout=120 -O ' . shellescape(a:dest) . ' ' . shellescape(a:url))
    if v:shell_error == 0
      return 1
    endif
  endif
  return 0
endfunction

" s:sha256 returns the hex digest of a file, or '' if no hashing tool is found
" (in which case the download is rejected -- we never run an unverified binary).
function! s:sha256(path) abort
  if executable('sha256sum')
    return matchstr(system('sha256sum ' . shellescape(a:path)), '^\x\+')
  elseif executable('shasum')
    return matchstr(system('shasum -a 256 ' . shellescape(a:path)), '^\x\+')
  endif
  return ''
endfunction

" tcl_lsp#download: fetch + checksum-verify + cache the release binary for the
" pinned version. Returns the path, or '' on any failure (caller falls back to a
" source build). A cache hit skips the network entirely.
function! tcl_lsp#download(root) abort
  let l:plat = tcl_lsp#platform()
  if empty(l:plat)
    return ''
  endif
  let l:version = tcl_lsp#version(a:root)
  if empty(l:version)
    return ''
  endif

  let l:asset = 'tcl-lsp-' . l:plat
  let l:dir = s:cache_dir() . '/' . l:version
  let l:bin = l:dir . '/' . l:asset
  if executable(l:bin)
    return l:bin
  endif

  echomsg 'tcl-lsp: downloading server ' . l:version . ' (' . l:plat . ')…'
  let l:base = 'https://github.com/' . s:repo . '/releases/download/' . l:version . '/'

  let l:sums = s:http_get(l:base . 'SHA256SUMS')
  if empty(l:sums)
    return ''
  endif
  let l:want = get(tcl_lsp#_parse_sums(split(l:sums, "\n")), l:asset, '')
  if empty(l:want)
    return ''
  endif

  call mkdir(l:dir, 'p')
  " Download beside the final path (same filesystem) so the rename is atomic.
  let l:tmp = l:bin . '.download'
  if !s:http_download(l:base . l:asset, l:tmp)
    return ''
  endif
  if tolower(s:sha256(l:tmp)) !=# l:want
    call delete(l:tmp)
    echohl ErrorMsg | echomsg 'tcl-lsp: checksum mismatch for ' . l:asset . ' (corrupt or tampered)' | echohl None
    return ''
  endif
  call rename(l:tmp, l:bin)
  call setfperm(l:bin, 'rwxr-xr-x')
  return l:bin
endfunction

" tcl_lsp#resolve: the server binary the Vim client should run. Prefer the
" prebuilt release download (no toolchain); fall back to a source build for
" unsupported platforms, offline use, or local development.
function! tcl_lsp#resolve(root, auto_build) abort
  let l:bin = tcl_lsp#download(a:root)
  if !empty(l:bin)
    return l:bin
  endif
  return tcl_lsp#ensure_built(a:root, a:auto_build)
endfunction
