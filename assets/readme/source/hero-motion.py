#!/usr/bin/env python3
"""Render an animated GIF hero from hero.svg by replaying the terminal session."""

from __future__ import annotations

import argparse
import math
import subprocess
import tempfile
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path

CHROME = "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
FPS = 30
MONO_ADVANCE = 9.6
TEXT_X = 656
KEY_COLOR = (255, 0, 255)
ALPHA_THRESHOLD = 128

COMMANDS = [
    ("axern run python:3.12-slim -- python app.py", 140, "hello from axern", 168, "#7d8290"),
    ("axern service create --file api.yaml --wait", 212, "service api ready", 240, "check"),
    ("axern service tunnel api --to 127.0.0.1:8080", 284, "forwarding 127.0.0.1:8080 → api:8080", 312, "#7d8290"),
]
FINAL_Y = 352


def cursor_xml(x: float, y: int, opacity: float = 1.0) -> str:
    return (
        f'<rect x="{x:.1f}" y="{y - 14}" width="10" height="19" '
        f'fill="#78a7ff" opacity="{opacity:.3f}"/>'
    )


def command_xml(typed: str, y: int, cursor: bool) -> str:
    xml = (
        f'<text x="{TEXT_X}" y="{y}">'
        f'<tspan fill="#78a7ff">&gt; </tspan>'
        f'<tspan fill="#e8eaf0">{typed}</tspan></text>'
    )
    if cursor:
        xml += cursor_xml(TEXT_X + MONO_ADVANCE * (2 + len(typed)) + 1, y)
    return xml


def output_xml(text: str, y: int, style: str, opacity: float) -> str:
    if style == "check":
        body = f'<tspan fill="#42a9c2">✓ </tspan><tspan fill="#7d8290">{text}</tspan>'
    else:
        body = f'<tspan fill="{style}">{text}</tspan>'
    return (
        f'<text x="{TEXT_X}" y="{y}" opacity="{opacity:.3f}">{body}</text>'
    )


def terminal_group(state: dict) -> str:
    session: list[str] = []
    for index, (command, cmd_y, output, out_y, style) in enumerate(COMMANDS):
        typed_count = state["typed"][index]
        is_active = state["active"] == index
        if typed_count > 0 or is_active:
            session.append(
                command_xml(command[:typed_count], cmd_y, cursor=is_active)
            )
        out_opacity = state["outputs"][index]
        if out_opacity > 0:
            session.append(output_xml(output, out_y, style, out_opacity))

    session_xml = "".join(session)
    if session_xml:
        session_xml = f'<g opacity="{state["session"]:.3f}">{session_xml}</g>'

    final_xml = ""
    if state["final"] > 0:
        final_xml = (
            f'<text x="{TEXT_X}" y="{FINAL_Y}" fill="#78a7ff" '
            f'opacity="{state["final"]:.3f}">&gt; </text>'
        )
        if state["cursor"]:
            final_xml += cursor_xml(
                TEXT_X + MONO_ADVANCE * 2 + 1, FINAL_Y, state["final"]
            )

    return f"""  <g id="terminal-proof">
    <rect x="632" y="56" width="504" height="308" rx="14" fill="#10131a" stroke="#262c3a"/>
    <text x="656" y="92" fill="#6d7180" font-family="ui-monospace, SFMono-Regular, Menlo, monospace" font-size="14">sandbox → service → tunnel</text>
    <line x1="632" y1="104" x2="1136" y2="104" stroke="#1e2330"/>
    <g font-family="ui-monospace, SFMono-Regular, Menlo, monospace" font-size="16">{session_xml}{final_xml}</g>
  </g>
</svg>
"""


