// terrain.hlsl: GPU-accelerated 5x5 multi-room isometric Screeps terrain & 2.5D wall shader
// with continuous 360-degree yaw rotation via exact 2D DDA heightfield intersection.
//
// Tex0: 250x250 RGBA8 texture in UNROTATED world tile coords relative to active room
// (ix = gx + 100, iy = gy + 100):
//   R: 0=void, ~0.25=plain, ~0.50=swamp, ~0.75=wall, ~1.0=wall+swamp
//   G: wallDepth / 3.0  (0, 1/3, 2/3, 1.0)
//   B: 0=void, ~0.5=neighbor room, ~1.0=center room
//   A: 1.0
//
// Params:
//   params[0] = (originX, originY, halfW, halfH)
//   params[1] = (zoom, cosYaw, sinYaw, multiRoom)

float hash21(float2 p) {
    p = frac(p * float2(123.34, 456.21));
    p += dot(p, p + 45.32);
    return frac(p.x * p.y);
}

float4 sampleTile(float gx, float gy) {
    float ix = gx + 100.0;
    float iy = gy + 100.0;
    if (ix < 0.0 || ix >= 250.0 || iy < 0.0 || iy >= 250.0) {
        return float4(0.0, 0.0, 0.0, 0.0);
    }
    return sample0((float2(ix, iy) + 0.5) / 250.0);
}

float wallDepthAt(float gx, float gy) {
    float4 td = sampleTile(gx, gy);
    if (td.b < 0.25 || td.r < 0.62) {
        return 0.0;
    }
    return max(1.0, floor(td.g * 3.0 + 0.5));
}

float wallHeightU(float gx, float gy, float d, float zoom, float halfH) {
    // Deterministic per-tile height variation invariant across room boundary recentering (mod 50)
    float lx = fmod(gx + 1000.0, 50.0);
    float ly = fmod(gy + 1000.0, 50.0);
    float hVar = (hash21(float2(lx, ly)) - 0.5) * 0.9;
    return ((4.0 + 4.0 * d + hVar) * zoom) / halfH;
}

