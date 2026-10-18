# shellcheck shell=bash
# The steps of the workflows Release and Publish pages source this file to
# run the program of release as release <command> <arguments>:
#
#   . .github/release.sh
#   release check "$VERSION" > "$RUNNER_TEMP/notes.md"
#
# A refusal of the program goes to the log of the run as an error: its lines
# on the error output of the step, and all of them in one annotation
# ::error::, which the summary of the run shows. The function then fails, and
# with it the step.

# The output of the step, for the annotation when the output of the function
# goes to a file.
exec 3>&1

release() {
  local program="$RUNNER_TEMP/release" refusal="$RUNNER_TEMP/release-refusal.txt"
  # Built once, not run by go run: go run adds «exit status 1» to a refusal.
  if [ ! -e "$program" ] && ! go build -o "$program" ./cmd/release 2>"$refusal"; then
    release_failed "$refusal"
    return 1
  fi
  if ! "$program" "$@" 2>"$refusal"; then
    release_failed "$refusal"
    return 1
  fi
}

# release_failed prints the refusal in the file $1 on the error output and as
# an annotation. An annotation is one line: its line ends and % are written
# as %0A, %0D and %25.
release_failed() {
  cat "$1" >&2
  echo "::error::$(sed -e 's/%/%25/g' -e 's/\r/%0D/g' "$1" | awk 'NR > 1 { printf "%%0A" } { printf "%s", $0 }')" >&3
}
