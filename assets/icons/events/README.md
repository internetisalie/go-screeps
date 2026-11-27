# Screeps / Event Feed icon set (SB-1)

Eight original, stroke-only vector icons. Six active engine kinds and two optional kinds.

- Each `kind.svg` is a standalone 24 × 24 SVG with its event accent color.
- `event_icons.sprite.svg` contains all eight symbols, named by `WorldEvent.Kind`.
- `event_icons.json` contains the source paths and colors, intended for porting to Go.
- The paths contain only absolute M (move), L (line) and Z (close) commands.
- Render into a 14 × 14 px square by scaling the 24-unit coordinate system by 14/24. Scale the 2.1-unit stroke with the geometry (approximately 1.225 px at 14 px). Use round caps and joins, no fill, and antialiasing.
- Keep the active event palette as specified; economy defaults to slate (#94a3b8), with amber (#fbbf24) available as a UI-level warning override.
- RCL uses one upward-arrow icon for all events of that kind. Danger uses a warning triangle; raid uses crossed swords. Optional kinds do not imply engine support.
- No glow or background is part of the icons. Color is redundant with the icon shape and event message.

The vectors are standalone design assets, not a compiled Go integration. The project's local `paint.Path` API was not available for verification. Use the manifest to translate M/L/Z into the corresponding move/line/close operations of your renderer.
