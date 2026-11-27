// objects.hlsl: High-craft GPU 3D raymarched SDF models matching the Screeps concept sheet.
// - Volumetric emissive bloom (cyan & yellow halos) + local colored bounce light (GI).
// - Zero-cost tetrahedral Laplacian curvature for crisp steel-blue bevel highlights & dark crevice lines.
// - Layered translucent-core shading for crystals & energy cubes + vertical gradient faction domes.
//
// Params:
//   params[0] = (kind, cosYaw, sinYaw, energyRatio)
//   params[1] = (colR, colG, colB, flags)
//               creep flags: 0=harvester, 1=hauler, 2=builder, 3=upgrader, 4=scout, 5=defender/combat,
//                            6=dismantler, 7=healer/medic, 8=skirmisher, 9=claimer/reserver,
//                            10=mineralHarvester, 11=pioneer/remoteBuilder
//               spawn flags: 4=actively spawning
//   params[2] = (anchorX, anchorY, scalePx, 0)

float smin(float a, float b, float k) {
    float h = clamp(0.5 + 0.5 * (b - a) / k, 0.0, 1.0);
    return lerp(b, a, h) - k * h * (1.0 - h);
}

float smax(float a, float b, float k) {
    return -smin(-a, -b, k);
}

float sdEllipsoid(float3 p, float3 r) {
    float k0 = length(p / r);
    float k1 = length(p / (r * r));
    return k0 * (k0 - 1.0) / max(k1, 1e-4);
}

float sdCapsule(float3 p, float3 a, float3 b, float r) {
    float3 pa = p - a, ba = b - a;
    float h = clamp(dot(pa, ba) / max(dot(ba, ba), 1e-5), 0.0, 1.0);
    return length(pa - ba * h) - r;
}

float sdRoundBox(float3 p, float3 b, float r) {
    float3 q = abs(p) - b;
    return length(max(q, 0.0)) + min(max(q.x, max(q.y, q.z)), 0.0) - r;
}

float sdPyramid4(float3 p, float w, float h) {
    float3 q = p;
    q.xz = abs(q.xz);
    float m = max(q.x, q.z);
    float slope = (m - w * (1.0 - clamp(q.y / max(h, 0.01), 0.0, 1.0))) * 0.7071;
    return max(slope, max(q.y - h, -q.y));
}

float sdOctahedron(float3 p, float s) {
    p = abs(p);
    return (p.x + p.y + p.z - s) * 0.57735027;
}

float sdTorus(float3 p, float2 tr) {
    float2 q = float2(length(p.xz) - tr.x, p.y);
    return length(q) - tr.y;
}

float2 rot2(float2 p, float a) {
    float c = cos(a), s = sin(a);
    return float2(p.x * c - p.y * s, p.x * s + p.y * c);
}

// materialID:
//   0 = dark slate armor / architecture
//   1 = faction dome (vertical cerulean gradient)
//   2 = glowing warm yellow energy (white-hot core + amber rim)
//   3 = glowing electric cyan crystal / neon / holo
//   4 = brushed metallic steel (claws, crane, barrel)
//   5 = glowing neon magenta/violet (Spawn well, Claimer spire/halo, Mineral/Extractor crystals)
//   6 = glowing emerald healing green (Healer/Medic cross & emitter tips)
//   7 = glowing fiery siege orange (Dismantler rotary saw core & teeth)
//
// mapScene returns float4(minDist, matID, dYellowEmissive, dCyanEmissive)
void addMat(inout float4 res, float d, float matID) {
    if (d < res.x) {
        res.x = d;
        res.y = matID;
    }
    if (matID > 1.5 && matID < 2.5) {
        res.z = min(res.z, d);
    } else if (matID > 2.5 && matID < 3.5) {
        res.w = min(res.w, d);
    }
}

