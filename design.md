# gg

## help

prints a help message; baked in via cobra

## init

inits an existing repo (whether a bare clone or a normal clone) as a managed gg repo. the layout is opinionated.

a gg configured repo looks like this:

repo/
  .bare/  <- the bare repo clone
  main/   <- the primary worktree, typically main or master
  feat-a/ <- feature branch a; gg uses a flat layout for branches within a repo; lineage does not correlate to file structure

## clone

clones a remote repo into a managed gg repo.

examples:

`gg clone https://github.com/owner/repo.git` -> `repo`
`gg clone https://github.com/owner/repo.git my-repo` -> `my-repo`

## new
