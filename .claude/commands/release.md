Tag the current commit, push it to GitHub, build the Docker image, and push it to GHCR (`ghcr.io/bro3886/go-docpdf`).

## Steps

### 1. Check for uncommitted changes

Run `git status --porcelain`. If the output is non-empty there are uncommitted changes.

- Ask the user: "There are uncommitted changes. Should I commit them before releasing? If yes, what commit message should I use? (or type 'no' to abort)"
- If the user says yes (and provides a message), stage all tracked+untracked files with `git add -A` and commit with the provided message.
- If the user says no or abort, stop here and tell them to commit or stash first.

### 2. Ensure all commits are pushed to `origin`

Run `git log origin/main..HEAD --oneline` (or the current branch if not on main).
If there are unpushed commits, run `git push origin <branch>` to push them.

### 3. Determine the next tag

Run `git tag --sort=-version:refname | head -1` to find the latest existing tag.
- If no tags exist, start at `v0.1.0`.
- Otherwise increment the **patch** component by default: `v<major>.<minor>.<patch+1>`.
- Ask the user to confirm the tag or provide a different one: "Next tag will be `<proposed>`. Press enter to confirm or type a different tag:"

### 4. Create and push the git tag

```sh
git tag <tag>
git push origin <tag>
```

### 5. Build the Docker image

```sh
docker build -t ghcr.io/bro3886/go-docpdf:<tag> -t ghcr.io/bro3886/go-docpdf:latest .
```

Confirm the build succeeds before proceeding.

### 6. Push to GHCR

```sh
docker push ghcr.io/bro3886/go-docpdf:<tag>
docker push ghcr.io/bro3886/go-docpdf:latest
```

If the push fails with an auth error, tell the user to run:
```sh
gh auth refresh -s write:packages
echo <PAT> | docker login ghcr.io -u <github-username> --password-stdin
```

### 7. Report success

Print a summary:
```
Released <tag>

Git tag:   <tag> → pushed to origin
Images:
  ghcr.io/bro3886/go-docpdf:<tag>
  ghcr.io/bro3886/go-docpdf:latest
```
