#!/bin/sh
set -eu

repo_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
artifact_dir="$repo_dir/poc/artifacts"
log_dir="$repo_dir/poc/logs"
result="$log_dir/chapter-boundary-analysis.tsv"

count_colors() {
  ffmpeg -v error -i "$1" -map 0:v:0 \
    -vf "scale=1:1,format=rgb24" -f rawvideo - \
    | od -An -tu1 -v \
    | awk '
      {
        for (i = 1; i <= NF; i++) {
          rgb[++component] = $i
          if (component == 3) {
            if (rgb[1] > 200 && rgb[2] < 50 && rgb[3] < 50) red++
            else if (rgb[1] < 50 && rgb[2] > 80 && rgb[3] < 50) green++
            else if (rgb[1] < 50 && rgb[2] < 50 && rgb[3] > 200) blue++
            else if (rgb[1] > 200 && rgb[2] > 200 && rgb[3] < 50) yellow++
            else other++
            component = 0
          }
        }
      }
      END {
        printf "%d\t%d\t%d\t%d\t%d", red, green, blue, yellow, other
      }'
}

printf 'file\tduration\tframes\tred\tgreen\tblue\tyellow\tother\n' >"$result"
for name in range-1-2 range-2 range-3-4
do
  file="$artifact_dir/$name.mkv"
  duration=$(ffprobe -v error -show_entries format=duration -of csv=p=0 "$file")
  frames=$(ffprobe -v error -count_frames -select_streams v:0 \
    -show_entries stream=nb_read_frames -of csv=p=0 "$file")
  printf '%s\t%s\t%s\t%s\n' "$name" "$duration" "$frames" "$(count_colors "$file")" \
    >>"$result"
done

cat "$result"
