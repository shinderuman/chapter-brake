#!/bin/sh
set -eu

repo_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
log_dir="$repo_dir/poc/logs"
result="$log_dir/audio-quality-verification.tsv"

verify_case() {
  name=$1
  expected_languages=$2
  mode=${3:-passthru}
  json="$log_dir/$name.ffprobe.json"
  stderr_log="$log_dir/$name.stderr.log"

  actual=$(jq -r '
    [.streams[] | select(.codec_type == "audio")
      | [.codec_name, (.channels | tostring), (.tags.language // "")] | join(":")]
    | join(",")
  ' "$json")

  if [ "$actual" != "$expected_languages" ]; then
    echo "$name: unexpected audio streams: $actual" >&2
    exit 1
  fi

  if rg -q "sanitizing .* bitrate" "$stderr_log"; then
    echo "$name: HandBrake changed a requested bitrate" >&2
    exit 1
  fi

  if [ "$mode" = passthru ]; then
    standard_count=$(rg -c "encoder: AAC \\(Apple AudioToolbox\\)" "$stderr_log")
    high_count=$(rg -c "AC3 Passthru" "$stderr_log")
    if [ "$standard_count" -ne "$high_count" ]; then
      echo "$name: high/standard output count mismatch" >&2
      exit 1
    fi
  else
    aac_count=$(rg -c "encoder: AAC \\(Apple AudioToolbox\\)" "$stderr_log")
    if [ "$aac_count" -ne 2 ]; then
      echo "$name: fallback did not produce two AAC outputs" >&2
      exit 1
    fi
    high_count=1
    standard_count=1
  fi

  printf '%s\t%s\t%s\t%s\n' \
    "$name" "$high_count" "$standard_count" "$actual" >>"$result"
}

printf 'case\thigh_count\tstandard_count\tstreams\n' >"$result"
verify_case audio-quality-track-1 'ac3:6:jpn,aac:2:jpn'
verify_case audio-quality-track-2 'ac3:6:eng,aac:2:eng'
verify_case audio-quality-tracks-1-2 \
  'ac3:6:jpn,aac:2:jpn,ac3:6:eng,aac:2:eng'
verify_case audio-quality-fallback 'aac:6:jpn,aac:2:jpn' fallback
verify_case audio-quality-mp4 'ac3:6:jpn,aac:2:jpn'

format_name=$(jq -r '.format.format_name' "$log_dir/audio-quality-mp4.ffprobe.json")
case "$format_name" in
  *mp4*) ;;
  *)
    echo "MP4 case: unexpected format $format_name" >&2
    exit 1
    ;;
esac

if rg -q "dropping track" "$log_dir/audio-quality-fallback.stderr.log"; then
  echo "fallback case: high-quality output was dropped" >&2
  exit 1
fi

audio_count=$(
  sed -n '/^JSON Title Set: /,$p' "$log_dir/source-one-audio-scan.stdout.log" \
    | sed '1s/^JSON Title Set: //' \
    | jq '.TitleList[0].AudioList | length'
)
if [ "$audio_count" -ne 1 ]; then
  echo "one-audio fixture: scan returned $audio_count tracks" >&2
  exit 1
fi

cat "$result"
