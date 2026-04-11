#!/usr/bin/env python3
"""
Real-time X11 cursor type detection using XFixes.
Outputs cursor type whenever it changes. Detects actual cursor shape
by analyzing cursor pixel data (not unreliable WM_CLASS heuristics).
"""
import ctypes
import sys
import time

try:
    x11 = ctypes.CDLL('libX11.so.6')
    xfixes = ctypes.CDLL('libXfixes.so.3')
except OSError:
    while True:
        print("default", flush=True)
        time.sleep(1)

class XFixesCursorImage(ctypes.Structure):
    _fields_ = [
        ('x', ctypes.c_short),
        ('y', ctypes.c_short),
        ('width', ctypes.c_ushort),
        ('height', ctypes.c_ushort),
        ('xhot', ctypes.c_ushort),
        ('yhot', ctypes.c_ushort),
        ('cursor_serial', ctypes.c_ulong),
        ('pixels', ctypes.c_void_p),
        ('atom', ctypes.c_ulong),
        ('name', ctypes.c_char_p),
    ]

x11.XOpenDisplay.restype = ctypes.c_void_p
x11.XFree.argtypes = [ctypes.c_void_p]
xfixes.XFixesGetCursorImage.restype = ctypes.POINTER(XFixesCursorImage)

dpy = x11.XOpenDisplay(None)
if not dpy:
    sys.exit(1)


