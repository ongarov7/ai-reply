#!/usr/bin/env python3
"""Assemble genuine portrait recordings with captions below the recorded UI.

Requires ffmpeg (libx264) and Pillow. The manifest contains an ordered ``clips``
array. Every clip has ``path``, ``seconds`` and a ``captions`` dictionary keyed
by en/ru/kk/uz; ``start`` defaults to zero. ``capture_wall_seconds`` is required
only when a static VFR recording ends before its measured capture wall time.
Relative clip paths resolve beside the manifest. No screen, permission outcome
or localized system label is created or replaced by this tool.

Example manifest:
{"clips": [{"path": "settings.mov", "start": 1, "seconds": 6,
            "captions": {"en": "Open Keyboards, then enable AI Reply."}}]}
"""

from __future__ import annotations

import argparse
import hashlib
import io
import json
import math
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile

WIDTH = 720
STRIP_HEIGHT = 220
LANGUAGES = ("en", "ru", "kk", "uz")


def run(command: list[str]) -> subprocess.CompletedProcess[str]:
    result = subprocess.run(command, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    if result.returncode:
        raise RuntimeError(result.stderr.strip() or "Command failed")
    return result


def inspect(ffmpeg: str, path: Path) -> dict[str, object]:
    report = subprocess.run([ffmpeg, "-hide_banner", "-i", str(path)], text=True,
                            stdout=subprocess.PIPE, stderr=subprocess.PIPE).stderr
    duration = re.search(r"Duration: (\d+):(\d+):(\d+(?:\.\d+)?)", report)
    video = next((line for line in report.splitlines() if "Video:" in line), "")
    dimensions = re.search(r"\b(\d{2,5})x(\d{2,5})\b", video)
    if not duration or not dimensions:
        raise ValueError(f"Cannot inspect recording: {path}\n{report}")
    return {"seconds": int(duration[1]) * 3600 + int(duration[2]) * 60 + float(duration[3]),
            "width": int(dimensions[1]), "height": int(dimensions[2]), "video": video.strip(),
            "audio_streams": sum("Audio:" in line for line in report.splitlines()),
            "subtitle_streams": sum("Subtitle:" in line for line in report.splitlines())}


def finite_number(value: object, label: str, *, positive: bool = False) -> float:
    number = float(value)
    if not math.isfinite(number) or (number <= 0 if positive else number < 0):
        raise ValueError(f"{label} must be finite and {'positive' if positive else 'nonnegative'}")
    return number


def caption_image(text: str, font_path: Path, output: Path) -> None:
    from PIL import Image, ImageDraw, ImageFont

    picture = Image.new("RGB", (WIDTH, STRIP_HEIGHT), "#111827")
    draw = ImageDraw.Draw(picture)
    # No truncation: reject copy that cannot fit the separate caption strip.
    for font_size in (30, 28, 26, 24):
        font = ImageFont.truetype(str(font_path), font_size)
        lines = []
        for paragraph in text.splitlines() or [""]:
            line = ""
            for word in paragraph.split():
                candidate = f"{line} {word}".strip()
                if draw.textlength(candidate, font=font) > WIDTH - 64:
                    if not line:
                        raise ValueError("Caption contains a word too wide for the strip")
                    lines.append(line)
                    line = word
                else:
                    line = candidate
            lines.append(line)
        wrapped = "\n".join(lines)
        bounds = draw.multiline_textbbox((0, 0), wrapped, font=font, spacing=7, align="center")
        if bounds[3] - bounds[1] <= STRIP_HEIGHT - 36:
            x = WIDTH / 2
            y = (STRIP_HEIGHT - (bounds[3] - bounds[1])) / 2 - bounds[1]
            draw.multiline_text((x, y), wrapped, font=font, fill="white", spacing=7,
                                align="center", anchor="ma")
            picture.save(output)
            return
    raise ValueError("Caption does not fit the strip; shorten it")


def contact_sheet(ffmpeg: str, movie: Path, times: list[float], destination: Path) -> None:
    from PIL import Image

    thumbs = []
    for index, time in enumerate(times):
        data = subprocess.run([ffmpeg, "-hide_banner", "-loglevel", "error", "-ss", str(time),
                               "-i", str(movie), "-frames:v", "1", "-vf", "scale=240:-2",
                               "-f", "image2pipe", "-vcodec", "png", "-"],
                              stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        if data.returncode:
            raise RuntimeError(data.stderr.decode(errors="replace"))
        thumbs.append(Image.open(io.BytesIO(data.stdout)).convert("RGB"))
    columns = min(3, len(thumbs))
    rows = math.ceil(len(thumbs) / columns)
    cell_width, cell_height = thumbs[0].size
    sheet = Image.new("RGB", (columns * (cell_width + 10) + 10,
                              rows * (cell_height + 10) + 10), "#111827")
    for index, thumb in enumerate(thumbs):
        sheet.paste(thumb, (10 + (index % columns) * (cell_width + 10),
                           10 + (index // columns) * (cell_height + 10)))
    destination.parent.mkdir(parents=True, exist_ok=True)
    sheet.save(destination)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--manifest", type=Path, required=True)
    parser.add_argument("--language", choices=LANGUAGES, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--contact", type=Path, required=True)
    parser.add_argument("--evidence", type=Path, required=True)
    parser.add_argument("--ffmpeg", default=os.environ.get("FFMPEG") or shutil.which("ffmpeg"))
    parser.add_argument("--font", type=Path,
                        default=Path("/System/Library/Fonts/Supplemental/Arial Unicode.ttf"))
    args = parser.parse_args()
    if not args.ffmpeg:
        try:
            import imageio_ffmpeg
            args.ffmpeg = imageio_ffmpeg.get_ffmpeg_exe()
        except ImportError:
            parser.error("Supply --ffmpeg or install imageio-ffmpeg")
    ffmpeg = str(Path(args.ffmpeg).resolve())
    manifest = args.manifest.resolve()
    clips = json.loads(manifest.read_text(encoding="utf-8"))["clips"]
    if not clips or not args.font.is_file():
        parser.error("An ordered nonempty clips list and a Unicode font are required")
    sources, total, evidence = [], 0.0, []
    aspect = None
    for clip in clips:
        path = Path(clip["path"])
        path = (manifest.parent / path).resolve() if not path.is_absolute() else path.resolve()
        if path == args.output.resolve():
            raise ValueError("Keep raw footage separate from the encoded asset")
        metadata = inspect(ffmpeg, path)
        start = finite_number(clip.get("start", 0), "start")
        seconds = finite_number(clip["seconds"], "seconds", positive=True)
        wall = finite_number(clip.get("capture_wall_seconds", metadata["seconds"]), "capture_wall_seconds")
        if start >= float(metadata["seconds"]):
            raise ValueError(f"Clip starts after its last recorded frame: {path}")
        if start + seconds > max(float(metadata["seconds"]), wall) + 0.05:
            raise ValueError(f"Requested range exceeds the genuine recording/capture wall time: {path}")
        ratio = int(metadata["height"]) / int(metadata["width"])
        if ratio <= 1 or (aspect is not None and abs(ratio - aspect) > .001):
            raise ValueError("All recordings must share the same portrait aspect ratio")
        aspect = ratio
        text = clip["captions"][args.language].strip()
        if not text:
            raise ValueError("Every clip requires a nonempty localized caption")
        held = start + seconds > float(metadata["seconds"]) + 0.05
        sources.append((path, start, seconds, text))
        evidence.append({"path": str(path), "source_sha256": hashlib.sha256(path.read_bytes()).hexdigest(),
                         "start": start, "seconds": seconds, "capture_wall_seconds": wall,
                         "held_static_final_frame": held, "caption": text, "source": metadata})
        total += seconds
    if total < 20 or total > 90:
        raise ValueError(f"Expected a short 20–90 second tutorial, got {total}")
    screen_height = math.floor(WIDTH * aspect / 2) * 2
    args.output.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="keyboard-tutorial-") as directory:
        work = Path(directory)
        normalized = []
        for index, (path, start, seconds, text) in enumerate(sources):
            strip = work / f"caption-{index}.png"
            caption_image(text, args.font, strip)
            target = work / f"clip-{index}.mp4"
            # Normalize VFR before trimming: seeking into a static interval can
            # otherwise begin at the next changed screen and shift later taps.
            filters = (f"[0:v]fps=30,trim=start={start}:duration={seconds},setpts=PTS-STARTPTS,"
                       f"tpad=stop_mode=clone:stop_duration={seconds},"
                       f"scale={WIDTH}:{screen_height}:flags=lanczos,setsar=1,"
                       f"pad=iw:ih+{STRIP_HEIGHT}:0:0:color=0x111827[base];"
                       f"[base][1:v]overlay=0:{screen_height}:eof_action=repeat,format=yuv420p[out]")
            run([ffmpeg, "-hide_banner", "-loglevel", "warning", "-y", "-i", str(path),
                 "-loop", "1", "-i", str(strip), "-filter_complex", filters, "-map", "[out]",
                 "-map_metadata", "-1", "-t", str(seconds), "-r", "30", "-fps_mode", "cfr",
                 "-c:v", "libx264", "-threads", "2", "-preset", "slow", "-crf", "25",
                 "-maxrate", "800k", "-bufsize", "1600k", "-pix_fmt", "yuv420p", "-an", "-sn", str(target)])
            normalized.append(target)
        listing = work / "concat.txt"
        listing.write_text("".join(f"file '{path.name}'\n" for path in normalized), encoding="utf-8")
        encoded = work / "keyboard-tutorial.mp4"
        run([ffmpeg, "-hide_banner", "-loglevel", "warning", "-y", "-f", "concat", "-safe", "0",
             "-i", str(listing), "-map", "0:v:0", "-c", "copy", "-an", "-sn", "-map_metadata", "-1",
             "-movflags", "+faststart", str(encoded)])
        final = inspect(ffmpeg, encoded)
        if (abs(float(final["seconds"]) - total) > .1 or final["audio_streams"] or final["subtitle_streams"]
                or "h264" not in str(final["video"]) or final["width"] != WIDTH
                or final["height"] != screen_height + STRIP_HEIGHT):
            raise RuntimeError(f"Output verification failed: {final}")
        # Fully decode before delivering an asset. No success from metadata alone.
        run([ffmpeg, "-v", "error", "-i", str(encoded), "-map", "0:v:0", "-f", "null", "-"])
        times, cursor = [], 0.0
        for _, _, seconds, _ in sources:
            times.append(cursor + seconds / 2)
            cursor += seconds
        sheet = work / "contact.png"
        contact_sheet(ffmpeg, encoded, times, sheet)
        args.contact.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(encoded, args.output)
        shutil.copyfile(sheet, args.contact)
    report = {"language": args.language, "output": str(args.output.resolve()), "bytes": args.output.stat().st_size,
              "sha256": hashlib.sha256(args.output.read_bytes()).hexdigest(),
              "contact": str(args.contact.resolve()), "clips": evidence, **final}
    args.evidence.parent.mkdir(parents=True, exist_ok=True)
    args.evidence.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(json.dumps({k: v for k, v in report.items() if k != "clips"}, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, RuntimeError, ValueError, KeyError, ImportError) as failure:
        print(f"Keyboard tutorial encoding failed: {failure}", file=sys.stderr)
        sys.exit(1)
