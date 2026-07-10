"""Module entrypoint: ``python3 -m cc_tmux`` -> the CLI."""

from __future__ import annotations

import sys

from .cli import main

if __name__ == "__main__":
    sys.exit(main())
