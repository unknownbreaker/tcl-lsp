-- The GitHub Release tag whose prebuilt server binary this plugin downloads.
-- Bumped automatically by `make publish` (see docs/RELEASING.md) so the Lua
-- client and the server binary stay in lockstep. Until the matching release is
-- published, the client falls back to building from source (needs go + make).
return "v0.4.0"
