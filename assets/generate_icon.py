#!/usr/bin/env python3
"""
Generate macOS AppIcon.icns and master PNG icon for Jira Quick Access.
Uses high-resolution supersampling and Apple macOS Human Interface Guidelines (HIG) squircle geometry.
"""

import math
import os
import shutil
import subprocess
from PIL import Image, ImageDraw, ImageFilter

def create_superellipse_mask(size, radius, n=4.2):
    """Generates an accurate macOS squircle / superellipse mask with smooth antialiasing."""
    w, h = size
    mask = Image.new("L", (w, h), 0)
    # We render at supersampled resolution
    draw = ImageDraw.Draw(mask)
    
    # Apple icon squircle can be approximated using rounded rect or superellipse:
    # |(x-cx)/a|^n + |(y-cy)/b|^n <= 1
    # For performance and crispness at high resolution, we draw rounded rectangle with smooth corners
    draw.rounded_rectangle([(0, 0), (w - 1, h - 1)], radius=radius, fill=255)
    return mask

def generate_master_icon(size=1024):
    """Draws a 1024x1024 macOS app icon with supersampling (2048x2048 downscaled)."""
    scale = 2
    canvas_size = size * scale  # 2048x2048
    
    # Base transparent canvas
    img = Image.new("RGBA", (canvas_size, canvas_size), (0, 0, 0, 0))
    
    # Squircle dimensions inside canvas (macOS standard: ~824x824 at 1024 canvas, so inset is 100px)
    inset = 100 * scale
    squircle_size = canvas_size - (2 * inset)  # 1648x1648
    squircle_radius = int(185 * scale * (squircle_size / (824 * scale)))
    
    # 1. Drop shadow beneath squircle
    shadow_img = Image.new("RGBA", (canvas_size, canvas_size), (0, 0, 0, 0))
    shadow_draw = ImageDraw.Draw(shadow_img)
    shadow_offset_y = int(24 * scale)
    shadow_box = [
        inset,
        inset + shadow_offset_y,
        canvas_size - inset,
        canvas_size - inset + shadow_offset_y
    ]
    shadow_draw.rounded_rectangle(shadow_box, radius=squircle_radius, fill=(0, 0, 0, 140))
    # Soft ambient blur for shadow
    shadow_img = shadow_img.filter(ImageFilter.GaussianBlur(radius=22 * scale))
    img.alpha_composite(shadow_img)
    
    # 2. Squircle Background Tile
    tile = Image.new("RGBA", (squircle_size, squircle_size), (0, 0, 0, 0))
    
    # Draw deep radial / linear gradient (Dark Midnight Navy -> Royal Sapphire -> Electric Cobalt)
    for y in range(squircle_size):
        t = y / (squircle_size - 1)
        # Top: #0747A6 (7, 71, 166), Mid: #0052CC (0, 82, 204), Bottom: #091E42 (9, 30, 66)
        if t < 0.6:
            st = t / 0.6
            r = int(10 * (1 - st) + 0 * st)
            g = int(82 * (1 - st) + 90 * st)
            b = int(210 * (1 - st) + 240 * st)
        else:
            st = (t - 0.6) / 0.4
            r = int(0 * (1 - st) + 8 * st)
            g = int(90 * (1 - st) + 28 * st)
            b = int(240 * (1 - st) + 80 * st)
        
        # Subtle light beam across gradient
        line_img = Image.new("RGBA", (squircle_size, 1), (r, g, b, 255))
        tile.paste(line_img, (0, y))
    
    # Add subtle radial glow at upper center
    glow = Image.new("RGBA", (squircle_size, squircle_size), (0, 0, 0, 0))
    glow_draw = ImageDraw.Draw(glow)
    cx, cy = squircle_size // 2, int(squircle_size * 0.35)
    for rad in range(int(squircle_size * 0.7), 0, -8):
        alpha = int(45 * (1 - rad / (squircle_size * 0.7)))
        glow_draw.ellipse([cx - rad, cy - rad, cx + rad, cy + rad], fill=(0, 184, 217, alpha))
    tile = Image.alpha_composite(tile, glow)
    
    # 3. Draw Jira Quick Access Elements on the tile
    elements = Image.new("RGBA", (squircle_size, squircle_size), (0, 0, 0, 0))
    draw_elem = ImageDraw.Draw(elements)
    
    # Center coordinates of the squircle
    scx, scy = squircle_size // 2, squircle_size // 2
    
    # (A) Background Frosted Ticket Stack / Side Rail representation
    # Sliding frosted tab on the right side
    tab_w = int(220 * scale)
    tab_h = int(520 * scale)
    tab_x = squircle_size - tab_w - int(80 * scale)
    tab_y = scy - (tab_h // 2)
    draw_elem.rounded_rectangle(
        [tab_x, tab_y, tab_x + tab_w, tab_y + tab_h],
        radius=int(32 * scale),
        fill=(255, 255, 255, 30),
        outline=(255, 255, 255, 80),
        width=int(3 * scale)
    )
    # Mini indicator pills inside tab
    for i in range(3):
        py = tab_y + int(60 * scale) + i * int(140 * scale)
        colors = [(0, 184, 217, 220), (101, 84, 192, 220), (54, 179, 126, 220)]
        draw_elem.ellipse(
            [tab_x + int(30 * scale), py, tab_x + int(50 * scale), py + int(20 * scale)],
            fill=colors[i]
        )
        draw_elem.rounded_rectangle(
            [tab_x + int(65 * scale), py + int(4 * scale), tab_x + tab_w - int(30 * scale), py + int(16 * scale)],
            radius=int(6 * scale),
            fill=(255, 255, 255, 120)
        )
        
    # (B) Iconic Jira Diamond / Ribbon Symbol
    # Jira symbol consists of two dynamic overlapping curved shapes
    # Left chevron / diamond:
    left_sym = Image.new("RGBA", (squircle_size, squircle_size), (0, 0, 0, 0))
    left_draw = ImageDraw.Draw(left_sym)
    
    # Coordinate math for Jira diamond mark (shifted slightly left of center to balance the side rail)
    jx = scx - int(70 * scale)
    jy = scy
    unit = int(140 * scale)
    
    # Left polygon (upper-left to center-down)
    poly1 = [
        (jx - unit, jy - int(unit * 1.3)),
        (jx + int(unit * 0.1), jy - int(unit * 2.2)),
        (jx + int(unit * 0.8), jy - int(unit * 1.5)),
        (jx - int(unit * 0.3), jy - int(unit * 0.6)),
    ]
    # Smooth Jira shape
    # Bottom chevron:
    poly_bot = [
        (jx - int(unit * 1.4), jy - int(unit * 0.2)),
        (jx - int(unit * 0.5), jy - int(unit * 1.1)),
        (jx + int(unit * 0.6), jy),
        (jx - int(unit * 0.3), jy + int(unit * 0.9)),
    ]
    
    # Draw Jira main diamond ribbons with smooth gradients
    # Upper right ribbon (Bright Cyan / Electric Sky)
    poly_top = [
        (jx - int(unit * 0.2), jy - int(unit * 1.6)),
        (jx + int(unit * 1.2), jy - int(unit * 0.4)),
        (jx + int(unit * 0.4), jy + int(unit * 0.4)),
        (jx - int(unit * 0.6), jy - int(unit * 0.8)),
    ]
    
    # Draw Jira styled ribbons
    # Ribbon 1 (Left / Lower):
    ribbon1_pts = [
        (jx - int(unit * 1.5), jy - int(unit * 0.2)),
        (jx - int(unit * 0.3), jy - int(unit * 1.4)),
        (jx + int(unit * 0.5), jy - int(unit * 0.6)),
        (jx - int(unit * 0.7), jy + int(unit * 0.6)),
    ]
    
    # Ribbon 2 (Right / Upper):
    ribbon2_pts = [
        (jx - int(unit * 0.3), jy - int(unit * 1.4)),
        (jx + int(unit * 0.9), jy - int(unit * 0.2)),
        (jx - int(unit * 0.1), jy + int(unit * 1.0)),
        (jx - int(unit * 0.7), jy + int(unit * 0.6)),
    ]
    
    # Draw bottom shadow of the ribbons
    draw_elem.polygon(
        [(x + int(10*scale), y + int(15*scale)) for x,y in ribbon1_pts],
        fill=(0, 15, 45, 120)
    )
    draw_elem.polygon(
        [(x + int(10*scale), y + int(15*scale)) for x,y in ribbon2_pts],
        fill=(0, 15, 45, 140)
    )
    
    # Draw Ribbon 1 (Vibrant Blue to Cyan)
    draw_elem.polygon(ribbon1_pts, fill=(0, 101, 255, 240))
    # Draw Ribbon 2 (Electric Cyan / Aqua with glass sheen)
    draw_elem.polygon(ribbon2_pts, fill=(0, 199, 230, 245))
    
    # (C) Speed / Quick Access Lightning Emblem across Jira symbol
    bolt_pts = [
        (jx + int(unit * 0.5), jy - int(unit * 1.7)),   # Top tip
        (jx - int(unit * 0.3), jy - int(unit * 0.1)),   # Inner bend left
        (jx + int(unit * 0.2), jy - int(unit * 0.1)),   # Step right
        (jx - int(unit * 0.5), jy + int(unit * 1.6)),   # Bottom point
        (jx + int(unit * 0.4), jy + int(unit * 0.1)),   # Inner bend right
        (jx - int(unit * 0.1), jy + int(unit * 0.1)),   # Step left
    ]
    
    # Bolt glow
    bolt_glow = Image.new("RGBA", (squircle_size, squircle_size), (0, 0, 0, 0))
    bolt_draw = ImageDraw.Draw(bolt_glow)
    bolt_draw.polygon(bolt_pts, fill=(255, 255, 255, 255))
    bolt_glow = bolt_glow.filter(ImageFilter.GaussianBlur(radius=16 * scale))
    elements = Image.alpha_composite(elements, bolt_glow)
    
    # Bolt Shadow & Core
    draw_elem = ImageDraw.Draw(elements)
    draw_elem.polygon([(x+int(4*scale), y+int(8*scale)) for x,y in bolt_pts], fill=(0, 20, 60, 160))
    draw_elem.polygon(bolt_pts, fill=(255, 255, 255, 250))
    
    # Bolt inner gradient / highlight
    draw_elem.line([bolt_pts[0], bolt_pts[1]], fill=(255, 255, 255, 255), width=int(5*scale))
    draw_elem.line([bolt_pts[2], bolt_pts[3]], fill=(220, 250, 255, 255), width=int(5*scale))
    
    # Composite elements onto tile
    tile = Image.alpha_composite(tile, elements)
    
    # 4. Glass Edge Bevel / Inner Highlight Border
    border = Image.new("RGBA", (squircle_size, squircle_size), (0, 0, 0, 0))
    border_draw = ImageDraw.Draw(border)
    # Top highlight (crisp white semi-transparent)
    border_draw.rounded_rectangle(
        [int(2*scale), int(2*scale), squircle_size - int(2*scale), squircle_size - int(2*scale)],
        radius=squircle_radius,
        outline=(255, 255, 255, 60),
        width=int(2.5 * scale)
    )
    # Inner dark shadow rim at bottom
    border_draw.rounded_rectangle(
        [int(4*scale), int(4*scale), squircle_size - int(4*scale), squircle_size - int(4*scale)],
        radius=squircle_radius - int(2*scale),
        outline=(0, 0, 0, 40),
        width=int(1.5 * scale)
    )
    tile = Image.alpha_composite(tile, border)
    
    # 5. Mask Tile to Squircle
    mask = Image.new("L", (squircle_size, squircle_size), 0)
    mask_draw = ImageDraw.Draw(mask)
    mask_draw.rounded_rectangle(
        [0, 0, squircle_size - 1, squircle_size - 1],
        radius=squircle_radius,
        fill=255
    )
    
    # Composite squircle into main canvas
    img.paste(tile, (inset, inset), mask=mask)
    
    # 6. Downsample from 2048 to 1024 with high-quality Lanczos resampling
    final_img = img.resize((size, size), Image.Resampling.LANCZOS)
    return final_img

def build_iconset_and_icns(master_img, assets_dir):
    """Generates an .iconset directory and converts to AppIcon.icns using iconutil."""
    iconset_dir = os.path.join(assets_dir, "AppIcon.iconset")
    if os.path.exists(iconset_dir):
        shutil.rmtree(iconset_dir)
    os.makedirs(iconset_dir, exist_ok=True)
    
    icon_sizes = [
        (16, "icon_16x16.png"),
        (32, "icon_16x16@2x.png"),
        (32, "icon_32x32.png"),
        (64, "icon_32x32@2x.png"),
        (128, "icon_128x128.png"),
        (256, "icon_128x128@2x.png"),
        (256, "icon_256x256.png"),
        (512, "icon_256x256@2x.png"),
        (512, "icon_512x512.png"),
        (1024, "icon_512x512@2x.png"),
    ]
    
    for sz, filename in icon_sizes:
        resized = master_img.resize((sz, sz), Image.Resampling.LANCZOS)
        resized.save(os.path.join(iconset_dir, filename), "PNG")
        
    icns_path = os.path.join(assets_dir, "AppIcon.icns")
    subprocess.run(["iconutil", "-c", "icns", iconset_dir, "-o", icns_path], check=True)
    
    # Also save Windows .ico file
    ico_path = os.path.join(assets_dir, "icon.ico")
    master_img.save(ico_path, format="ICO", sizes=[(16,16), (32,32), (48,48), (64,64), (128,128), (256,256)])
    
    # Clean up temporary iconset directory
    shutil.rmtree(iconset_dir)
    print(f"✓ Successfully generated {icns_path} and {ico_path}")

def main():
    assets_dir = os.path.dirname(os.path.abspath(__file__))
    master_png_path = os.path.join(assets_dir, "icon.png")
    
    jira_variant = os.path.join(assets_dir, "icon_variant_jira_ribbons.png")
    glass_variant = os.path.join(assets_dir, "icon_variant_glass.png")
    
    if os.path.exists(jira_variant):
        icon = Image.open(jira_variant).convert("RGBA")
        icon.save(master_png_path, "PNG")
    elif os.path.exists(glass_variant):
        icon = Image.open(glass_variant).convert("RGBA")
        icon.save(master_png_path, "PNG")
    else:
        print("🎨 Rendering high-resolution macOS master icon (1024x1024)...")
        icon = generate_master_icon(1024)
        icon.save(master_png_path, "PNG")
        
    print(f"✓ Master icon: {master_png_path}")
    print("📦 Building Apple ICNS and Windows ICO bundles...")
    build_iconset_and_icns(icon, assets_dir)

if __name__ == "__main__":
    main()
