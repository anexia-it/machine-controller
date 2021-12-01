# This repository is ANEXIAs fork of machine-controller

To enable us to work fast without being blocked by upstream (and spamming them with PRs), we work on this fork
and give bigger chunks upstream.

Since we have to touch some things that are not supposed to go upstream (like custom CI pipeline), we need some
workflows to prepare branches in this fork to go upstream, by having them rebased on the upstream destination
branch.


## Git workflow

Workflows in this repository are quite normal:

* make a branch for the thing you are working on
* commit your changes into it, maybe even squash sensible (not just everything into a single commit)
* push it and make a PR to `main`

It first gets tricky when changes are supposed to go upstream:

* make a new branch collecting the things we want to give upstream
  * its name should have the prefix `for-upstream/`
* rebase the branch onto the upstream destination branch
  * `git fetch upstream && git rebase --onto upstream/master`
* open the pull request on upstream repository
  * verify only intended changes are included
  * if more than intended changes are included and you cannot sort it out yourself, mark the PR as Draft and ask
    for help - we don't want to annoy upstream with our workflow problems when we can prevent it

Part of this is automated in the github actions workflow `upstream-compat.yaml`, which manages `for-upstream/`
branches for every branch pushed into our Github repository. These branches can then be combined into a suitable
branch for a upstream pull request or used as is. **Beware**: never store anything in branches with name prefix
`for-upstream/`, these are managed by the workflow and force pushed automatically.
