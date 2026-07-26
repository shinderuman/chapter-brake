#!/bin/sh
set -eu

repo_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
artifact_dir="$repo_dir/poc/artifacts"
log_dir="$repo_dir/poc/logs"

probe() {
  ffprobe -v error -show_format -show_streams -show_chapters -of json \
    "$1" >"$2"
}

payload_hashes() {
  file=$1
  output=$2
  {
    ffmpeg -v error -i "$file" -map 0:v:0 -c copy -f hash -hash sha256 -
    ffmpeg -v error -i "$file" -map 0:a:0 -c copy -f hash -hash sha256 -
    ffmpeg -v error -i "$file" -map 0:v:0 -pix_fmt yuv420p -f framemd5 - \
      | shasum -a 256 | awk '{print "decoded-video-sha256=" $1}'
    ffmpeg -v error -i "$file" -map 0:a:0 -c:a pcm_s32le -f hash -hash sha256 -
  } >"$output"
}

structure() {
  jq '{
    format_name: .format.format_name,
    duration: .format.duration,
    start_time: .format.start_time,
    streams: [.streams[] | {
      index, codec_name, codec_type, profile, time_base,
      start_pts, start_time, duration_ts, duration, bit_rate,
      channels, channel_layout, sample_rate, avg_frame_rate,
      disposition
    }],
    chapters: [.chapters[] | {
      id, time_base, start, start_time, end, end_time,
      title: .tags.title
    }]
  }' "$1" >"$2"
}

mkv_encode="$artifact_dir/.chapterbrake-poc-mkv-encode.mkv"
mkv_final="$artifact_dir/日本語 Title #01.mkv"
rm -f "$mkv_encode" "$mkv_final"
cp "$artifact_dir/mkv-subtitles-1-2.mkv" "$mkv_encode"
probe "$mkv_encode" "$log_dir/metadata-mkv-before.ffprobe.json"
structure "$log_dir/metadata-mkv-before.ffprobe.json" "$log_dir/metadata-mkv-before.structure.json"
payload_hashes "$mkv_encode" "$log_dir/metadata-mkv-before.hashes.txt"

start_ns=$(perl -MTime::HiRes=time -e 'printf "%.0f\n", time() * 1000000000')
mkvpropedit "$mkv_encode" --edit info --set "title=日本語 Title #01" \
  >"$log_dir/mkvpropedit.stdout.log" \
  2>"$log_dir/mkvpropedit.stderr.log"
end_ns=$(perl -MTime::HiRes=time -e 'printf "%.0f\n", time() * 1000000000')
printf '%s\n' "$((end_ns - start_ns))" >"$log_dir/mkvpropedit.elapsed-ns.txt"

probe "$mkv_encode" "$log_dir/metadata-mkv-after.ffprobe.json"
structure "$log_dir/metadata-mkv-after.ffprobe.json" "$log_dir/metadata-mkv-after.structure.json"
payload_hashes "$mkv_encode" "$log_dir/metadata-mkv-after.hashes.txt"
test "$(jq -r '.format.tags.title' "$log_dir/metadata-mkv-after.ffprobe.json")" = "日本語 Title #01"
diff -u "$log_dir/metadata-mkv-before.structure.json" "$log_dir/metadata-mkv-after.structure.json" \
  >"$log_dir/metadata-mkv-structure.diff" || true
diff -u "$log_dir/metadata-mkv-before.hashes.txt" "$log_dir/metadata-mkv-after.hashes.txt" \
  >"$log_dir/metadata-mkv-hashes.diff" || true
mv "$mkv_encode" "$mkv_final"

mp4_encode="$artifact_dir/.chapterbrake-poc-mp4-encode.mp4"
mp4_metadata="$artifact_dir/.chapterbrake-poc-mp4-metadata.mp4"
mp4_final="$artifact_dir/日本語 Title #02.mp4"
rm -f "$mp4_encode" "$mp4_metadata" "$mp4_final"
cp "$artifact_dir/mp4-no-subtitles.mp4" "$mp4_encode"
probe "$mp4_encode" "$log_dir/metadata-mp4-before.ffprobe.json"
structure "$log_dir/metadata-mp4-before.ffprobe.json" "$log_dir/metadata-mp4-before.structure.json"
payload_hashes "$mp4_encode" "$log_dir/metadata-mp4-before.hashes.txt"

major_brand=$(jq -r '.format.tags.major_brand' "$log_dir/metadata-mp4-before.ffprobe.json")
ffmpeg -hide_banner -y -i "$mp4_encode" \
  -map 0:v -map "0:a?" -map_metadata 0 -map_chapters 0 -c copy \
  -metadata "title=日本語 Title #02" \
  -brand "$major_brand" \
  "$mp4_metadata" \
  >"$log_dir/metadata-ffmpeg.stdout.log" \
  2>"$log_dir/metadata-ffmpeg.stderr.log"

probe "$mp4_metadata" "$log_dir/metadata-mp4-after.ffprobe.json"
structure "$log_dir/metadata-mp4-after.ffprobe.json" "$log_dir/metadata-mp4-after.structure.json"
payload_hashes "$mp4_metadata" "$log_dir/metadata-mp4-after.hashes.txt"
test "$(jq -r '.format.tags.title' "$log_dir/metadata-mp4-after.ffprobe.json")" = "日本語 Title #02"
diff -u "$log_dir/metadata-mp4-before.structure.json" "$log_dir/metadata-mp4-after.structure.json" \
  >"$log_dir/metadata-mp4-structure.diff" || true
diff -u "$log_dir/metadata-mp4-before.hashes.txt" "$log_dir/metadata-mp4-after.hashes.txt" \
  >"$log_dir/metadata-mp4-hashes.diff" || true
mv "$mp4_metadata" "$mp4_final"
rm -f "$mp4_encode"

printf 'mkv_title=%s\n' "$(jq -r '.format.tags.title' "$log_dir/metadata-mkv-after.ffprobe.json")"
printf 'mp4_title=%s\n' "$(jq -r '.format.tags.title' "$log_dir/metadata-mp4-after.ffprobe.json")"
printf 'mkv_elapsed_ns=%s\n' "$(cat "$log_dir/mkvpropedit.elapsed-ns.txt")"
