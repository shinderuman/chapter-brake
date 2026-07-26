#!/bin/sh
set -eu

repo_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
artifact_dir="$repo_dir/poc/artifacts"
log_dir="$repo_dir/poc/logs"
output="$artifact_dir/large-interrupt-source.mkv"

mkdir -p "$artifact_dir" "$log_dir"
ffmpeg -hide_banner -y \
  -stream_loop 300 \
  -i "$artifact_dir/mkv-subtitles-1-2.mkv" \
  -t 3600 \
  -map 0 -c copy \
  "$output" \
  >"$log_dir/prepare-interrupt-fixture.stdout.log" \
  2>"$log_dir/prepare-interrupt-fixture.stderr.log"

ffprobe -v error -show_format -show_streams -of json "$output" \
  >"$log_dir/large-interrupt-source.ffprobe.json"

