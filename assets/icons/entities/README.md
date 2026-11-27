# Screeps / world entity & creep role icon set

Companion to `event_icons.json`. This pack contains 30 original vector icons: 14 structures, 11 named creep roles plus the `creep` fallback, and 4 natural/dropped objects. All keys and colors match the supplied tables exactly.

## Files
- `entity_icons.json`: drop-in manifest, using the same top-level fields and types as the previous event manifest. Each icon entry contains `color` and `path`.
- `<key>.svg`: standalone SVG per exact case-sensitive entity key.
- `entity_icons.sprite.svg`: all 30 SVG symbols (IDs equal the entity keys).
- `preview.svg` and `preview.png`: reference sheet with 14px examples and a stacked-tab mockup.

## Native Go drawing
Use the same path conversion as for `event_icons.json`: parse absolute `M x y`, `L x y`, and `Z` commands. There are no curves, relative commands, transforms, fills, clipping masks or embedded rasters in the individual assets. Scale the 24×24 viewBox to the desired target (14×14 by default); scale the 2.1 master stroke as well (1.225 px at 14 px). Use round joins and caps, no fill, and antialiasing. For disabled/selected states tint the existing paths in the UI if required. Keep semantic accents distinct from ownership/faction tint and do not encode information by color alone.

## Lookup
Use separate event and entity icon maps: `spawn` occurs in both but has different geometry (event cube versus entity core ring). For creep entities, select the role icon first and fall back to `creep` for unknown/empty roles. Keep keys case sensitive (`constructedWall`, `constructionSite`, `keeperLair`, `mineralHarvester`). The manifest contains only the keys listed in the request; the role list has 11 named roles, not 12. Potential role aliases from the supplied names can be normalized at the call site if those values occur in the engine: `miner` → `harvester`, `carrier` → `hauler`, `brawler` → `defender`, `medic` → `healer`, `reserver` → `claimer`, `remoteBuilder` → `pioneer`. These are suggestions, not verified engine identifiers, and are not extra manifest entries.

The geometry, palette, SVGs, manifest, and raster previews were validated as standalone assets; local Go compilation/integration was not performed.
