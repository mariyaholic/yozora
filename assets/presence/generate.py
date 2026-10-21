#!/usr/bin/env python3
"""Generate Yozora's original presence symbols using Pillow."""
from pathlib import Path

from PIL import Image, ImageDraw

SIZE = 512
SCALE = 4
RED = '#E62117'
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
    for symbol in ('youtube', 'youtube-music'):
        generate(symbol)
