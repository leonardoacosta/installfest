#!/usr/bin/env python3
"""
zsa-globe-key-patch.py — reapply the real Apple Globe/Fn key patch to a
freshly-fetched Oryx layout (Br7g0/keymap.c + rules.mk).

Why this exists: zsa-firmware-build.sh does `rm -rf` + refetch of the
whole layout dir from Oryx on every new revision, which would silently
wipe this manual patch (Oryx itself has no "send Consumer Usage 0x29D"
key option -- ZSA's own blog confirms QMK has no native Fn/Globe key
support at all, see blog.zsa.io/where-fn-key). Full history/rationale:
zsa-voyager-keymap commit 56f780b.

As long as the ORYX-SIDE config for the patched key isn't touched again
(it still says "tap-dance -> F13" in Oryx's own cloud UI -- this patch
only affects the locally-fetched files, not what Oryx itself thinks
that key does), future fetches keep regenerating the same tap-dance
boilerplate for that key, making it a stable match target.

Idempotent: no-ops if the patch is already applied. Fails LOUDLY
(exit 1, not a silent skip) if neither the "already patched" nor the
"raw Oryx tap-dance" markers are found -- that means the Oryx-side key
config changed in some way this script doesn't know how to handle, and
a human needs to look (most likely: Leo edited that key again in Oryx).

Usage: zsa-globe-key-patch.py <layout_dir>
  (operates on <layout_dir>/keymap.c and <layout_dir>/rules.mk)
"""
import re
import sys
from pathlib import Path

ENUM_INSERT = """  // Sends the real Apple Globe/Fn key via Consumer Usage 0x029D
  // (AC Keyboard Layout Select, per Apple's Accessory Design Guidelines) --
  // NOT a spoofed Apple vendor/product ID, just the standard USB HID
  // consumer usage macOS treats as the Globe key. Requires
  // KEYBOARD_SHARED_EP = yes in rules.mk (see process_record_user below).
  // Auto-reapplied by scripts/hooks/zsa-globe-key-patch.py after every
  // Oryx fetch -- see zsa-voyager-keymap commit 56f780b for full history.
  CUSTOM_GLOBE,
};"""

PROCESS_RECORD_INSERT = """    case CUSTOM_GLOBE:
      // Momentary consumer-page report, same pattern QMK uses for other
      // consumer keys (e.g. KC_MEDIA_PLAY_PAUSE) -- press sends the usage,
      // release clears it (0). No register_code/unregister_code exists for
      // consumer usages; host_consumer_send(0) is the "key up" equivalent.
      if (record->event.pressed) {
        host_consumer_send(AC_NEXT_KEYBOARD_LAYOUT_SELECT);
      } else {
        host_consumer_send(0);
      }
      return false;
    case RGB_SLD:"""

RULES_MK_INSERT = (
    "# Real Apple Globe/Fn key via Consumer Usage (CUSTOM_GLOBE in keymap.c) --\n"
    "# needed to send it as a modifier-capable report over the shared HID\n"
    "# endpoint. Auto-reapplied by scripts/hooks/zsa-globe-key-patch.py.\n"
    "KEYBOARD_SHARED_EP = yes\n"
)


def patch_keymap(path: Path) -> bool:
    text = path.read_text()

    if "CUSTOM_GLOBE" in text:
        print(f"  {path.name}: already patched, skipping")
        return True

    # --- Detect the raw Oryx tap-dance markers (regex, tolerant of minor
    # whitespace drift between fetches) ---------------------------------
    enum_re = re.compile(r"(enum custom_keycodes \{[^}]*?\n)\};", re.DOTALL)
    tapdance_enum_re = re.compile(r"\n*enum tap_dance_codes \{\s*DANCE_0,\s*\};\n*")
    layout_re = re.compile(r"TD\(DANCE_0\)\s*,")
    tapdance_block_re = re.compile(
        r"typedef struct \{\s*bool is_press_action;.*?"
        r"tap_dance_action_t tap_dance_actions\[\] = \{.*?\};\n*",
        re.DOTALL,
    )
    process_record_re = re.compile(r"(    case RGB_SLD:)")

    enum_m = enum_re.search(text)
    tapdance_block_m = tapdance_block_re.search(text)
    if not enum_m or "TD(DANCE_0)" not in text or not tapdance_block_m:
        print(
            f"  ERR: {path.name} has neither the Globe-key patch nor the "
            f"expected raw Oryx tap-dance markers -- the Oryx-side key "
            f"config for this key may have changed. Manual review needed.",
            file=sys.stderr,
        )
        return False

    # 1. Add the CUSTOM_GLOBE enum entry (before the closing '};').
    text = enum_re.sub(lambda m: m.group(1) + ENUM_INSERT, text, count=1)

    # 2. Drop the now-unused tap_dance_codes enum.
    text = tapdance_enum_re.sub("\n\n", text, count=1)

    # 3. Repoint the layout cell.
    text = layout_re.sub("CUSTOM_GLOBE,", text, count=1)

    # 4. Strip the tap-dance implementation block.
    text = tapdance_block_re.sub("", text, count=1)

    # 5. Insert the CUSTOM_GLOBE case into process_record_user.
    if not process_record_re.search(text):
        print(f"  ERR: {path.name}: process_record_user marker not found", file=sys.stderr)
        return False
    text = process_record_re.sub(PROCESS_RECORD_INSERT, text, count=1)

    path.write_text(text)
    print(f"  {path.name}: patched (TD(DANCE_0)/tap-dance -> CUSTOM_GLOBE)")
    return True


def patch_rules_mk(path: Path) -> bool:
    text = path.read_text()

    if "KEYBOARD_SHARED_EP" in text:
        print(f"  {path.name}: already patched, skipping")
        return True

    if "TAP_DANCE_ENABLE = yes" not in text:
        print(
            f"  ERR: {path.name}: TAP_DANCE_ENABLE marker not found -- "
            f"Oryx-side rules.mk may have changed. Manual review needed.",
            file=sys.stderr,
        )
        return False

    text = text.replace("TAP_DANCE_ENABLE = yes\n", RULES_MK_INSERT, 1)
    path.write_text(text)
    print(f"  {path.name}: patched (TAP_DANCE_ENABLE -> KEYBOARD_SHARED_EP)")
    return True


def main() -> int:
    if len(sys.argv) != 2:
        print("usage: zsa-globe-key-patch.py <layout_dir>", file=sys.stderr)
        return 2

    layout_dir = Path(sys.argv[1])
    keymap = layout_dir / "keymap.c"
    rules = layout_dir / "rules.mk"

    if not keymap.is_file() or not rules.is_file():
        print(f"ERR: {keymap} or {rules} not found", file=sys.stderr)
        return 1

    ok = patch_keymap(keymap) and patch_rules_mk(rules)
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())
