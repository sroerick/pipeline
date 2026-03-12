#!/usr/bin/env python3

from __future__ import annotations

import re
import sys
from dataclasses import dataclass
from pathlib import Path


WIKILINK_RE = re.compile(r"\[\[(?:[^|\]]+\|)?([^\]]+)\]\]")
BOLD_RE = re.compile(r"\*\*(.+?)\*\*")
LEADING_VERSE_RE = re.compile(r"^\*\*\d+\*\*\s*")


@dataclass
class QuoteEntry:
    title: str
    reference: str
    body_lines: list[str]
    book: str


def main() -> int:
    if len(sys.argv) != 2:
        print("usage: markdown_to_quotes_tex.py <Quotes.md>", file=sys.stderr)
        return 1

    source = Path(sys.argv[1])
    entries = parse_entries(source.read_text())
    if not entries:
        print("no [!bible] quote blocks found", file=sys.stderr)
        return 1

    sys.stdout.write(render_document(entries))
    return 0


def parse_entries(text: str) -> list[QuoteEntry]:
    lines = text.splitlines()
    entries: list[QuoteEntry] = []
    i = 0
    while i < len(lines):
        line = lines[i].rstrip()
        if not line.startswith("> [!bible] "):
            i += 1
            continue

        title = line[len("> [!bible] ") :].strip()
        i += 1
        reference = ""
        body_lines: list[str] = []
        while i < len(lines) and lines[i].startswith("> "):
            content = lines[i][2:].rstrip()
            if not reference and is_wikilink(content):
                reference = wikilink_label(content)
            elif content:
                body_lines.append(content)
            i += 1

        while i < len(lines) and not lines[i].strip():
            i += 1

        book = ""
        if i < len(lines) and is_wikilink(lines[i].strip()):
            book = wikilink_label(lines[i].strip())
            i += 1

        entries.append(
            QuoteEntry(
                title=title,
                reference=reference,
                body_lines=body_lines,
                book=book,
            )
        )
    return entries


def render_document(entries: list[QuoteEntry]) -> str:
    parts = [
        r"\documentclass[letterpaper]{article}",
        r"\usepackage[T1]{fontenc}",
        r"\usepackage[utf8]{inputenc}",
        r"\usepackage{ebgaramond}",
        r"\usepackage[margin=0.72in]{geometry}",
        r"\usepackage{microtype}",
        r"\usepackage[most]{tcolorbox}",
        r"\usepackage{pgfornament}",
        r"\usepackage{tikz}",
        r"\usepackage{eso-pic}",
        r"\usepackage{xcolor}",
        r"\usepackage{ragged2e}",
        r"\pagestyle{empty}",
        r"\setlength{\parindent}{0pt}",
        r"\definecolor{cardpaper}{HTML}{FCF8F1}",
        r"\definecolor{cardborder}{HTML}{D5C6B0}",
        r"\definecolor{cardink}{HTML}{33281F}",
        r"\definecolor{cardaccent}{HTML}{8A6D49}",
        r"\definecolor{cutguide}{HTML}{D8CCBB}",
        r"\newcommand{\cardcutguides}{%",
        r"  \begin{tikzpicture}[remember picture,overlay]",
        r"    \draw[color=cutguide,line width=0.45pt,dash pattern=on 2.5pt off 3.5pt]",
        r"      ([xshift=0.9in]current page.west |- current page.center) -- ([xshift=-0.9in]current page.east |- current page.center);",
        r"    \draw[color=cutguide,line width=0.45pt]",
        r"      ([xshift=0.72in]current page.west |- current page.center) ++(0,-0.22in) -- ++(0,0.44in);",
        r"    \draw[color=cutguide,line width=0.45pt]",
        r"      ([xshift=-0.72in]current page.east |- current page.center) ++(0,-0.22in) -- ++(0,0.44in);",
        r"  \end{tikzpicture}%",
        r"}",
        r"\AddToShipoutPictureBG*{\cardcutguides}",
        r"\tcbset{",
        r"  quotecard/.style={",
        r"    enhanced,",
        r"    breakable=false,",
        r"    colback=cardpaper,",
        r"    colframe=cardborder,",
        r"    boxrule=0.5pt,",
        r"    arc=3.2mm,",
        r"    left=8mm,",
        r"    right=8mm,",
        r"    top=7mm,",
        r"    bottom=7mm,",
        r"    width=6in,",
        r"    height=4in,",
        r"    valign=center,",
        r"  }",
        r"}",
        r"\begin{document}",
        "",
    ]
    for start in range(0, len(entries), 2):
        page_entries = entries[start : start + 2]
        parts.append(r"\vspace*{\fill}")
        parts.extend(render_entry(page_entries[0]))
        if len(page_entries) == 2:
            parts.append(r"\vspace{0.34in}")
            parts.extend(render_entry(page_entries[1]))
        parts.append(r"\vspace*{\fill}")
        if start + 2 < len(entries):
            parts.append(r"\newpage")
            parts.append("")
    parts.append(r"\end{document}")
    parts.append("")
    return "\n".join(parts)