float4 mapScene(float3 p, float kind, float energy, float flags, float t) {
    float4 res = float4(1e5, 0.0, 1e5, 1e5);

    // 1. SPAWN: Two-tier beveled plinth + 4 corner brackets + magenta trim + recessed well + levitating cyan crystal
    if (kind < 1.5) {
        float bob = sin(t * 2.6) * 0.045;
        float3 ap = float3(abs(p.x), p.y, abs(p.z));

        // Stepped base plinth with corner armor tabs
        float plinth = sdRoundBox(p - float3(0.0, 0.08, 0.0), float3(0.74, 0.08, 0.74), 0.025);
        float deck = sdRoundBox(p - float3(0.0, 0.30, 0.0), float3(0.54, 0.16, 0.54), 0.025);
        float well = sdRoundBox(p - float3(0.0, 0.46, 0.0), float3(0.32, 0.14, 0.32), 0.02);
        float hull = min(plinth, max(deck, -well));

        // 4 raised corner armor brackets framing the crystal well
        float bracket = sdRoundBox(ap - float3(0.46, 0.38, 0.46), float3(0.13, 0.16, 0.13), 0.02);
        float cornerPad = sdRoundBox(ap - float3(0.68, 0.09, 0.68), float3(0.11, 0.09, 0.11), 0.02);
        hull = min(hull, min(bracket, cornerPad));

        // Neon magenta-violet trim wrapping upper deck (flashes yellow when actively spawning)
        float mTrim = sdRoundBox(p - float3(0.0, 0.31, 0.0), float3(0.555, 0.032, 0.555), 0.01);
        // Cyan energy strip around lower plinth + glowing inner well floor
        float baseTrim = sdRoundBox(p - float3(0.0, 0.10, 0.0), float3(0.755, 0.022, 0.755), 0.008);
        float pool = sdRoundBox(p - float3(0.0, 0.36, 0.0), float3(0.28, 0.03, 0.28), 0.01);

        // Crisp faceted floating cyan diamond crystal
        float3 cp = p - float3(0.0, 0.90 + bob, 0.0);
        cp.xz = rot2(cp.xz, t * 0.75);
        float crystal = sdOctahedron(cp * float3(1.18, 0.62, 1.18), 0.35) * 0.70;

        addMat(res, hull, 0.0);
        addMat(res, mTrim, (flags > 3.5) ? 2.0 : 5.0);
        addMat(res, min(baseTrim, min(pool, crystal)), 3.0);
        return res;
    }

    // 2. CONTROLLER / POWER RELAY: 4-tier stepped pyramid + 4 yellow corner pylons + cyan rails + floating crystal
    if (kind < 2.5) {
        float bob = sin(t * 2.2) * 0.05;
        float3 ap = float3(abs(p.x), p.y, abs(p.z));

        float plinth = sdRoundBox(p - float3(0.0, 0.08, 0.0), float3(0.74, 0.08, 0.74), 0.025);
        // Crisp 4-tier terraced pyramid (matches concept sheet Power Relay!)
        float t1 = sdRoundBox(p - float3(0.0, 0.24, 0.0), float3(0.46, 0.09, 0.46), 0.02);
        float t2 = sdRoundBox(p - float3(0.0, 0.44, 0.0), float3(0.36, 0.09, 0.36), 0.02);
        float t3 = sdRoundBox(p - float3(0.0, 0.64, 0.0), float3(0.26, 0.09, 0.26), 0.02);
        float t4 = sdRoundBox(p - float3(0.0, 0.84, 0.0), float3(0.16, 0.09, 0.16), 0.02);
        float hull = min(plinth, min(min(t1, t2), min(t3, t4)));

        // Glowing cyan circuit seams tucked between pyramid tiers
        float c1 = sdRoundBox(p - float3(0.0, 0.34, 0.0), float3(0.39, 0.02, 0.39), 0.008);
        float c2 = sdRoundBox(p - float3(0.0, 0.54, 0.0), float3(0.29, 0.02, 0.29), 0.008);
        float c3 = sdRoundBox(p - float3(0.0, 0.74, 0.0), float3(0.19, 0.02, 0.19), 0.008);

        // 4 corner pylons with glowing yellow beacon caps + cyan perimeter rails
        float post = sdRoundBox(ap - float3(0.58, 0.18, 0.58), float3(0.08, 0.12, 0.08), 0.015);
        float cap = sdRoundBox(ap - float3(0.58, 0.32, 0.58), float3(0.075, 0.045, 0.075), 0.015);
        hull = min(hull, post);

        float railX = sdRoundBox(ap - float3(0.0, 0.20, 0.58), float3(0.52, 0.02, 0.02), 0.006);
        float railZ = sdRoundBox(ap - float3(0.58, 0.20, 0.0), float3(0.02, 0.02, 0.52), 0.006);

        // Levitating faceted cyan crystal above apex
        float3 cp = p - float3(0.0, 1.32 + bob, 0.0);
        cp.xz = rot2(cp.xz, t * 0.85);
        float crystal = sdOctahedron(cp * float3(1.18, 0.62, 1.18), 0.33) * 0.70;

        addMat(res, hull, 0.0);
        addMat(res, cap, 2.0);
        addMat(res, min(min(min(c1, c2), c3), min(min(railX, railZ), crystal)), 3.0);
        return res;
    }

    // 3. SOURCE: Dark basalt plinth with glowing golden crystal pyramids
    if (kind < 3.5) {
        float pulse = 0.5 + 0.5 * sin(t * 2.4);
        float rock = sdRoundBox(p - float3(0.0, 0.09, 0.0), float3(0.58, 0.09, 0.58), 0.035);
        float k1 = sdPyramid4(p - float3(-0.12, 0.13, 0.04), 0.27, 0.90 + 0.04 * pulse);
        float k2 = sdPyramid4(p - float3(0.20, 0.13, -0.08), 0.21, 0.66);
        float k3 = sdPyramid4(p - float3(0.06, 0.13, 0.22), 0.17, 0.50);
        addMat(res, rock, 0.0);
        addMat(res, min(k1, min(k2, k3)), 2.0);
        return res;
    }

    // 4. MINERAL: Dark rock base with glowing cyan crystal spires
    if (kind < 4.5) {
        float rock = sdRoundBox(p - float3(0.0, 0.09, 0.0), float3(0.56, 0.09, 0.56), 0.035);
        float k1 = sdPyramid4(p - float3(-0.04, 0.13, 0.0), 0.26, 0.96);
        float k2 = sdPyramid4(p - float3(0.22, 0.13, 0.12), 0.18, 0.56);
        addMat(res, rock, 0.0);
        addMat(res, min(k1, k2), 3.0);
        return res;
    }

    // 5. EXTENSION: Beveled square dark-slate pad + 4 corner tabs + recessed socket + glowing yellow cube!
    if (kind < 5.5) {
        float3 ap = float3(abs(p.x), p.y, abs(p.z));
        float pad = sdRoundBox(p - float3(0.0, 0.08, 0.0), float3(0.47, 0.08, 0.47), 0.022);
        float tab = sdRoundBox(ap - float3(0.42, 0.09, 0.42), float3(0.09, 0.08, 0.09), 0.018);
        float collar = sdRoundBox(p - float3(0.0, 0.17, 0.0), float3(0.28, 0.04, 0.28), 0.015);
        float hull = min(min(pad, tab), collar);

        float trim = sdRoundBox(p - float3(0.0, 0.10, 0.0), float3(0.485, 0.018, 0.485), 0.006);
        float cubeH = 0.13 + 0.13 * energy;
        float cube = sdRoundBox(p - float3(0.0, 0.20 + cubeH * 0.62, 0.0), float3(0.21, cubeH, 0.21), 0.028);

        addMat(res, hull, 0.0);
        addMat(res, trim, 3.0);
        addMat(res, cube, (energy > 0.05) ? 2.0 : 4.0);
        return res;
    }

    // 6. TOWER: Beveled plinth with 4 cyan corner lights + armored neck + chamfered turret head with yellow optics!
    if (kind < 6.5) {
        float3 ap = float3(abs(p.x), p.y, abs(p.z));
        float base = sdRoundBox(p - float3(0.0, 0.10, 0.0), float3(0.60, 0.10, 0.60), 0.025);
        float plinth2 = sdRoundBox(p - float3(0.0, 0.21, 0.0), float3(0.42, 0.07, 0.42), 0.02);
        float neck = sdRoundBox(p - float3(-0.05, 0.48, 0.0), float3(0.18, 0.24, 0.20), 0.025);
        float head = sdRoundBox(p - float3(0.06, 0.80, 0.0), float3(0.30, 0.19, 0.25), 0.035);
        float hull = min(min(base, plinth2), min(neck, head));

        // 4 cyan corner indicator strips on base
        float cLight = sdRoundBox(ap - float3(0.44, 0.12, 0.61), float3(0.11, 0.022, 0.015), 0.006);
        float cLight2 = sdRoundBox(ap - float3(0.61, 0.12, 0.44), float3(0.015, 0.022, 0.11), 0.006);

        // Brushed-metal rail barrel + yellow side optic bars & muzzle tip
        float barrel = sdRoundBox(p - float3(0.44, 0.78, 0.0), float3(0.20, 0.09, 0.12), 0.02);
        float optic = sdRoundBox(float3(p.x, p.y, abs(p.z)) - float3(0.12, 0.83, 0.245), float3(0.13, 0.07, 0.03), 0.012);
        float muzzle = sdRoundBox(p - float3(0.65, 0.78, 0.0), float3(0.035, 0.065, 0.09), 0.012);

        addMat(res, hull, 0.0);
        addMat(res, barrel, 4.0);
        addMat(res, min(cLight, cLight2), 3.0);
        addMat(res, min(optic, muzzle), 2.0);
        return res;
    }

    // 7. STORAGE / TERMINAL: Heavy ribbed bunker with 3 raised armor arches, 4 glowing yellow cargo bays & cyan base!
    if (kind < 7.5) {
        float3 ap = float3(abs(p.x), p.y, abs(p.z));
        float base = sdRoundBox(p - float3(0.0, 0.09, 0.0), float3(0.68, 0.09, 0.68), 0.025);
        float baseTrim = sdRoundBox(p - float3(0.0, 0.11, 0.0), float3(0.695, 0.022, 0.695), 0.008);
        float body = sdRoundBox(p - float3(0.0, 0.38, 0.0), float3(0.52, 0.23, 0.54), 0.035);

        // 3 raised dark-slate armor ribs arching across the roof
        float ribCenter = sdRoundBox(p - float3(0.0, 0.43, 0.0), float3(0.56, 0.25, 0.07), 0.02);
        float ribOuter  = sdRoundBox(ap - float3(0.0, 0.43, 0.46), float3(0.56, 0.25, 0.08), 0.02);
        float ribSpine  = sdRoundBox(p - float3(0.0, 0.63, 0.0), float3(0.06, 0.07, 0.55), 0.018);
        float hull = min(min(base, body), min(min(ribCenter, ribOuter), ribSpine));

        // 4 glowing yellow cargo cells seated crisply between the armor ribs + side status bars
        float topCell = sdRoundBox(ap - float3(0.24, 0.64, 0.23), float3(0.14, 0.055, 0.13), 0.016);
        float sideCell = sdRoundBox(ap - float3(0.54, 0.38, 0.23), float3(0.035, 0.12, 0.13), 0.012);

        addMat(res, hull, 0.0);
        addMat(res, baseTrim, 3.0);
        addMat(res, min(topCell, sideCell), 2.0);
        return res;
    }

    // 8. CONTAINER: Heavy beveled dark-slate industrial cargo crate with corner armor posts & yellow gauge bars!
    if (kind < 8.5) {
        float3 ap = float3(abs(p.x), p.y, abs(p.z));
        float base = sdRoundBox(p - float3(0.0, 0.06, 0.0), float3(0.47, 0.06, 0.47), 0.02);
        float body = sdRoundBox(p - float3(0.0, 0.24, 0.0), float3(0.41, 0.17, 0.41), 0.025);
        float lid = sdRoundBox(p - float3(0.0, 0.42, 0.0), float3(0.44, 0.045, 0.44), 0.02);
        float post = sdRoundBox(ap - float3(0.39, 0.24, 0.39), float3(0.065, 0.18, 0.065), 0.015);
        float hull = min(min(base, body), min(lid, post));

        // Top lid recessed dark-slate trim + twin glowing yellow status bars
        float fillW = 0.08 + 0.15 * max(energy, 0.12);
        float topBar = sdRoundBox(ap - float3(0.16, 0.47, 0.0), float3(0.05, 0.02, 0.22), 0.008);
        // Side recessed glowing yellow energy gauge bars on all 4 faces
        float sideX = sdRoundBox(ap - float3(0.425, 0.24, 0.0), float3(0.022, 0.045, fillW), 0.008);
        float sideZ = sdRoundBox(ap - float3(0.0, 0.24, 0.425), float3(fillW, 0.045, 0.022), 0.008);
        float sideLoX = sdRoundBox(ap - float3(0.425, 0.14, 0.0), float3(0.02, 0.022, 0.22), 0.006);
        float sideLoZ = sdRoundBox(ap - float3(0.0, 0.14, 0.425), float3(0.22, 0.022, 0.02), 0.006);

        addMat(res, hull, 0.0);
        addMat(res, min(sideLoX, sideLoZ), 4.0);
        if (energy > 0.02) {
            addMat(res, min(topBar, min(sideX, sideZ)), 2.0);
        } else {
            addMat(res, topBar, 4.0);
        }
        return res;
    }

    // 9. CREEP: Rounded gradient faction dome + dark base collar + recessed cap socket + role hardware!
    // flags: 0=harvester, 1=hauler, 2=builder, 3=upgrader, 4=scout, 5=defender/combat
    if (kind < 9.5) {
        float hover = 0.06 + 0.02 * sin(t * 3.4);
        float3 q = p - float3(0.0, hover, 0.0);

        // SCOUT (flags == 4): Sleek aerodynamic teardrop pod + twin swept angular wings + twin cyan tips
        if (flags > 3.5 && flags < 4.5) {
            float pod = sdEllipsoid(q - float3(0.02, 0.26, 0.0), float3(0.38, 0.21, 0.29));
            float collar = sdEllipsoid(q - float3(0.0, 0.13, 0.0), float3(0.34, 0.08, 0.27));
            float cap = sdEllipsoid(q - float3(0.0, 0.44, 0.0), float3(0.15, 0.075, 0.13));
            float3 az = float3(q.x, q.y, abs(q.z));
            float3 wp = az - float3(-0.08, 0.27, 0.37);
            wp.xz = rot2(wp.xz, 0.22);
            float wing = sdRoundBox(wp, float3(0.38, 0.075, 0.09), 0.025);
            float tip = sdCapsule(az, float3(0.22, 0.21, 0.23), float3(0.48, 0.19, 0.25), 0.05);
            addMat(res, pod, 1.0);
            addMat(res, min(collar, wing), 0.0);
            addMat(res, cap, 2.0);
            addMat(res, tip, 3.0);
            return res;
        }

        // HAULER (flags == 1): Rounded periwinkle-blue cab dome + dark-slate flatbed with crisp cyan trim & yellow cube!
        if (flags > 0.5 && flags < 1.5) {
            float dome = sdEllipsoid(q - float3(0.25, 0.24, 0.0), float3(0.31, 0.24, 0.31));
            dome = smax(dome, -q.y + 0.04, 0.025);
            float collar = sdEllipsoid(q - float3(0.25, 0.09, 0.0), float3(0.32, 0.07, 0.32));
            float bed = sdRoundBox(q - float3(-0.22, 0.19, 0.0), float3(0.33, 0.15, 0.33), 0.028);
            float cradle = sdRoundBox(q - float3(-0.22, 0.34, 0.0), float3(0.24, 0.035, 0.24), 0.014);
            // Proud razor-thin cyan highlight strip along the dark-slate flatbed
            float trim = sdRoundBox(q - float3(-0.22, 0.15, 0.0), float3(0.362, 0.015, 0.362), 0.006);

            addMat(res, dome, 1.0);
            addMat(res, min(collar, min(bed, cradle)), 0.0);
            addMat(res, trim, 3.0);
            if (energy > 0.02) {
                float cubeH = 0.09 + 0.09 * energy;
                float cube = sdRoundBox(q - float3(-0.22, 0.37 + cubeH * 0.72, 0.0), float3(0.19, cubeH, 0.19), 0.025);
                addMat(res, cube, 2.0);
            } else {
                // Empty bay: sleek low warm-amber indicator pad instead of a tall grey block
                float pad = sdRoundBox(q - float3(-0.22, 0.385, 0.0), float3(0.16, 0.02, 0.16), 0.008);
                addMat(res, pad, 2.0);
            }
            return res;
        }

        // PIONEER / REMOTE BUILDER (flags == 11): Ruggedized chassis + amber beacon antenna + side cargo pack + wrench arm
        if (flags > 10.5) {
            float dome = sdEllipsoid(q - float3(0.0, 0.27, 0.0), float3(0.36, 0.28, 0.36));
            dome = smax(dome, -q.y + 0.04, 0.025);
            float collar = sdEllipsoid(q - float3(0.0, 0.09, 0.0), float3(0.38, 0.075, 0.38));
            float pack = sdRoundBox(q - float3(-0.30, 0.25, 0.0), float3(0.16, 0.17, 0.24), 0.03);
            float packStrip = sdRoundBox(q - float3(-0.46, 0.25, 0.0), float3(0.02, 0.07, 0.14), 0.008);
            float mast = sdCapsule(q, float3(-0.28, 0.42, 0.14), float3(-0.28, 0.80, 0.14), 0.024);
            float beacon = length(q - float3(-0.28, 0.84, 0.14)) - 0.062;
            float arm1 = sdCapsule(q, float3(0.10, 0.34, 0.30), float3(0.34, 0.50, 0.30), 0.055);
            float arm2 = sdCapsule(q, float3(0.34, 0.50, 0.30), float3(0.55, 0.30, 0.22), 0.048);
            float jaw1 = sdCapsule(q, float3(0.55, 0.30, 0.22), float3(0.67, 0.24, 0.28), 0.036);
            float jaw2 = sdCapsule(q, float3(0.55, 0.30, 0.22), float3(0.67, 0.19, 0.14), 0.036);
            addMat(res, dome, 1.0);
            addMat(res, min(collar, pack), 0.0);
            addMat(res, min(mast, min(min(arm1, arm2), min(jaw1, jaw2))), 4.0);
            addMat(res, min(packStrip, beacon), 2.0);
            return res;
        }

        // MINERAL HARVESTER (flags == 10): Armored chassis + glowing violet crystal canisters + conical crystal drill
        if (flags > 9.5) {
            float body = sdRoundBox(q - float3(-0.08, 0.27, 0.0), float3(0.36, 0.21, 0.28), 0.06);
            float collar = sdEllipsoid(q - float3(-0.04, 0.09, 0.0), float3(0.40, 0.075, 0.34));
            float3 az = float3(q.x, q.y, abs(q.z));
            float canister = sdCapsule(az, float3(-0.30, 0.28, 0.26), float3(0.06, 0.28, 0.26), 0.105);
            float shroud = sdCapsule(q, float3(0.22, 0.24, 0.0), float3(0.38, 0.24, 0.0), 0.16);
            // Tapered conical crystal auger pointing along +X
            float3 dp = q - float3(0.40, 0.24, 0.0);
            float drillFrac = clamp(dp.x / 0.34, 0.0, 1.0);
            float drillR = lerp(0.135, 0.02, drillFrac);
            float drill = max(length(dp.yz) - drillR, max(-dp.x, dp.x - 0.34));
            addMat(res, min(body, collar), 0.0);
            addMat(res, shroud, 4.0);
            addMat(res, min(canister, drill), 5.0);
            return res;
        }

        // CLAIMER / RESERVER (flags == 9): Tiered faction dome + tall central violet crystal spire + floating magenta halo
        if (flags > 8.5) {
            float base = sdEllipsoid(q - float3(0.0, 0.10, 0.0), float3(0.42, 0.085, 0.42));
            float dome = sdEllipsoid(q - float3(0.0, 0.26, 0.0), float3(0.35, 0.24, 0.35));
            dome = smax(dome, -q.y + 0.05, 0.025);
            float ringBase = sdTorus(q - float3(0.0, 0.40, 0.0), float2(0.22, 0.042));
            // Tall central purple crystal spire
            float3 sp = q - float3(0.0, 0.42, 0.0);
            float spire = sdPyramid4(sp, 0.125, 0.54);
            // Floating rotating magenta energy halo ring around the spire
            float3 hp = q - float3(0.0, 0.66 + 0.025 * sin(t * 3.8), 0.0);
            hp.xz = rot2(hp.xz, t * 1.4);
            float halo = sdTorus(hp, float2(0.32, 0.028));
            addMat(res, base, 0.0);
            addMat(res, dome, 1.0);
            addMat(res, ringBase, 4.0);
            addMat(res, min(spire, halo), 5.0);
            return res;
        }

        // SKIRMISHER (flags == 8): Agile faction dome + slate shoulder pods + twin forward cyan-vented railguns
        if (flags > 7.5) {
            float dome = sdEllipsoid(q - float3(-0.04, 0.27, 0.0), float3(0.33, 0.25, 0.31));
            dome = smax(dome, -q.y + 0.04, 0.025);
            float collar = sdEllipsoid(q - float3(-0.04, 0.09, 0.0), float3(0.35, 0.07, 0.33));
            float3 az = float3(q.x, q.y, abs(q.z));
            float pod = sdRoundBox(az - float3(-0.06, 0.27, 0.34), float3(0.20, 0.14, 0.095), 0.03);
            float barrel = sdRoundBox(az - float3(0.32, 0.27, 0.33), float3(0.30, 0.055, 0.055), 0.018);
            float muzzle = sdRoundBox(az - float3(0.46, 0.27, 0.33), float3(0.14, 0.026, 0.062), 0.008);
            float cap = sdEllipsoid(q - float3(-0.04, 0.49, 0.0), float3(0.14, 0.055, 0.14));
            addMat(res, dome, 1.0);
            addMat(res, min(collar, pod), 0.0);
            addMat(res, barrel, 4.0);
            addMat(res, min(muzzle, cap), 3.0);
            return res;
        }

        // HEALER / MEDIC (flags == 7): Clean support dome + glowing emerald top cross + twin green nanite emitters
        if (flags > 6.5) {
            float dome = sdEllipsoid(q - float3(0.0, 0.27, 0.0), float3(0.38, 0.28, 0.38));
            dome = smax(dome, -q.y + 0.04, 0.025);
            float collar = sdEllipsoid(q - float3(0.0, 0.09, 0.0), float3(0.40, 0.075, 0.40));
            // Glowing emerald medical cross recessed on crown
            float cross1 = sdRoundBox(q - float3(0.0, 0.52, 0.0), float3(0.17, 0.035, 0.055), 0.012);
            float cross2 = sdRoundBox(q - float3(0.0, 0.52, 0.0), float3(0.055, 0.035, 0.17), 0.012);
            float3 az = float3(q.x, q.y, abs(q.z));
            float pod = sdRoundBox(az - float3(0.02, 0.22, 0.39), float3(0.16, 0.10, 0.085), 0.025);
            float nozzle = sdCapsule(az, float3(0.16, 0.22, 0.39), float3(0.36, 0.20, 0.36), 0.042);
            float tip = length(az - float3(0.41, 0.19, 0.35)) - 0.058;
            addMat(res, dome, 1.0);
            addMat(res, min(collar, pod), 0.0);
            addMat(res, nozzle, 4.0);
            addMat(res, min(min(cross1, cross2), tip), 6.0);
            return res;
        }

        // DISMANTLER (flags == 6): Heavy siege chassis + amber vents + front yoke + spinning orange rotary demolition saw!
        if (flags > 5.5) {
            float hull = sdRoundBox(q - float3(-0.10, 0.27, 0.0), float3(0.34, 0.20, 0.29), 0.05);
            float collar = sdEllipsoid(q - float3(-0.08, 0.09, 0.0), float3(0.38, 0.075, 0.34));
            float3 az = float3(q.x, q.y, abs(q.z));
            float vent = sdRoundBox(az - float3(-0.14, 0.27, 0.32), float3(0.14, 0.08, 0.03), 0.012);
            float yoke = sdCapsule(az, float3(0.16, 0.24, 0.24), float3(0.42, 0.24, 0.14), 0.052);
            // Spinning circular saw blade in XY plane at front (x = 0.48)
            float3 sp = q - float3(0.46, 0.24, 0.0);
            sp.xy = rot2(sp.xy, t * 4.5);
            float ang = atan2(sp.y, sp.x);
            float tooth = 0.032 * sin(ang * 10.0);
            float bladeDisc = max(length(sp.xy) - (0.22 + tooth), abs(sp.z) - 0.032);
            float bladeHub = max(length(sp.xy) - 0.095, abs(sp.z) - 0.055);
            addMat(res, min(hull, collar), 0.0);
            addMat(res, yoke, 4.0);
            addMat(res, vent, 2.0);
            addMat(res, min(bladeDisc, bladeHub), 7.0);
            return res;
        }

        // Common rounded faction dome + dark base collar + recessed cap bezel + glowing yellow top cap
        float dome = sdEllipsoid(q - float3(0.0, 0.28, 0.0), float3(0.38, 0.31, 0.38));
        dome = smax(dome, -q.y + 0.04, 0.025);
        float collar = sdEllipsoid(q - float3(0.0, 0.09, 0.0), float3(0.39, 0.075, 0.39));
        float bezel = sdEllipsoid(q - float3(0.0, 0.54, 0.0), float3(0.21, 0.065, 0.21));
        float cap = sdEllipsoid(q - float3(0.0, 0.57, 0.0), float3(0.17, 0.085, 0.17));

        addMat(res, dome, 1.0);
        addMat(res, min(collar, bezel), 0.0);
        addMat(res, cap, 2.0);

        // DEFENDER / COMBAT (flags >= 5): Heavy curved slate pauldrons + kite riot shield with cyan emblem!
        if (flags >= 4.5) {
            float3 az = float3(q.x, q.y, abs(q.z));
            float pauldron = sdRoundBox(az - float3(-0.04, 0.30, 0.40), float3(0.22, 0.25, 0.095), 0.038);
            float shield = sdRoundBox(q - float3(0.43, 0.28, 0.0), float3(0.075, 0.24, 0.23), 0.035);
            float emblem = sdRoundBox(q - float3(0.505, 0.28, 0.0), float3(0.022, 0.13, 0.13), 0.015);
            addMat(res, min(pauldron, shield), 0.0);
            addMat(res, emblem, 3.0);
            return res;
        }

        // UPGRADER (flags == 3): Side slate pack (translucent cyan glass holo-panel is ray-intersected in shade())
        if (flags > 2.5 && flags < 3.5) {
            float pack = sdRoundBox(q - float3(-0.34, 0.22, 0.0), float3(0.14, 0.16, 0.22), 0.028);
            addMat(res, pack, 0.0);
            return res;
        }

        // BUILDER (flags == 2): Side slate pack + articulated elbow crane arm & C-shaped wrench claw!
        if (flags > 1.5 && flags < 2.5) {
            float pack = sdRoundBox(q - float3(-0.32, 0.22, 0.0), float3(0.14, 0.16, 0.22), 0.028);
            float arm1 = sdCapsule(q, float3(0.08, 0.36, 0.32), float3(0.32, 0.56, 0.34), 0.06);
            float arm2 = sdCapsule(q, float3(0.32, 0.56, 0.34), float3(0.54, 0.36, 0.26), 0.05);
            float jaw1 = sdCapsule(q, float3(0.54, 0.36, 0.26), float3(0.66, 0.30, 0.32), 0.04);
            float jaw2 = sdCapsule(q, float3(0.54, 0.36, 0.26), float3(0.66, 0.24, 0.18), 0.04);
            addMat(res, pack, 0.0);
            addMat(res, min(min(arm1, arm2), min(jaw1, jaw2)), 4.0);
            return res;
        }

        // HARVESTER (flags == 0): Side thruster pods + twin articulated mining pincers with cyan cutter tips!
        float3 az = float3(q.x, q.y, abs(q.z));
        float pod = sdRoundBox(az - float3(-0.08, 0.18, 0.38), float3(0.20, 0.12, 0.085), 0.028);
        float arm = sdCapsule(az, float3(0.22, 0.28, 0.22), float3(0.54, 0.18, 0.22), 0.06);
        float tip = sdCapsule(az, float3(0.50, 0.18, 0.22), float3(0.65, 0.10, 0.17), 0.05);
        addMat(res, pod, 0.0);
        addMat(res, arm, 4.0);
        addMat(res, tip, 3.0);
        return res;
    }

    // 10. DROPPED RESOURCE: Glowing golden energy cube
    if (kind < 10.5) {
        float3 cp = p - float3(0.0, 0.22 + 0.03 * sin(t * 3.0), 0.0);
        cp.xz = rot2(cp.xz, t * 1.1);
        addMat(res, sdRoundBox(cp, float3(0.18, 0.18, 0.18), 0.028), 2.0);
        return res;
    }

    // 11. CONSTRUCTED WALL: Crisp beveled dark fortification block with recessed horizontal seam
    if (kind < 11.5) {
        float wall = sdRoundBox(p - float3(0.0, 0.34, 0.0), float3(0.60, 0.34, 0.60), 0.035);
        float seam = sdRoundBox(p - float3(0.0, 0.36, 0.0), float3(0.62, 0.025, 0.62), 0.008);
        addMat(res, max(wall, -seam), 0.0);
        return res;
    }

    // 12. TOMBSTONE / RUIN: Rounded-arch dark-slate memorial headstone on rocky base with glowing cyan glyph!
    if (kind < 12.5) {
        // Stepped rocky dark-slate pedestal mound + accent stones
        float mound = sdEllipsoid(p - float3(0.0, 0.06, 0.0), float3(0.56, 0.11, 0.46));
        mound = smax(mound, -p.y, 0.02);
        float plinth = sdRoundBox(p - float3(0.0, 0.13, 0.0), float3(0.42, 0.07, 0.24), 0.025);
        float rockL = sdEllipsoid(p - float3(-0.38, 0.10, 0.22), float3(0.14, 0.09, 0.12));
        float rockR = sdEllipsoid(p - float3(0.36, 0.09, -0.18), float3(0.13, 0.08, 0.11));

        // Upright rounded-arch headstone slab + recessed front tablet face
        float slabBody = sdRoundBox(p - float3(0.0, 0.44, 0.0), float3(0.30, 0.28, 0.10), 0.03);
        float arch = sdCapsule(p, float3(0.0, 0.64, -0.06), float3(0.0, 0.64, 0.06), 0.31);
        float stone = smin(slabBody, arch, 0.04);
        // Subtle recessed front face so the outer stone rim catches bevel highlights
        float recess = sdRoundBox(p - float3(0.0, 0.46, 0.11), float3(0.21, 0.24, 0.035), 0.02);
        stone = max(stone, -recess);
        float hull = min(min(mound, plinth), min(min(rockL, rockR), stone));

        // Glowing cyan memorial emblem + 2 horizontal epitaph lines on front & back faces
        float az = abs(p.z);
        float skull = sdEllipsoid(float3(p.x, p.y, az) - float3(0.0, 0.60, 0.095), float3(0.09, 0.09, 0.025));
        float line1 = sdRoundBox(float3(p.x, p.y, az) - float3(0.0, 0.42, 0.095), float3(0.14, 0.022, 0.02), 0.008);
        float line2 = sdRoundBox(float3(p.x, p.y, az) - float3(0.0, 0.33, 0.095), float3(0.10, 0.022, 0.02), 0.008);
        float glyph = min(skull, min(line1, line2));

        addMat(res, hull, 0.0);
        addMat(res, glyph, 3.0);
        return res;
    }

    // 13. EXTRACTOR: 3-legged dark-slate tripod mining rig arching over glowing purple mineral crystals!
    {
        float mound = sdEllipsoid(p - float3(0.0, 0.06, 0.0), float3(0.72, 0.12, 0.72));
        mound = smax(mound, -p.y, 0.02);

        // 3 heavy arched industrial tripod legs spaced at 120 degrees
        float legs = 1e5;
        float footPads = 1e5;
        for (int i = 0; i < 3; i++) {
            float a = float(i) * 2.0943951;
            float2 d = float2(cos(a), sin(a));
            float3 foot = float3(d.x * 0.62, 0.08, d.y * 0.62);
            float3 knee = float3(d.x * 0.44, 0.56, d.y * 0.44);
            float3 topP = float3(d.x * 0.16, 0.88, d.y * 0.16);
            legs = min(legs, min(sdCapsule(p, foot, knee, 0.075), sdCapsule(p, knee, topP, 0.068)));
            footPads = min(footPads, sdRoundBox(p - float3(d.x * 0.62, 0.06, d.y * 0.62), float3(0.11, 0.06, 0.11), 0.02));
        }

        // Central apex extraction collar + vertical extractor core with amber status bars
        float head = sdRoundBox(p - float3(0.0, 0.88, 0.0), float3(0.19, 0.14, 0.19), 0.035);
        float core = sdCapsule(p, float3(0.0, 0.46, 0.0), float3(0.0, 0.78, 0.0), 0.095);
        float3 absXZ = float3(abs(p.x), p.y, abs(p.z));
        float gauge = min(
            sdRoundBox(absXZ - float3(0.195, 0.88, 0.0), float3(0.018, 0.07, 0.05), 0.006),
            sdRoundBox(absXZ - float3(0.0, 0.88, 0.195), float3(0.05, 0.07, 0.018), 0.006)
        );

        // Glowing purple/violet mineral crystal spires underneath the rig
        float c1 = sdPyramid4(p - float3(0.0, 0.06, 0.0), 0.17, 0.44);
        float c2 = sdPyramid4(p - float3(-0.20, 0.05, 0.14), 0.12, 0.30);
        float c3 = sdPyramid4(p - float3(0.20, 0.05, -0.12), 0.12, 0.32);
        float c4 = sdPyramid4(p - float3(0.06, 0.05, 0.22), 0.10, 0.24);
        float crystals = min(min(c1, c2), min(c3, c4));

        addMat(res, min(min(mound, footPads), min(legs, head)), 0.0);
        addMat(res, core, 4.0);
        addMat(res, gauge, 2.0);
        addMat(res, crystals, 5.0);
        return res;
    }
}