float4 shade(ShaderInput i) {
    float originX = i.params[0].x;
    float originY = i.params[0].y;
    float halfW = i.params[0].z;
    float halfH = i.params[0].w;
    float zoom = i.params[1].x;
    float cosY = i.params[1].y;
    float sinY = i.params[1].z;

    float3 bg = float3(0.055, 0.067, 0.082); // #0e1115
    if (halfW <= 0.0 || halfH <= 0.0) {
        return float4(bg, 1.0);
    }

    // Screen pixel -> rotated iso coords -> unrotated world coords P0 at ground (z=0).
    float dx = (i.pos.x - originX) / halfW;
    float dy = (i.pos.y - originY) / halfH;
    float frx0 = 0.5 * (dx + dy);
    float fry0 = 0.5 * (dy - dx);

    float ux0 = frx0 - 24.5;
    float uy0 = fry0 - 24.5;
    float2 P0 = float2(
        ux0 * cosY + uy0 * sinY + 24.5,
        -ux0 * sinY + uy0 * cosY + 24.5
    );

    // Ray in unrotated world coords as height z increases: P(z) = P0 + D * z.
    float2 D = float2(0.5 * (cosY + sinY), 0.5 * (cosY - sinY));
    float zMax = (17.2 * zoom) / halfH;

    // Exact 2D DDA walking from front/top (z = zMax, u = 0) down to ground (z = 0, u = zMax).
    float2 pos = P0 + D * zMax;
    float2 V = -D;
    float2 cell = floor(pos + 0.5);

    float stepX = (V.x >= 0.0) ? 1.0 : -1.0;
    float stepY = (V.y >= 0.0) ? 1.0 : -1.0;
    float nextX = cell.x + 0.5 * stepX;
    float nextY = cell.y + 0.5 * stepY;

    float uMaxX = (abs(V.x) > 1e-5) ? ((nextX - pos.x) / V.x) : 1e9;
    float uMaxY = (abs(V.y) > 1e-5) ? ((nextY - pos.y) / V.y) : 1e9;
    float uDeltaX = (abs(V.x) > 1e-5) ? abs(1.0 / V.x) : 1e9;
    float uDeltaY = (abs(V.y) > 1e-5) ? abs(1.0 / V.y) : 1e9;

    float uCur = 0.0;
    float2 hitNormal = float2(0.0, 0.0);
    // Sun direction in world space (upper-left at Yaw=0)
    float2 sunDir = normalize(float2(-0.75, -0.55));

    [unroll]
    for (int k = 0; k < 8; k++) {
        float uNext = min(min(uMaxX, uMaxY), zMax);
        float tEnter = zMax - uCur;
        float tExit = zMax - uNext;

        float4 td = sampleTile(cell.x, cell.y);
        if (td.b >= 0.25 && td.r >= 0.62) {
            float d = max(1.0, floor(td.g * 3.0 + 0.5));
            float h = wallHeightU(cell.x, cell.y, d, zoom, halfH);
            if (h >= tExit) {
                float dim = (td.b > 0.75) ? 1.0 : 0.78;
                float grain = (hash21(cell * 1.7 + 3.1) - 0.5) * 0.025;

                if (h <= tEnter) {
                    // Hit flat top plateau at z = h
                    float2 hitP = P0 + D * h;
                    float2 lf = hitP - cell; // [-0.5 .. 0.5]
                    float3 topCol = float3(0.235, 0.262, 0.318); // d=1
                    if (d >= 3.0) {
                        topCol = float3(0.325, 0.362, 0.435);    // d>=3
                    } else if (d >= 2.0) {
                        topCol = float3(0.278, 0.310, 0.376);    // d=2
                    }
                    float sunGrad = clamp(0.07 * dot(lf, sunDir), -0.04, 0.06);
                    topCol += sunGrad + grain;

                    if (halfW >= 5.0) {
                        float edgeDist = max(abs(lf.x), abs(lf.y)) * 2.0; // 0..1
                        float bevel = smoothstep(0.76, 0.96, edgeDist);
                        float rim = (dot(lf, sunDir) > 0.0) ? (0.045 * bevel) : (-0.055 * bevel);
                        topCol += rim;
                    }
                    return float4(saturate(topCol * dim), 1.0);
                } else {
                    // Hit vertical cliff face at z = tEnter
                    float v = clamp(tEnter / max(h, 0.01), 0.0, 1.0);
                    float ao = lerp(0.73, 1.04, smoothstep(0.0, 0.75, v));
                    float ndl = dot(hitNormal, sunDir); // -1 .. +1
                    float3 sideBase = lerp(float3(0.122, 0.140, 0.175), float3(0.185, 0.208, 0.255), ndl * 0.5 + 0.5);
                    float3 sideCol = (sideBase + grain * 0.55) * ao * dim;
                    return float4(saturate(sideCol), 1.0);
                }
            }
        }

        if (uNext >= zMax) {
            break;
        }
        if (uMaxX < uMaxY) {
            cell.x += stepX;
            uCur = uMaxX;
            uMaxX += uDeltaX;
            hitNormal = float2(-stepX, 0.0);
        } else {
            cell.y += stepY;
            uCur = uMaxY;
            uMaxY += uDeltaY;
            hitNormal = float2(0.0, -stepY);
        }
    }

    // Ground floor tile at P0
    float gx = floor(P0.x + 0.5);
    float gy = floor(P0.y + 0.5);
    float4 g = sampleTile(gx, gy);
    if (g.b < 0.25) {
        return float4(bg, 1.0);
    }

    float fx = P0.x - gx;
    float fy = P0.y - gy;

    float3 col;
    bool isSwamp = (g.r > 0.37 && g.r < 0.62);
    if (isSwamp) {
        float wave1 = sin(P0.x * 1.65 + P0.y * 1.15 + i.time * 0.70);
        float wave2 = cos(P0.x * 0.95 - P0.y * 1.85 - i.time * 0.55);
        float shimmer = (wave1 * wave2) * 0.5 + 0.5;
        float3 swampDeep = float3(0.086, 0.165, 0.122);
        float3 swampGlow = float3(0.128, 0.232, 0.174);
        col = lerp(swampDeep, swampGlow, shimmer * 0.42);
    } else {
        // Crisp, 100% deterministic two-tone isometric checkerboard (zero non-deterministic noise)
        float checker = abs(fmod(gx + gy + 1000.0, 2.0));
        if (checker < 0.5) {
            col = float3(0.112, 0.129, 0.161);
        } else {
            col = float3(0.100, 0.116, 0.146);
        }
    }

    // Cliff-base ambient occlusion & soft directional wall shadows
    float wNegX = wallDepthAt(gx - 1.0, gy);
    float wNegY = wallDepthAt(gx, gy - 1.0);
    float wPosX = wallDepthAt(gx + 1.0, gy);
    float wPosY = wallDepthAt(gx, gy + 1.0);

    float shadow = 0.0;
    if (wNegX > 0.0) {
        float reach = clamp(0.45 + 0.18 * wNegX, 0.45, 0.95);
        shadow = max(shadow, (1.0 - smoothstep(0.0, reach, fx + 0.5)) * (0.28 + 0.06 * wNegX));
    }
    if (wNegY > 0.0) {
        shadow = max(shadow, (1.0 - smoothstep(0.0, 0.55, fy + 0.5)) * (0.18 + 0.04 * wNegY));
    }
    if (wPosX > 0.0) {
        shadow = max(shadow, (1.0 - smoothstep(0.0, 0.32, 0.5 - fx)) * 0.20);
    }
    if (wPosY > 0.0) {
        shadow = max(shadow, (1.0 - smoothstep(0.0, 0.32, 0.5 - fy)) * 0.22);
    }
    if (shadow < 0.25 && wallDepthAt(gx - 1.0, gy - 1.0) > 0.0) {
        float cDist = length(float2(fx + 0.5, fy + 0.5));
        shadow = max(shadow, (1.0 - smoothstep(0.0, 0.55, cDist)) * 0.22);
    }
    col *= (1.0 - shadow);

    bool isCenter = (g.b > 0.75);
    if (!isCenter) {
        col *= 0.76;
    }

    // Crisp anti-aliased tile grid lines when zoomed in enough
    if (halfW >= 5.0) {
        float edgeDist = min(0.5 - abs(fx), 0.5 - abs(fy)) * halfW;
        float lineAlpha = (1.0 - smoothstep(0.0, 0.9, edgeDist)) * (isCenter ? 0.32 : 0.18);
        col = lerp(col, float3(0.038, 0.048, 0.065), lineAlpha);
    }

    return float4(saturate(col), 1.0);
}
