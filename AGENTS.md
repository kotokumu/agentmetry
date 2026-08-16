# Repository instructions

## Releases

- Do not create or push release tags directly.
- Use the Release Please PR created from `main` for every release.
- Keep Conventional Commit titles so Release Please can calculate the next
  version and changelog.
- Merge the Release Please PR only after its checks pass. Release Please owns
  the tag and GitHub Release; the distribution workflow owns release assets.