// Computes surface normal AND tetrahedral Laplacian curvature in 4 taps (zero extra cost).
// curvature > 0 on convex bevel edges (cool catchlight), < 0 in concave seams (dark crease).
float3 calcNormalAndCurv(float3 p, float d0, float kind, float energy, float flags, float t, out float curv) {
    float h = 0.005;
    float2 e = float2(h, -h);
    float d1 = mapScene(p + e.xyy, kind, energy, flags, t).x;
    float d2 = mapScene(p + e.yyx, kind, energy, flags, t).x;
    float d3 = mapScene(p + e.yxy, kind, energy, flags, t).x;
    float d4 = mapScene(p + e.xxx, kind, energy, flags, t).x;
    curv = (d1 + d2 + d3 + d4 - 4.0 * d0) / (h * 4.0);
    float3 g = e.xyy * d1 + e.yyx * d2 + e.yxy * d3 + e.xxx * d4;
    float len = length(g);
    return len > 1e-6 ? g / len : float3(0.0, 1.0, 0.0);
}

float calcSoftShadow(float3 ro, float3 rd, float kind, float energy, float flags, float t) {
    float res = 1.0;
    float d = 0.035;
    for (int s = 0; s < 12; s++) {
        float h = mapScene(ro + rd * d, kind, energy, flags, t).x;
        res = min(res, 7.0 * h / max(d, 0.015));
        d += clamp(h, 0.03, 0.18);
        if (res < 0.04 || d > 1.7) {
            break;
        }
    }
    return clamp(res, 0.24, 1.0);
}

