#!/usr/bin/env python3
"""Generate Yozora's original presence symbols using Pillow."""
from pathlib import Path

from PIL import Image, ImageDraw

SIZE = 512
SCALE = 4
RED = '#E62117'
PURPLE = '#9146FF'
WHITE = '#FFFFFF'
OUTPUT = Path(__file__).resolve().parent


def coords(values):
    return tuple(round(value * SCALE) for value in values)


def generate(name):
    image = Image.new('RGBA', (SIZE * SCALE, SIZE * SCALE), (0, 0, 0, 0))
    draw = ImageDraw.Draw(image)
    if name == 'youtube':
        # A deliberately square tile with generous rounded corners.
        draw.rounded_rectangle(coords((48, 80, 464, 432)), radius=88 * SCALE, fill=RED)
        draw.polygon([coords(point) for point in ((214, 164), (214, 348), (354, 256))], fill=WHITE)
        shapes = f'<rect x="48" y="80" width="416" height="352" rx="88" fill="{RED}"/>\n  <path d="M214 164 L214 348 L354 256 Z" fill="{WHITE}"/>'
    elif name == 'twitch':
        # A live-broadcast mark: a dot with two pairs of signal arcs, not Twitch's own glyph.
        draw.rounded_rectangle(coords((48, 48, 464, 464)), radius=96 * SCALE, fill=PURPLE)
        draw.ellipse(coords((220, 220, 292, 292)), fill=WHITE)
        for inset, start, end in ((150, 135, 225), (150, -45, 45), (100, 135, 225), (100, -45, 45)):
            draw.arc(coords((inset, inset, 512 - inset, 512 - inset)), start, end, fill=WHITE, width=26 * SCALE)
        arcs = ''.join(
            f'<path d="M{256 - r * 0.7071:.1f} {256 - r * 0.7071:.1f} A{r} {r} 0 0 0 {256 - r * 0.7071:.1f} {256 + r * 0.7071:.1f}"/>'
            f'<path d="M{256 + r * 0.7071:.1f} {256 - r * 0.7071:.1f} A{r} {r} 0 0 1 {256 + r * 0.7071:.1f} {256 + r * 0.7071:.1f}"/>'
            for r in (93, 143))
        shapes = f'<rect x="48" y="48" width="416" height="416" rx="96" fill="{PURPLE}"/>\n  <circle cx="256" cy="256" r="36" fill="{WHITE}"/>\n  <g fill="none" stroke="{WHITE}" stroke-width="26" stroke-linecap="round">{arcs}</g>'
    else:
        # An eighth-note silhouette gives the music symbol its own identity.
        draw.ellipse(coords((40, 40, 472, 472)), fill=RED)
        draw.ellipse(coords((148, 290, 252, 370)), fill=WHITE)
        draw.rectangle(coords((228, 142, 252, 330)), fill=WHITE)
        draw.polygon([coords(point) for point in ((228, 142), (342, 180), (342, 248), (252, 218))], fill=WHITE)
        shapes = f'<circle cx="256" cy="256" r="216" fill="{RED}"/>\n  <g fill="{WHITE}">\n    <ellipse cx="200" cy="330" rx="52" ry="40"/>\n    <rect x="228" y="142" width="24" height="188"/>\n    <path d="M228 142 L342 180 L342 248 L252 218 Z"/>\n  </g>'
    image = image.resize((SIZE, SIZE), Image.Resampling.LANCZOS)
    image.save(OUTPUT / f'{name}.png', optimize=True)
    svg = f'<svg xmlns="http://www.w3.org/2000/svg" width="512" height="512" viewBox="0 0 512 512">\n  {shapes}\n</svg>\n'
    (OUTPUT / f'{name}.svg').write_text(svg, encoding='utf-8')


if __name__ == '__main__':
    for symbol in ('youtube', 'youtube-music', 'twitch'):
        generate(symbol)