def build_states() -> list[dict]:
    base = {"typed": [0, 0, 0], "outputs": [0.0, 0.0, 0.0], "active": -1,
            "session": 1.0, "final": 0.0, "cursor": False}
    states: list[dict] = []

    def emit(count: int, **updates) -> None:
        for _ in range(count):
            base.update(updates)
            states.append(dict(base, typed=list(base["typed"]),
                             outputs=list(base["outputs"])))

    emit(18)
    for index, (command, _, _, _, _) in enumerate(COMMANDS):
        frames = math.ceil(len(command) / 2)
        for step in range(1, frames + 1):
            emit(1, active=index,
                 typed=[*base["typed"][:index], min(len(command), step * 2),
                        *base["typed"][index + 1:]])
        emit(4, active=index)
        for fade in range(1, 7):
            emit(1, active=index,
                 outputs=[*base["outputs"][:index],
                          round(1 - (1 - fade / 6) ** 2, 3),
                          *base["outputs"][index + 1:]])
        emit(6 if index < 2 else 4, active=index)
    emit(4, active=-1)
    for fade in range(1, 5):
        emit(1, final=round(fade / 4, 3), cursor=fade >= 3)
    for beat in range(75):
        emit(1, cursor=(beat // 15) % 2 == 0)
    for fade in range(1, 13):
        emit(1, session=round(1 - fade / 12, 3),
             final=round(1 - fade / 12, 3), cursor=False)
    return states


def flatten_frames(frames_dir: Path, count: int) -> None:
    from PIL import Image
    for index in range(count):
        png_path = frames_dir / f"frame-{index:04d}.png"
        rgba = Image.open(png_path).convert("RGBA")
        mask = rgba.getchannel("A").point(
            lambda alpha: 255 if alpha >= ALPHA_THRESHOLD else 0
        )
        flat = Image.new("RGB", rgba.size, KEY_COLOR)
        flat.paste(rgba.convert("RGB"), mask=mask)
        flat.save(png_path)


def patch_gif_transparency(gif_path: Path, frame_count: int) -> None:
    from PIL import Image
    with Image.open(gif_path) as image:
        palette = image.getpalette()
        key_index = next(
            i for i in range(len(palette) // 3)
            if tuple(palette[i * 3: i * 3 + 3]) == KEY_COLOR
        )

    data = bytearray(gif_path.read_bytes())
    packed = data[10]
    cursor = 13 + (3 * (2 ** ((packed & 0x07) + 1)) if packed & 0x80 else 0)
    patched = 0
    while cursor < len(data) and data[cursor] != 0x3B:
        marker = data[cursor]
        if marker == 0x21 and data[cursor + 1] == 0xF9:
            data[cursor + 3] |= 0x01
            data[cursor + 6] = key_index
            patched += 1
        position = cursor + 2 if marker == 0x21 else cursor + 10
        if marker == 0x2C:
            image_packed = data[cursor + 9]
            if image_packed & 0x80:
                position += 3 * (2 ** ((image_packed & 0x07) + 1))
            position += 1
        while data[position] != 0:
            position += 1 + data[position]
        cursor = position + 1
    if patched != frame_count:
        raise SystemExit(f"patched {patched} frames, expected {frame_count}")
    gif_path.write_bytes(data)


def render_frame(chrome: str, svg_path: Path, png_path: Path) -> None:
    subprocess.run(
        [chrome, "--headless", "--disable-gpu", "--hide-scrollbars",
         "--default-background-color=00000000",
         f"--screenshot={png_path}", "--window-size=1200,420",
         f"file://{svg_path}"],
        check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
    )


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--svg", type=Path,
                        default=Path(__file__).resolve().parents[1] / "hero.svg")
    parser.add_argument("--out", type=Path,
                        default=Path(__file__).resolve().parents[1] / "hero.gif")
    parser.add_argument("--keep-frames", type=Path)
    args = parser.parse_args()

    source = args.svg.read_text(encoding="utf-8")
    marker = '  <g id="terminal-proof">'
    prefix, _, _ = source.partition(marker)
    if not prefix.endswith("\n") or "<svg" not in prefix:
        raise SystemExit("terminal-proof group not found in hero.svg")

    states = build_states()
    if args.keep_frames:
        workspace = args.keep_frames.resolve()
        workspace.mkdir(parents=True, exist_ok=True)
    else:
        workspace = Path(tempfile.mkdtemp(prefix="hero-motion-"))
    frames_dir = workspace / "frames"
    frames_dir.mkdir(exist_ok=True)

    svg_paths = []
    for index, state in enumerate(states):
        svg_path = frames_dir / f"frame-{index:04d}.svg"
        svg_path.write_text(prefix + terminal_group(state), encoding="utf-8")
        svg_paths.append(svg_path)

    with ThreadPoolExecutor(max_workers=6) as pool:
        list(pool.map(
            lambda pair: render_frame(
                CHROME, pair[0], pair[0].with_suffix(".png")),
            [(p,) for p in svg_paths],
        ))

    flatten_frames(frames_dir, len(states))

    palette = workspace / "palette.png"
    pattern = str(frames_dir / "frame-%04d.png")
    subprocess.run(
        ["ffmpeg", "-y", "-framerate", str(FPS), "-i", pattern,
         "-vf", "palettegen=stats_mode=diff:max_colors=256:reserve_transparent=0",
         str(palette)],
        check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
    )
    subprocess.run(
        ["ffmpeg", "-y", "-framerate", str(FPS), "-i", pattern, "-i", str(palette),
         "-lavfi", "paletteuse=dither=none:diff_mode=rectangle",
         "-gifflags", "+offsetting-transdiff", "-loop", "0", str(args.out)],
        check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
    )
    patch_gif_transparency(args.out, len(states))

    size_mb = args.out.stat().st_size / (1024 * 1024)
    print(f"GIF: {args.out}")
    print(f"Frames: {len(states)}, {FPS} FPS, {len(states) / FPS:.2f}s, {size_mb:.2f} MB")


if __name__ == "__main__":
    main()