float calcAO(float3 pos, float3 N, float kind, float energy, float flags, float t) {
    float occ = 0.0;
    float d1 = mapScene(pos + N * 0.05, kind, energy, flags, t).x;
    occ += (0.05 - d1);
    float d2 = mapScene(pos + N * 0.14, kind, energy, flags, t).x;
    occ += (0.14 - d2) * 0.65;
    float d3 = mapScene(pos + N * 0.30, kind, energy, flags, t).x;
    occ += (0.30 - d3) * 0.35;
    return clamp(1.0 - 2.6 * occ, 0.22, 1.0);
}

float sdSeg2D(float2 p, float2 a, float2 b) {
    float2 pa = p - a, ba = b - a;
    float h = clamp(dot(pa, ba) / max(dot(ba, ba), 1e-5), 0.0, 1.0);
    return length(pa - ba * h);
}

// Evaluates the Upgrader's translucent cyan glass holo-panel with glowing border & upward chevron (^)
// Returns premultiplied float4(rgb, alpha) if the ray hits the glass in front of maxT, else float4(0).
float4 evalUpgraderGlass(float3 ro, float3 rd, float maxT, float t, float px) {
    float hover = 0.06 + 0.02 * sin(t * 3.4);
    float3 center = float3(0.48, 0.35 + hover, 0.0);
    float c = cos(0.22), s = sin(0.22);
    float3 nPlane = float3(c, s, 0.0);
    float denom = dot(rd, nPlane);
    if (abs(denom) < 1e-4) {
        return float4(0.0, 0.0, 0.0, 0.0);
    }
    float tHit = dot(center - ro, nPlane) / denom;
    if (tHit <= 0.0 || tHit >= maxT) {
        return float4(0.0, 0.0, 0.0, 0.0);
    }
    float3 hp = (ro + rd * tHit) - center;
    float3 tUp = float3(-s, c, 0.0);
    float2 uv = float2(hp.z, dot(hp, tUp));
    float2 q = abs(uv) - float2(0.20, 0.21);
    float dRect = length(max(q, 0.0)) + min(max(q.x, q.y), 0.0) - 0.03;
    float mask = smoothstep(px, -px * 0.5, dRect);
    if (mask <= 0.002) {
        return float4(0.0, 0.0, 0.0, 0.0);
    }

    // Glowing cyan glass edge rim + subtle vertical scanline sheen
    float rim = smoothstep(0.036, 0.008, abs(dRect));
    // Crisp glowing upward double-chevron (^) icon in the center of the glass
    float2 auv = float2(abs(uv.x), uv.y);
    float chev1 = sdSeg2D(auv, float2(0.0, 0.095), float2(0.095, -0.005));
    float chev2 = sdSeg2D(auv, float2(0.0, 0.015), float2(0.080, -0.070));
    float chev = smoothstep(0.028, 0.010, min(chev1, chev2));

    float glassA = (0.24 + 0.56 * rim + 0.65 * chev) * mask;
    glassA = saturate(glassA);
    float3 glassCol = float3(0.10, 0.84, 0.98) * (0.34 + 0.65 * rim) +
                      float3(0.75, 1.00, 1.00) * (0.95 * chev + 0.30 * rim);
    return float4(saturate(glassCol) * mask, glassA);
}

