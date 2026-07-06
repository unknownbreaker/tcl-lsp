" tcl-lsp for Vim via vim-lsp (prabirshrestha/vim-lsp).
"
" Classic Vimscript — works in Vim and Neovim. Source this from your vimrc:
"     source /path/to/tcl-lsp/editors/vim/tcl-lsp.vim
"
" On first use it downloads the prebuilt server binary for your OS/arch from a
" GitHub Release (needs curl or wget), verifies its SHA-256, and caches it under
" ~/.cache/tcl-lsp/ -- parity with the Neovim plugin, no toolchain required.
" `go` + `make` are only a fallback (unsupported platform, offline, or dev).
"
" Prerequisites:
"   - vim-lsp (+ async.vim) installed.
"   - .rvt filetype detection — shipped in this repo's ftdetect/rvt.vim, loaded
"     automatically because this file adds the repo to 'runtimepath'.
"
" Options (set before sourcing):
"   let g:tcl_lsp_cmd = '/abs/path/to/tcl-lsp'   " use a prebuilt binary; skips the build
"   let g:tcl_lsp_auto_build = 0                 " never build; use whatever binary exists
"
" Use :LspDefinition and :LspReferences (or your vim-lsp keymaps).

if exists('g:loaded_tcl_lsp_vim')
  finish
endif
let g:loaded_tcl_lsp_vim = 1

" Repo root from this file: <root>/editors/vim/tcl-lsp.vim. Put the repo on
" 'runtimepath' so autoload/tcl_lsp.vim (the build helpers) and ftdetect/rvt.vim
" resolve even when this file is sourced directly rather than installed.
let s:root = fnamemodify(resolve(expand('<sfile>:p')), ':h:h:h')
if stridx(&runtimepath, s:root) < 0
  execute 'set runtimepath+=' . fnameescape(s:root)
endif

" Resolve the project root. .git wins so ONE server indexes the whole repo
" (TCL/RVT cross-file resolution needs the .tcl definitions and the .rvt call
" sites in a single index). pkgIndex.tcl is only a fallback for non-git
" checkouts: listing both together would let a nearer pkgIndex.tcl win and
" fragment the project into per-package servers — the exact failure that hides
" .rvt references from a .tcl definition.
function! s:tcl_lsp_root_uri(...) abort
  let l:path = lsp#utils#get_buffer_path()
  let l:dir = lsp#utils#find_nearest_parent_file_directory(l:path, ['.git/'])
  if empty(l:dir)
    let l:dir = lsp#utils#find_nearest_parent_file_directory(l:path, ['pkgIndex.tcl'])
  endif
  return empty(l:dir) ? '' : lsp#utils#path_to_uri(l:dir)
endfunction

function! s:tcl_lsp_register() abort
  " An explicit g:tcl_lsp_cmd (a prebuilt binary) is used as-is; otherwise build
  " or refresh the bundled server.
  let l:cmd = get(g:, 'tcl_lsp_cmd', '')
  if empty(l:cmd)
    let l:cmd = tcl_lsp#resolve(s:root, get(g:, 'tcl_lsp_auto_build', 1))
  endif
  if empty(l:cmd) || !executable(l:cmd)
    echohl WarningMsg
    echomsg 'tcl-lsp: no usable server binary; not registering. Build: make -C ' . s:root . '/server build'
    echohl None
    return
  endif
  call lsp#register_server({
    \ 'name': 'tcl-lsp',
    \ 'cmd': {server_info -> [l:cmd]},
    \ 'allowlist': ['tcl', 'rvt'],
    \ 'root_uri': function('s:tcl_lsp_root_uri'),
    \ })
endfunction

augroup tcl_lsp_vim_lsp
  autocmd!
  autocmd User lsp_setup call s:tcl_lsp_register()
augroup END