def render_entry(entry: QuoteEntry) -> list[str]:
    title = inline_to_latex(display_book_label(entry))
    reference = inline_to_latex(entry.reference) if entry.reference else ""
    book = inline_to_latex(entry.book) if entry.book else ""
    rendered_lines = [inline_to_latex(strip_leading_verse_number(line)) for line in entry.body_lines if line.strip()]
    body = r" \\[0.55em] ".join(rendered_lines) if rendered_lines else ""
    footer = reference or book

    out = [
        r"\begin{center}",
        r"\begin{tcolorbox}[quotecard]",
        r"\begin{center}",
        rf"{{\fontsize{{12.5pt}}{{15pt}}\selectfont\scshape\color{{cardaccent}} {title}\par}}",
        r"\vspace{0.18em}",
        r"{\color{cardborder}\pgfornament[width=0.72cm]{88}\par}",
        r"\end{center}",
        r"\vspace{0.58em}",
        r"{\fontsize{16.5pt}{20.5pt}\selectfont\color{cardink}\justifying",
        body,
        r"\par}",
    ]
    if footer:
        out.extend(
            [
                r"\vfill",
                r"\begin{center}",
                rf"{{\large\itshape\color{{cardaccent}} {footer}\par}}",
                r"\end{center}",
            ]
        )
    out.extend(
        [
            r"\end{tcolorbox}",
            r"\end{center}",
            "",
        ]
    )
    return out


def inline_to_latex(text: str) -> str:
    text = replace_wikilinks(text)
    parts: list[str] = []
    last = 0
    for match in BOLD_RE.finditer(text):
        parts.append(escape_latex(text[last : match.start()]))
        parts.append(rf"\textbf{{{escape_latex(match.group(1))}}}")
        last = match.end()
    parts.append(escape_latex(text[last:]))
    return "".join(parts)


def replace_wikilinks(text: str) -> str:
    return WIKILINK_RE.sub(lambda match: match.group(1), text)


def strip_leading_verse_number(text: str) -> str:
    return LEADING_VERSE_RE.sub("", text)


def display_book_label(entry: QuoteEntry) -> str:
    if entry.book:
        return entry.book
    if entry.reference:
        return entry.reference.split(":", 1)[0].strip()
    if entry.title:
        return entry.title.split("(", 1)[0].strip()
    return ""


def is_wikilink(text: str) -> bool:
    return bool(WIKILINK_RE.fullmatch(text.strip()))


def wikilink_label(text: str) -> str:
    match = WIKILINK_RE.fullmatch(text.strip())
    if not match:
        return text.strip()
    return match.group(1)


def escape_latex(text: str) -> str:
    replacements = {
        "\\": r"\textbackslash{}",
        "&": r"\&",
        "%": r"\%",
        "$": r"\$",
        "#": r"\#",
        "_": r"\_",
        "{": r"\{",
        "}": r"\}",
        "~": r"\textasciitilde{}",
        "^": r"\textasciicircum{}",
    }
    return "".join(replacements.get(char, char) for char in text)


if __name__ == "__main__":
    raise SystemExit(main())