float4 shade(ShaderInput i) {
    float kind = i.params[0].x;
    float cosY = i.params[0].y;
    float sinY = i.params[0].z;
    float energy = i.params[0].w;

    float3 factionCol = i.params[1].rgb;
    float flags = i.params[1].w;

    float anchorX = i.params[2].x;
    float anchorY = i.params[2].y;
    float scalePx = max(i.params[2].z, 1.0);

    float sx = (i.pos.x - anchorX) / scalePx;
    float sy = -(i.pos.y - anchorY) / scalePx;

    float3 camRight = float3(1.0, 0.0, 0.0);
    float3 camUp    = float3(0.0, 0.8660254, -0.5);
    float3 camFwd   = float3(0.0, -0.5, -0.8660254);

    float3 roCam = sx * camRight + sy * camUp - 3.6 * camFwd;
    float3 rdCam = camFwd;

    float3 ro = float3(roCam.x * cosY + roCam.z * sinY, roCam.y, -roCam.x * sinY + roCam.z * cosY);
    float3 rd = float3(rdCam.x * cosY + rdCam.z * sinY, rdCam.y, -rdCam.x * sinY + rdCam.z * cosY);

    // Exact physical pixel width in SDF units for zero-cost 1-pixel silhouette & seam AA
    float px = 1.20 / max(scalePx * max(i.scale, 1.0), 1.0);

    float dist = 0.0;
    float4 hit = float4(10.0, -1.0, 10.0, 10.0);
    float minYel = 10.0;
    float minCyn = 10.0;
    float bestDist = 10.0;
    float bestT = 0.0;
    bool didHit = false;

    for (int step = 0; step < 44; step++) {
        float3 p = ro + rd * dist;
        hit = mapScene(p, kind, energy, flags, i.time);
        minYel = min(minYel, hit.z);
        minCyn = min(minCyn, hit.w);
        if (hit.x < bestDist) {
            bestDist = hit.x;
            bestT = dist;
        }
        if (hit.x < 0.0035) {
            didHit = true;
            break;
        }
        dist += hit.x;
        if (dist > 7.5) {
            break;
        }
    }

    // Tight, subtle additive volumetric glow (gowaku uses premultiplied alpha: rgb adds, a occludes)
    float3 haloCol = float3(1.0, 0.80, 0.22) * (exp(-max(minYel, 0.0) * 24.0) * 0.16) +
                     float3(0.14, 0.92, 1.0) * (exp(-max(minCyn, 0.0) * 22.0) * 0.18);

    // Analytic silhouette coverage: 1.0 on solid hit, smooth 1-pixel ramp on grazing misses
    float cov = didHit ? 1.0 : smoothstep(px, 0.0035, bestDist);

    bool isUpgrader = (kind > 8.5 && kind < 9.5 && flags > 2.5 && flags < 3.5);
    float4 glass = float4(0.0, 0.0, 0.0, 0.0);
    if (isUpgrader) {
        float glassMaxT = didHit ? dist : 10.0;
        glass = evalUpgraderGlass(ro, rd, glassMaxT, i.time, px);
    }

    // Background shadow + halo (needed when cov < 1.0 for smooth silhouette compositing)
    float bgA = 0.0;
    float3 bgRgb = float3(0.0, 0.0, 0.0);
    if (cov < 0.999) {
        float3 spill = float3(0.0, 0.0, 0.0);
        if (abs(rd.y) > 0.01) {
            float tG = -ro.y / rd.y;
            if (tG > 0.0) {
                float3 gp = ro + rd * tG;
                float3 sp = gp - float3(0.11, 0.12, 0.11);
                float4 gHit = mapScene(sp, kind, energy, flags, i.time);
                bgA = (1.0 - smoothstep(-0.05, 0.26, gHit.x)) * 0.52;
                spill = float3(1.0, 0.78, 0.20) * (exp(-max(gHit.z, 0.0) * 14.0) * 0.07) +
                        float3(0.12, 0.90, 1.0) * (exp(-max(gHit.w, 0.0) * 14.0) * 0.08);
            }
        }
        bgRgb = saturate(spill + haloCol);
        if (cov < 0.004) {
            float3 resRgb = glass.rgb + bgRgb * (1.0 - glass.a);
            float resA = glass.a + bgA * (1.0 - glass.a);
            if (resA < 0.005 && max(resRgb.r, max(resRgb.g, resRgb.b)) < 0.004) {
                return float4(0.0, 0.0, 0.0, 0.0);
            }
            return float4(saturate(resRgb), saturate(resA));
        }
    }

    float3 pos = ro + rd * (didHit ? dist : bestT);
    if (!didHit) {
        hit = mapScene(pos, kind, energy, flags, i.time);
    }
    float curv = 0.0;
    float3 N = calcNormalAndCurv(pos, hit.x, kind, energy, flags, i.time, curv);
    float3 V = -rd;

    float3 L = normalize(float3(-0.62, 0.72, 0.32));
    float3 Lfill = normalize(float3(0.72, 0.38, -0.55));
    float3 H = normalize(L + V);
    float3 sunCol = float3(1.00, 0.96, 0.90);
    float3 skyCol = float3(0.34, 0.46, 0.68);
    float3 gndCol = float3(0.08, 0.11, 0.17);

    float ao = calcAO(pos, N, kind, energy, flags, i.time);
    float sh = calcSoftShadow(pos + N * 0.015, L, kind, energy, flags, i.time);
    float ndl = max(dot(N, L), 0.0);
    float ndlFill = max(dot(N, Lfill), 0.0);
    float ndv = saturate(dot(N, V));
    float ndh = max(dot(N, H), 0.0);

    // Subtle convex edge catchlight & crisp concave crevice darkening
    float edgeHi = saturate(curv * 2.6);
    float crevice = saturate(-curv * 4.8);

    float luma = dot(factionCol, float3(0.299, 0.587, 0.114));
    float3 tacticalCol = lerp(float3(luma, luma, luma), factionCol, 1.05);

    float3 albedo;
    float roughness = 0.62;
    float metallic = 0.18;
    float3 emissiveCol = float3(0.0, 0.0, 0.0);

    if (hit.y < 0.5) {
        // 0: Deep precision-machined dark slate armor with crisp steel-blue bevel edges & legible mid-tones
        albedo = float3(0.165, 0.205, 0.275);
        albedo += float3(0.20, 0.27, 0.38) * edgeHi * (0.40 + 0.60 * ndl);
        albedo *= (1.0 - 0.45 * crevice);
        roughness = 0.58;
        metallic = 0.26;
    } else if (hit.y < 1.5) {
        // 1: Studio-lit faction dome (rich cobalt-periwinkle shadow -> luminous periwinkle-blue crown)
        float vGrad = smoothstep(0.05, 0.56, pos.y);
        albedo = lerp(tacticalCol * float3(0.50, 0.52, 0.72), tacticalCol * float3(0.96, 1.00, 1.15), vGrad);
        roughness = 0.34;
        metallic = 0.10;
    } else if (hit.y < 2.5) {
        // 2: Layered warm yellow energy (rich golden-amber sides -> bright warm core on lit/front faces)
        float core = pow(ndv, 1.45) * (0.55 + 0.45 * ndl);
        albedo = lerp(float3(0.95, 0.62, 0.06), float3(1.0, 0.96, 0.74), core);
        roughness = 0.24;
        metallic = 0.04;
        emissiveCol = lerp(float3(0.72, 0.42, 0.04), float3(1.18, 1.05, 0.62), core) * 0.78;
    } else if (hit.y < 3.5) {
        // 3: Electric cyan crystal / neon / holo (deep teal-cyan facets -> icy-white lit core)
        float core = pow(ndv, 1.35) * (0.55 + 0.45 * ndl);
        albedo = lerp(float3(0.04, 0.74, 0.94), float3(0.82, 1.0, 1.0), core);
        roughness = 0.18;
        metallic = 0.06;
        emissiveCol = lerp(float3(0.05, 0.68, 0.90), float3(0.92, 1.15, 1.18), core) * 0.85;
    } else if (hit.y < 4.5) {
        // 4: Brushed titanium-grey mechanical attachments (claws, crane, rail barrel)
        albedo = float3(0.42, 0.49, 0.58) + float3(0.16, 0.20, 0.26) * edgeHi;
        albedo *= (1.0 - 0.42 * crevice);
        roughness = 0.36;
        metallic = 0.72;
    } else if (hit.y < 5.5) {
        // 5: Neon magenta-violet (Spawn trim, Claimer spire/halo, Mineral/Extractor crystals)
        float core = pow(ndv, 1.35);
        albedo = lerp(float3(0.76, 0.14, 0.92), float3(1.0, 0.70, 1.0), core);
        roughness = 0.22;
        metallic = 0.05;
        emissiveCol = albedo * 0.85;
    } else if (hit.y < 6.5) {
        // 6: Glowing emerald healing green (Healer/Medic medical cross & nanite emitters)
        float core = pow(ndv, 1.35);
        albedo = lerp(float3(0.08, 0.80, 0.36), float3(0.76, 1.0, 0.86), core);
        roughness = 0.20;
        metallic = 0.05;
        emissiveCol = albedo * 0.90;
    } else {
        // 7: Glowing fiery siege orange (Dismantler rotary demolition saw)
        float core = pow(ndv, 1.35);
        albedo = lerp(float3(0.95, 0.32, 0.04), float3(1.0, 0.82, 0.42), core);
        roughness = 0.24;
        metallic = 0.28;
        emissiveCol = lerp(float3(0.85, 0.26, 0.02), float3(1.15, 0.68, 0.20), core) * 0.85;
    }

    float specPow = lerp(80.0, 12.0, roughness);
    float3 F0 = lerp(float3(0.04, 0.04, 0.04), albedo, metallic);
    float3 specCol = F0 * pow(ndh, specPow) * (1.35 - roughness) * sh;

    // Crisp 3-plane directional contrast with cool studio fill light so shadowed faces stay rich & legible
    float3 hemi = lerp(gndCol, skyCol, N.y * 0.5 + 0.5);
    float3 fillLight = float3(0.36, 0.46, 0.68) * (0.34 * ndlFill * ao);
    float3 diffuse = albedo * (1.0 - metallic * 0.40) * (hemi * 0.48 * ao + fillLight + sunCol * (1.28 * ndl * sh * ao));

    // Tight local colored bounce light (GI) right next to yellow/cyan emissives
    float3 bounce = float3(0.0, 0.0, 0.0);
    if (hit.y < 1.5 || (hit.y > 3.5 && hit.y < 4.5)) {
        bounce = float3(1.0, 0.74, 0.14) * (exp(-max(hit.z, 0.0) * 11.5) * 0.16) +
                 float3(0.08, 0.86, 1.0) * (exp(-max(hit.w, 0.0) * 11.5) * 0.16);
    }

    // Subtle 1-pixel anti-aliasing on internal glowing cyan/yellow trim seams
    if (hit.y < 1.5 || (hit.y > 3.5 && hit.y < 4.5)) {
        float aaYel = smoothstep(px * 1.1, 0.0, max(hit.z, 0.0));
        float aaCyn = smoothstep(px * 1.1, 0.0, max(hit.w, 0.0));
        emissiveCol += float3(1.0, 0.84, 0.28) * (aaYel * 0.55) +
                       float3(0.20, 0.94, 1.0) * (aaCyn * 0.60);
    }

    float rim = pow(max(1.0 - ndv, 0.0), 3.2) * (0.28 * (1.0 - roughness * 0.45)) * (0.45 + 0.55 * max(N.y, 0.0));
    float3 lit = diffuse + specCol + skyCol * rim + bounce + emissiveCol;

    // Ground contact AO fade at y=0 (gentle so base faces don't crush to black)
    lit *= lerp(0.72, 1.0, smoothstep(0.0, 0.15, pos.y));
    // Standard ACES filmic tone curve (deep blacks, rich mid-tones, crisp glowing highlights)
    lit = (lit * (2.51 * lit + 0.03)) / (lit * (2.43 * lit + 0.59) + 0.14);
    lit = saturate(lit);

    // Premultiplied composite of anti-aliased silhouette over shadow/halo, then translucent glass over top
    float3 outRgb = lit * cov + bgRgb * (1.0 - cov);
    float outA = cov + bgA * (1.0 - cov);
    if (glass.a > 0.002) {
        outRgb = glass.rgb + outRgb * (1.0 - glass.a);
        outA = glass.a + outA * (1.0 - glass.a);
    }
    return float4(saturate(outRgb), saturate(outA));
}
