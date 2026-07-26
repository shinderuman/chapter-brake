#!/bin/sh
set -eu

repo_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
artifact_dir="$repo_dir/poc/artifacts"
fixture_dir="$repo_dir/poc/fixture"
log_dir="$repo_dir/poc/logs"

mkdir -p "$artifact_dir" "$log_dir"

ffmpeg -hide_banner -y \
  -f lavfi -i "color=c=red:s=320x180:r=30:d=3" \
  -f lavfi -i "color=c=green:s=320x180:r=30:d=3" \
  -f lavfi -i "color=c=blue:s=320x180:r=30:d=3" \
  -f lavfi -i "color=c=yellow:s=320x180:r=30:d=3" \
  -f lavfi -i "aevalsrc=0.08*sin(2*PI*440*t)|0.08*sin(2*PI*440*t)|0.08*sin(2*PI*440*t)|0.08*sin(2*PI*440*t)|0.08*sin(2*PI*440*t)|0.08*sin(2*PI*440*t):s=48000:d=12:c=5.1" \
  -f lavfi -i "aevalsrc=0.08*sin(2*PI*880*t)|0.08*sin(2*PI*880*t)|0.08*sin(2*PI*880*t)|0.08*sin(2*PI*880*t)|0.08*sin(2*PI*880*t)|0.08*sin(2*PI*880*t):s=48000:d=12:c=5.1" \
  -i "$fixture_dir/subtitle-ja.srt" \
  -i "$fixture_dir/subtitle-en.srt" \
  -f ffmetadata -i "$fixture_dir/chapters.ffmeta" \
  -filter_complex "[0:v][1:v][2:v][3:v]concat=n=4:v=1:a=0[v]" \
  -map "[v]" -map 4:a -map 5:a -map 6:0 -map 7:0 \
  -map_metadata 8 -map_chapters 8 \
  -c:v libx264 -preset ultrafast -crf 18 -g 30 -keyint_min 30 -sc_threshold 0 \
  -c:a ac3 -b:a 640k \
  -c:s srt \
  -metadata title="PoC Source Original Title" \
  -metadata:s:a:0 language=jpn -metadata:s:a:0 title="Tone 440Hz" \
  -metadata:s:a:1 language=eng -metadata:s:a:1 title="Tone 880Hz" \
  -metadata:s:s:0 language=jpn -metadata:s:s:0 title="Japanese Test" \
  -metadata:s:s:1 language=eng -metadata:s:s:1 title="English Test" \
  -t 12 \
  "$artifact_dir/source-four-chapters.mkv" \
  >"$log_dir/generate-fixture.stdout.log" \
  2>"$log_dir/generate-fixture.stderr.log"

ffprobe -v error -show_format -show_streams -show_chapters -of json \
  "$artifact_dir/source-four-chapters.mkv" \
  >"$log_dir/source-four-chapters.ffprobe.json"