def classify_cursor(img_ptr):
    """Classify cursor type by analyzing its pixel shape."""
    c = img_ptr.contents
    w, h = c.width, c.height
    xhot, yhot = c.xhot, c.yhot
    name = c.name.decode('utf-8', errors='ignore') if c.name else ''

    # If cursor has an explicit name, use it
    if name:
        nl = name.lower()
        if nl in ('xterm', 'ibeam', 'text'):
            return 'text'
        elif nl in ('hand1', 'hand2', 'pointer', 'pointing_hand', 'link'):
            return 'pointer'
        elif nl in ('crosshair', 'cross', 'tcross'):
            return 'crosshair'
        elif nl in ('fleur', 'move', 'all_scroll', 'grab', 'grabbing'):
            return 'move'
        elif nl in ('watch', 'wait', 'progress', 'left_ptr_watch'):
            return 'wait'
        elif nl in ('left_ptr', 'arrow', 'default', 'top_left_arrow'):
            return 'default'
        elif 'resize' in nl or 'size' in nl:
            return 'move'

    # No name — analyze pixel data
    if w == 0 or h == 0:
        return 'default'

    pixel_count = w * h
    pixels = ctypes.cast(c.pixels, ctypes.POINTER(ctypes.c_ulong * pixel_count))

    # Count opaque pixels per column and row
    col_counts = [0] * w
    row_counts = [0] * h
    total_opaque = 0

    for y in range(h):
        for x in range(w):
            pixel = pixels.contents[y * w + x]
            alpha = (pixel >> 24) & 0xFF
            if alpha > 50:
                col_counts[x] += 1
                row_counts[y] += 1
                total_opaque += 1

    if total_opaque == 0:
        return 'default'

    # Find bounding box of opaque pixels
    min_col = next((i for i in range(w) if col_counts[i] > 0), 0)
    max_col = next((i for i in range(w - 1, -1, -1) if col_counts[i] > 0), w - 1)
    min_row = next((i for i in range(h) if row_counts[i] > 0), 0)
    max_row = next((i for i in range(h - 1, -1, -1) if row_counts[i] > 0), h - 1)

    bbox_w = max_col - min_col + 1
    bbox_h = max_row - min_row + 1

    if bbox_w == 0 or bbox_h == 0:
        return 'default'

    aspect = bbox_w / bbox_h

    # Density analysis
    left_half = sum(col_counts[:w // 2])
    right_half = sum(col_counts[w // 2:])
    top_half = sum(row_counts[:h // 2])
    bottom_half = sum(row_counts[h // 2:])

    # Maximum column density (how many rows have pixels in the densest column)
    max_col_density = max(col_counts)
    # How many columns have significant pixels (> 20% of max)
    threshold = max(1, max_col_density * 0.2)
    active_cols = sum(1 for c in col_counts if c > threshold)

    # ─── TEXT / I-BEAM CURSOR ───
    # I-beam: narrow vertical shape. Few active columns, tall.
    # Pattern: serifs top/bottom (wider), thin stem in middle
    if bbox_h > bbox_w * 1.2 and active_cols <= bbox_w * 0.7:
        # Check for I-beam pattern: middle rows have fewer active columns than top/bottom
        top_quarter_width = sum(1 for x in range(w) if col_counts[x] > 0 and any(
            ((pixels.contents[y * w + x] >> 24) & 0xFF) > 50
            for y in range(min_row, min_row + bbox_h // 4)
        ))
        mid_width = sum(1 for x in range(w) if col_counts[x] > 0 and any(
            ((pixels.contents[y * w + x] >> 24) & 0xFF) > 50
            for y in range(min_row + bbox_h // 3, min_row + 2 * bbox_h // 3)
        ))
        if mid_width < top_quarter_width * 0.8:
            return 'text'
        # Still narrow and tall = likely text cursor
        if aspect < 0.5:
            return 'text'

    # ─── CROSSHAIR ───
    # Cross shape: thin lines both horizontal and vertical through center
    # Very few total opaque pixels, spread in + pattern
    center_col = w // 2
    center_row = h // 2
    has_vertical_line = col_counts[center_col] > h * 0.5 if center_col < w else False
    has_horizontal_line = row_counts[center_row] > w * 0.5 if center_row < h else False
    if has_vertical_line and has_horizontal_line and total_opaque < pixel_count * 0.15:
        return 'crosshair'

    # ─── DEFAULT ARROW ───
    # Arrow: triangular shape, pixels concentrated in top-left
    # Hotspot at top-left, more pixels on left and top
    if top_half > bottom_half * 1.3 and left_half > right_half * 1.3:
        return 'default'

    # Arrow but for right-handed cursors (most common)
    if xhot < w * 0.35 and yhot < h * 0.2:
        return 'default'

    # ─── POINTER / HAND ───
    # Hand cursor: hotspot near top, pixels more spread out
    if yhot < h * 0.3 and top_half > bottom_half and aspect > 0.5 and aspect < 1.5:
        return 'pointer'

    # ─── MOVE ───
    # Move/drag: symmetric shape, hotspot at center, like a 4-arrow
    cx_err = abs(xhot - w // 2)
    cy_err = abs(yhot - h // 2)
    lr_balance = min(left_half, right_half) / max(left_half, right_half) if max(left_half, right_half) > 0 else 0
    tb_balance = min(top_half, bottom_half) / max(top_half, bottom_half) if max(top_half, bottom_half) > 0 else 0
    if cx_err <= 2 and cy_err <= 2 and lr_balance > 0.7 and tb_balance > 0.7:
        return 'move'

    # ─── WAIT ───
    # Hourglass/spinner: roughly square, centered
    if aspect > 0.7 and aspect < 1.3 and cx_err <= 3 and cy_err <= 3:
        if total_opaque > pixel_count * 0.3:
            return 'wait'

    return 'default'


# Track cursor serial to detect changes
last_serial = -1
last_type = ''

# Poll at ~10Hz
while True:
    try:
        img = xfixes.XFixesGetCursorImage(dpy)
        if img:
            serial = img.contents.cursor_serial
            if serial != last_serial:
                last_serial = serial
                cursor_type = classify_cursor(img)
                if cursor_type != last_type:
                    last_type = cursor_type
                    print(cursor_type, flush=True)
            x11.XFree(img)
        time.sleep(0.1)
    except Exception:
        time.sleep(0.5)
