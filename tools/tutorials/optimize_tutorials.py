#!/usr/bin/env python3
"""Encode genuine 24-second tutorial footage with captions below the original UI.

The source recording must already show the application's requested language.
This tool never translates, replaces or paints over the recorded application UI.
Only the caption strip is added; the input's first 24 seconds retain their timing.
"""

from __future__ import annotations

import argparse
import json
import math
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile
import xml.etree.ElementTree as ET


LANGUAGES = ("en", "ru", "kk", "uz")
CAPTIONS = (
    (0, 4, "tutorial.paste.caption.setup"),
    (4, 7, "tutorial.paste.caption.settings"),
    (7, 14, "tutorial.paste.caption.allow"),
    (14, 24, "tutorial.paste.caption.try"),
)
WIDTH = 720
STRIP_HEIGHT = 200
DURATION = 24


def run(command: list[str], *, cwd: Path | None = None) -> subprocess.CompletedProcess[str]:
    result = subprocess.run(command, cwd=cwd, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    if result.returncode:
        raise RuntimeError(result.stderr.strip() or "Command failed")
    return result


def inspect(ffmpeg: str, path: Path) -> dict[str, object]:
    # ffmpeg's input report is also available when a separate ffprobe binary is not installed.
    result = subprocess.run([ffmpeg, "-hide_banner", "-i", str(path)], text=True,
                            stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    report = result.stderr
    duration = re.search(r"Duration: (\d+):(\d+):(\d+(?:\.\d+)?)", report)
    video = next((line for line in report.splitlines() if "Video:" in line), "")
    dimensions = re.search(r"\b(\d{2,5})x(\d{2,5})\b", video)
    if duration is None or dimensions is None:
        raise RuntimeError(f"Could not inspect video: {path}\n{report}")
    return {
        "seconds": int(duration[1]) * 3600 + int(duration[2]) * 60 + float(duration[3]),
        "width": int(dimensions[1]), "height": int(dimensions[2]),
        "video": video.strip(),
        "audio_streams": sum("Audio:" in line for line in report.splitlines()),
        "subtitle_streams": sum("Subtitle:" in line for line in report.splitlines()),
    }


def ass_time(seconds: int) -> str:
    return f"0:{seconds // 60:02d}:{seconds % 60:02d}.00"


def ass_text(text: str) -> str:
    # The catalogue supplies plain prose; prevent it being interpreted as ASS override markup.
    return text.replace("\\", "\\\\").replace("{", "\\{").replace("}", "\\}").replace("\n", r"\N")


def caption_script(catalogue: Path, language: str, video_height: int, platform: str) -> str:
    if platform == "ios":
        strings = json.loads(catalogue.read_text(encoding="utf-8"))["strings"]
    else:
        folder = "values" if language == "en" else f"values-{language}"
        resources = Path(__file__).resolve().parents[2] / "AI-Reply-Android/app/src/main/res" / folder
        strings = {node.attrib["name"]: node.text or "" for file in resources.glob("strings*.xml") for node in ET.parse(file).getroot().findall("string")}
    lines: list[str] = []
    for start, end, key in CAPTIONS:
        try:
            text = (strings[key]["localizations"][language]["stringUnit"]["value"] if platform == "ios" else
                    strings[key.replace("tutorial.paste.caption.", "tutorial_caption_")])
        except KeyError as error:
            raise ValueError(f"Missing {language} caption: {key}") from error
        if not text.strip():
            raise ValueError(f"Empty {language} caption: {key}")
        lines.append(f"Dialogue: 0,{ass_time(start)},{ass_time(end)},Caption,,0,0,0,,"
                     + r"{\an5\pos(360," + str(video_height + STRIP_HEIGHT // 2) + ")}"
                     + ass_text(text))
    return f"""[Script Info]
ScriptType: v4.00+
PlayResX: {WIDTH}
PlayResY: {video_height + STRIP_HEIGHT}
WrapStyle: 0
ScaledBorderAndShadow: yes

[V4+ Styles]
Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding
Style: Caption,Arial Unicode MS,28,&H00FFFFFF,&H00FFFFFF,&H00111827,&H00111827,0,0,0,0,100,100,0,0,1,0,0,5,40,40,20,1

[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
""" + "\n".join(lines) + "\n"


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--input", type=Path, required=True, help="Genuine recording already in the requested UI language")
    parser.add_argument("--language", choices=LANGUAGES, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--platform", choices=("ios", "android"), default="ios")
    parser.add_argument("--recorded-seconds", type=float, help="Verified capture wall time; permits holding the last genuine frame when a static VFR recording ends early")
    parser.add_argument("--contact", type=Path, help="Contact sheet destination (keep outside application resources)")
    parser.add_argument("--ffmpeg", default=os.environ.get("FFMPEG") or shutil.which("ffmpeg"))
    parser.add_argument("--font", type=Path, default=Path("/System/Library/Fonts/Supplemental/Arial Unicode.ttf"))
    parser.add_argument("--catalogue", type=Path, default=Path(__file__).resolve().parents[2] / "AI-Reply/AIReply/Resources/Localizable.xcstrings")
    args = parser.parse_args()
    if not args.ffmpeg:
        parser.error("Supply --ffmpeg or set FFMPEG to an ffmpeg binary with libass and libx264")
    ffmpeg = str(Path(args.ffmpeg).resolve())
    source, output = args.input.resolve(), args.output.resolve()
    if not source.is_file() or not args.font.is_file():
        parser.error("The recording and Unicode font must exist")
    if source == output:
        parser.error("Keep the raw recording separate from the encoded asset")
    metadata = inspect(ffmpeg, source)
    if args.recorded_seconds is not None and not math.isfinite(args.recorded_seconds):
        parser.error("Recorded wall time must be finite")
    holds_static_frame = float(metadata["seconds"]) < DURATION - 0.01
    if holds_static_frame and (args.recorded_seconds is None or args.recorded_seconds < DURATION):
        parser.error("Input timeline is shorter than 24 seconds. Normalize verified capture timing first, or supply --recorded-seconds for a static VFR capture that genuinely lasted at least 24 seconds")
    if int(metadata["width"]) >= int(metadata["height"]):
        parser.error("Expected portrait footage of the application")
    scaled_height = math.floor(int(metadata["height"]) * WIDTH / int(metadata["width"]) / 2) * 2
    output.parent.mkdir(parents=True, exist_ok=True)
    contact = args.contact.resolve() if args.contact else output.with_suffix(".contact.png")
    contact.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="tutorial-encode-") as directory:
        work = Path(directory)
        fonts = work / "fonts"
        fonts.mkdir()
        shutil.copyfile(args.font, fonts / "ArialUnicode.ttf")
        (work / "captions.ass").write_text(caption_script(args.catalogue, args.language, scaled_height, args.platform), encoding="utf-8")
        encoded = work / "encoded.mp4"
        # VFR recorders may emit nothing while the actual screen stays still.
        # Only an explicitly verified capture duration permits that final hold.
        hold = f"tpad=stop_mode=clone:stop_duration={DURATION}," if holds_static_frame else ""
        filters = (hold + f"scale={WIDTH}:{scaled_height}:flags=lanczos,setsar=1,fps=30,"
                   f"pad=iw:ih+{STRIP_HEIGHT}:0:0:color=0x111827,"
                   "ass=filename=captions.ass:fontsdir=fonts")
        run([ffmpeg, "-hide_banner", "-loglevel", "warning", "-y", "-i", str(source),
             "-map", "0:v:0", "-map_metadata", "-1", "-t", str(DURATION), "-vf", filters,
             "-r", "30", "-fps_mode", "cfr", "-c:v", "libx264", "-threads", "2", "-preset", "slow", "-crf", "25",
             "-maxrate", "600k", "-bufsize", "1200k", "-pix_fmt", "yuv420p",
             "-an", "-sn", "-movflags", "+faststart", str(encoded)], cwd=work)
        final = inspect(ffmpeg, encoded)
        if (abs(float(final["seconds"]) - DURATION) > 0.05 or final["audio_streams"] or
                final["subtitle_streams"] or "h264" not in str(final["video"]) or final["width"] != WIDTH):
            raise RuntimeError(f"Output verification failed: {final}")
        if encoded.stat().st_size >= 2_000_000:
            raise RuntimeError(f"Encoded tutorial exceeds the 2 MB budget: {encoded.stat().st_size} bytes")
        sheet = work / "contact.png"
        run([ffmpeg, "-hide_banner", "-loglevel", "warning", "-y", "-i", str(encoded),
             "-vf", "select='eq(n,60)+eq(n,165)+eq(n,315)+eq(n,570)',scale=360:-2,tile=2x2:padding=10:margin=10:color=0x111827",
             "-frames:v", "1", "-update", "1", str(sheet)])
        # All validation completes before replacing an existing bundled asset.
        shutil.copyfile(encoded, output)
        shutil.copyfile(sheet, contact)
    print(json.dumps({"language": args.language, "output": str(output), "contact": str(contact),
                      "bytes": output.stat().st_size, "capture_wall_seconds": args.recorded_seconds, "held_static_final_frame": holds_static_frame, **final}, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, RuntimeError, ValueError) as failure:
        print(f"Tutorial encoding failed: {failure}", file=sys.stderr)
        sys.exit(1)
