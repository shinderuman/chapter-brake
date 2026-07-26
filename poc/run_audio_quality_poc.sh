#!/bin/sh
set -eu

repo_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
artifact_dir="$repo_dir/poc/artifacts"
log_dir="$repo_dir/poc/logs"
source_file="$artifact_dir/source-four-chapters.mkv"

mkdir -p "$artifact_dir" "$log_dir"

run_case() {
  name=$1
  audio=$2
  encoders=$3
  bitrates=$4
  mixdowns=$5
  input=${6:-$source_file}
  output="$artifact_dir/$name.mkv"

  rm -f "$output"
  HandBrakeCLI \
    --json \
    --preset-import-gui \
    --preset "MKV Presets" \
    -i "$input" \
    -o "$output" \
    --chapters 1 \
    --no-align-av \
    --audio "$audio" \
    --aencoder "$encoders" \
    --ab "$bitrates" \
    --mixdown "$mixdowns" \
    --arate auto \
    --subtitle none \
    --subtitle-burned=none \
    --subtitle-default=none \
    >"$log_dir/$name.stdout.log" \
    2>"$log_dir/$name.stderr.log"

  ffprobe -v error -show_format -show_streams -of json \
    "$output" >"$log_dir/$name.ffprobe.json"
}

run_case audio-quality-track-1 \
  1,1 \
  copy:ac3,ca_aac \
  640,160 \
  5point1,stereo

run_case audio-quality-track-2 \
  2,2 \
  copy:ac3,ca_aac \
  640,160 \
  5point1,stereo

run_case audio-quality-tracks-1-2 \
  1,1,2,2 \
  copy:ac3,ca_aac,copy:ac3,ca_aac \
  640,160,640,160 \
  5point1,stereo,5point1,stereo

rm -f "$artifact_dir/source-one-audio.mkv"
ffmpeg -v error \
  -i "$source_file" \
  -map 0:v:0 \
  -map 0:a:0 \
  -map 0:s? \
  -map_chapters 0 \
  -map_metadata 0 \
  -c copy \
  "$artifact_dir/source-one-audio.mkv"

HandBrakeCLI \
  --json \
  --scan \
  -i "$artifact_dir/source-one-audio.mkv" \
  >"$log_dir/source-one-audio-scan.stdout.log" \
  2>"$log_dir/source-one-audio-scan.stderr.log"

rm -f "$artifact_dir/source-aac-audio.mkv"
ffmpeg -v error \
  -i "$source_file" \
  -map 0:v:0 \
  -map 0:a:0 \
  -map 0:s? \
  -map_chapters 0 \
  -map_metadata 0 \
  -c:v copy \
  -c:a aac \
  -b:a 256k \
  -c:s copy \
  "$artifact_dir/source-aac-audio.mkv"

run_case audio-quality-fallback \
  1,1 \
  ca_aac,ca_aac \
  640,160 \
  5point1,stereo \
  "$artifact_dir/source-aac-audio.mkv"

rm -f "$artifact_dir/audio-quality-mp4.mp4"
HandBrakeCLI \
  --json \
  --preset-import-gui \
  --preset "MP4 Presets" \
  -i "$source_file" \
  -o "$artifact_dir/audio-quality-mp4.mp4" \
  --chapters 1 \
  --no-align-av \
  --audio 1,1 \
  --aencoder copy:ac3,ca_aac \
  --ab 640,160 \
  --mixdown 5point1,stereo \
  --arate auto \
  --subtitle none \
  --subtitle-burned=none \
  --subtitle-default=none \
  >"$log_dir/audio-quality-mp4.stdout.log" \
  2>"$log_dir/audio-quality-mp4.stderr.log"

ffprobe -v error -show_format -show_streams -of json \
  "$artifact_dir/audio-quality-mp4.mp4" \
  >"$log_dir/audio-quality-mp4.ffprobe.json"
