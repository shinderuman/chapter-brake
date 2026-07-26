#!/bin/sh
set -eu

repo_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
artifact_dir="$repo_dir/poc/artifacts"
log_dir="$repo_dir/poc/logs"
source_file="$artifact_dir/source-four-chapters.mkv"

mkdir -p "$artifact_dir" "$log_dir"

run_handbrake() {
  name=$1
  preset=$2
  shift 2
  output="$artifact_dir/$name"
  stem=${name%.*}

  rm -f "$output"
  HandBrakeCLI \
    --json \
    --preset-import-gui \
    --preset "$preset" \
    -i "$source_file" \
    -o "$output" \
    "$@" \
    >"$log_dir/$stem.stdout.log" \
    2>"$log_dir/$stem.stderr.log"

  ffprobe -v error -show_format -show_streams -show_chapters -of json \
    "$output" >"$log_dir/$stem.ffprobe.json"
}

no_subtitle_args="--subtitle none --subtitle-burned=none --subtitle-default=none"

# P-03 and P-04: chapter-only ranges and audio selection matrix.
# shellcheck disable=SC2086
run_handbrake range-1-2.mkv "MKV Presets" \
  --chapters 1-2 --markers \
  --audio 1 --aencoder ac3 --ab 640 --mixdown 5point1 \
  $no_subtitle_args
# shellcheck disable=SC2086
run_handbrake range-3-4.mkv "MKV Presets" \
  --chapters 3-4 --markers \
  --audio 1 --aencoder ac3 --ab 640 --mixdown 5point1 \
  $no_subtitle_args
# shellcheck disable=SC2086
run_handbrake range-2.mkv "MKV Presets" \
  --chapters 2 --markers \
  --audio 1 --aencoder ac3 --ab 640 --mixdown 5point1 \
  $no_subtitle_args
# shellcheck disable=SC2086
run_handbrake audio-track-2.mkv "MKV Presets" \
  --chapters 1 --markers \
  --audio 2 --aencoder ac3 --ab 640 --mixdown 5point1 \
  $no_subtitle_args
# shellcheck disable=SC2086
run_handbrake audio-tracks-1-2.mkv "MKV Presets" \
  --chapters 1 --markers \
  --audio 1,2 --aencoder ac3,ac3 --ab 640,160 --mixdown 5point1,stereo \
  $no_subtitle_args

# P-05: no subtitle, one soft subtitle, and two soft subtitles.
# shellcheck disable=SC2086
run_handbrake mkv-no-subtitles.mkv "MKV Presets" \
  --chapters 1-4 --markers \
  --audio 1 --aencoder ac3 --ab 640 --mixdown 5point1 \
  $no_subtitle_args
run_handbrake mkv-subtitle-1.mkv "MKV Presets" \
  --chapters 1-4 --markers \
  --audio 1 --aencoder ac3 --ab 640 --mixdown 5point1 \
  --subtitle 1 --subtitle-burned=none --subtitle-default=none
run_handbrake mkv-subtitles-1-2.mkv "MKV Presets" \
  --chapters 1-4 --markers \
  --audio 1 --aencoder ac3 --ab 640 --mixdown 5point1 \
  --subtitle 1,2 --subtitle-burned=none --subtitle-default=none

# MP4 has no subtitle stream and no burned subtitle.
# shellcheck disable=SC2086
run_handbrake mp4-no-subtitles.mp4 "MP4 Presets" \
  --chapters 1-4 --markers \
  --audio 1 --aencoder ac3 --ab 640 --mixdown 5point1 \
  $no_subtitle_args

