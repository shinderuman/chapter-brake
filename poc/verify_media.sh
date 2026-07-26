#!/bin/sh
set -eu

repo_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
artifact_dir="$repo_dir/poc/artifacts"
log_dir="$repo_dir/poc/logs"
result_file="$log_dir/media-verification.tsv"

decoded_video_hash() {
  ffmpeg -v error -i "$1" -map 0:v:0 -pix_fmt yuv420p -f framemd5 - \
    | shasum -a 256 | awk '{print $1}'
}

decoded_audio_hash() {
  ffmpeg -v error -i "$1" -map 0:a:0 -c:a pcm_s32le -f hash -hash sha256 -
}

packet_hash() {
  stream=$2
  ffmpeg -v error -i "$1" -map "0:$stream:0" -c copy -f hash -hash sha256 -
}

printf 'file\tvideo_decoded_sha256\taudio_decoded_sha256\tvideo_packet_sha256\taudio_packet_sha256\n' \
  >"$result_file"

for name in \
  mkv-no-subtitles.mkv \
  mkv-subtitle-1.mkv \
  mkv-subtitles-1-2.mkv \
  mp4-no-subtitles.mp4
do
  file="$artifact_dir/$name"
  printf '%s\t%s\t%s\t%s\t%s\n' \
    "$name" \
    "$(decoded_video_hash "$file")" \
    "$(decoded_audio_hash "$file")" \
    "$(packet_hash "$file" v)" \
    "$(packet_hash "$file" a)" \
    >>"$result_file"
done

for name in range-1-2 range-2 range-3-4 audio-track-2 audio-tracks-1-2
do
  ffmpeg -hide_banner -nostats -i "$artifact_dir/$name.mkv" \
    -map 0:a:0 -af astats=metadata=1:reset=0 -f null - \
    >"$log_dir/$name.astats.stdout.log" \
    2>"$log_dir/$name.astats.stderr.log"
done

cat "$result_file"

