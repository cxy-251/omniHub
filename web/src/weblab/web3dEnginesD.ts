import * as THREE from 'three';
import {
  ActiveSceneHandle,
  SceneRunContext,
  attachOrbitCamera,
  createThreeStage,
} from './web3dEnginesA';

const FULLSCREEN_VERTEX = /* glsl */ `
varying vec2 vUv;
void main() {
  vUv = uv;
  gl_Position = vec4(position.xy, 0.0, 1.0);
}
`;

// ============================================================================
// SCENE 042 + 017: SDF Raymarching Ferrofluid & Smooth Boolean Sculpture
// ============================================================================
export function mountSdfFerrofluidScene(ctx: SceneRunContext): ActiveSceneHandle {
  const { renderer, scene, camera, disposeStage } = createThreeStage(ctx.container, 45);
  scene.background = new THREE.Color(0x03050a);
  const orbit = attachOrbitCamera(renderer.domElement, camera, {
    yaw: 0.42,
    pitch: 0.25,
    distance: 4.5,
    minDistance: 2.5,
    maxDistance: 8.5,
  });

  const uniforms = {
    uTime: { value: 0 },
    uResolution: { value: new THREE.Vector2(Math.max(1, ctx.container.clientWidth), Math.max(1, ctx.container.clientHeight)) },
    uShapeMorph: { value: 0.34 },
    uSmoothUnion: { value: 0.24 },
    uTwist: { value: 0.85 },
    uSpikeIntensity: { value: 0.08 },
    uSatelliteScale: { value: 1.05 },
    uRoughness: { value: 0.22 },
    uCamYaw: { value: 0.42 },
    uCamPitch: { value: 0.25 },
    uCamDist: { value: 4.5 },
  };

  const material = new THREE.ShaderMaterial({
    uniforms,
    vertexShader: FULLSCREEN_VERTEX,
    fragmentShader: /* glsl */ `
      precision highp float;
      varying vec2 vUv;
      uniform float uTime;
      uniform vec2 uResolution;
      uniform float uShapeMorph;
      uniform float uSmoothUnion;
      uniform float uTwist;
      uniform float uSpikeIntensity;
      uniform float uSatelliteScale;
      uniform float uRoughness;
      uniform float uCamYaw;
      uniform float uCamPitch;
      uniform float uCamDist;

      mat2 rot(float a) {
        float c = cos(a), s = sin(a);
        return mat2(c, -s, s, c);
      }

      float smin(float a, float b, float k) {
        float h = clamp(0.5 + 0.5 * (b - a) / max(k, 0.0001), 0.0, 1.0);
        return mix(b, a, h) - k * h * (1.0 - h);
      }

      float sdSphere(vec3 p, float r) {
        return length(p) - r;
      }

      float sdTorus(vec3 p, vec2 t) {
        vec2 q = vec2(length(p.xz) - t.x, p.y);
        return length(q) - t.y;
      }

      float sdRoundBox(vec3 p, vec3 b, float r) {
        vec3 q = abs(p) - b;
        return length(max(q, 0.0)) + min(max(q.x, max(q.y, q.z)), 0.0) - r;
      }

      float gyroidSpike(vec3 p) {
        return sin(p.x) * cos(p.y) + sin(p.y) * cos(p.z) + sin(p.z) * cos(p.x);
      }

      vec2 mapScene(vec3 p) {
        vec3 q = p;
        q.xz *= rot(q.y * uTwist + uTime * 0.32);
        q.xy *= rot(uTime * 0.22);

        float dSphere = sdSphere(q, 1.05);
        float dTorus = sdTorus(q, vec2(0.88, 0.34));
        float dBox = sdRoundBox(q, vec3(0.72), 0.18);

        float m = clamp(uShapeMorph, 0.0, 1.0);
        float core = m < 0.5
          ? mix(dSphere, dTorus, m * 2.0)
          : mix(dTorus, dBox, (m - 0.5) * 2.0);

        // Ferrofluid high-frequency magnetic spikes
        float spike = gyroidSpike(q * 6.8 + vec3(uTime * 1.9, -uTime * 1.4, uTime * 1.1));
        float spike2 = gyroidSpike(q * 13.5 - vec3(uTime * 2.6));
        core -= (spike * 0.65 + spike2 * 0.35) * uSpikeIntensity;

        float d = core;
        float matId = 1.0;

        for (int i = 0; i < 4; i++) {
          float fi = float(i);
          float ang = uTime * (0.72 + fi * 0.16) + fi * 1.5707963;
          vec3 satPos = vec3(
            cos(ang) * (1.52 + 0.2 * sin(uTime + fi)),
            sin(ang * 1.35 + fi) * 0.68,
            sin(ang) * (1.52 + 0.2 * cos(uTime * 0.8 + fi))
          );
          float dSat = sdSphere(p - satPos, (0.28 + 0.04 * fi) * uSatelliteScale);
          dSat -= gyroidSpike((p - satPos) * 8.0 + uTime * 2.2) * (uSpikeIntensity * 0.55);
          float merged = smin(d, dSat, uSmoothUnion);
          if (dSat < d + 0.15) {
            matId = mix(matId, 2.0 + fi * 0.25, clamp((d - dSat) * 2.5 + 0.5, 0.0, 1.0));
          }
          d = merged;
        }

        float floorD = p.y + 1.95 + 0.04 * sin(length(p.xz) * 4.5 - uTime * 2.4);
        if (floorD < d) {
          return vec2(floorD, 0.0);
        }
        return vec2(d, matId);
      }

      vec3 calcNormal(vec3 p) {
        vec2 e = vec2(0.0018, -0.0018);
        return normalize(
          e.xyy * mapScene(p + e.xyy).x +
          e.yyx * mapScene(p + e.yyx).x +
          e.yxy * mapScene(p + e.yxy).x +
          e.xxx * mapScene(p + e.xxx).x
        );
      }

      float softShadow(vec3 ro, vec3 rd, float mint, float maxt, float k) {
        float res = 1.0;
        float t = mint;
        for (int i = 0; i < 28; i++) {
          float h = mapScene(ro + rd * t).x;
          res = min(res, k * h / max(t, 0.001));
          t += clamp(h, 0.02, 0.22);
          if (res < 0.005 || t > maxt) break;
        }
        return clamp(res, 0.0, 1.0);
      }

      float calcAO(vec3 pos, vec3 nor) {
        float occ = 0.0;
        float sca = 1.0;
        for (int i = 0; i < 5; i++) {
          float h = 0.02 + 0.11 * float(i) / 4.0;
          float d = mapScene(pos + h * nor).x;
          occ += (h - d) * sca;
          sca *= 0.72;
        }
        return clamp(1.0 - 2.4 * occ, 0.0, 1.0);
      }

      void main() {
        vec2 uv = (vUv - 0.5) * 2.0;
        uv.x *= uResolution.x / max(1.0, uResolution.y);

        float cp = cos(uCamPitch);
        vec3 ro = vec3(
          sin(uCamYaw) * cp * uCamDist,
          sin(uCamPitch) * uCamDist,
          cos(uCamYaw) * cp * uCamDist
        );
        vec3 ta = vec3(0.0, -0.1, 0.0);
        vec3 cw = normalize(ta - ro);
        vec3 cu = normalize(cross(cw, vec3(0.0, 1.0, 0.0)));
        vec3 cv = cross(cu, cw);
        vec3 rd = normalize(uv.x * cu + uv.y * cv + 1.85 * cw);

        vec3 col = mix(vec3(0.02, 0.03, 0.07), vec3(0.06, 0.10, 0.19), clamp(vUv.y, 0.0, 1.0));

        float t = 0.0;
        float matId = -1.0;
        for (int i = 0; i < 104; i++) {
          vec3 p = ro + rd * t;
          vec2 h = mapScene(p);
          if (h.x < 0.0015) {
            matId = h.y;
            break;
          }
          t += h.x * 0.78;
          if (t > 18.0) break;
        }

        if (matId >= 0.0) {
          vec3 pos = ro + rd * t;
          vec3 nor = calcNormal(pos);
          vec3 lig = normalize(vec3(0.65, 0.82, 0.48));
          vec3 hal = normalize(lig - rd);

          float dif = clamp(dot(nor, lig), 0.0, 1.0);
          float sha = softShadow(pos + nor * 0.01, lig, 0.02, 8.0, 12.0);
          float ao = calcAO(pos, nor);
          float fre = pow(clamp(1.0 + dot(nor, rd), 0.0, 1.0), 3.2);
          float specPow = mix(96.0, 12.0, clamp(uRoughness, 0.05, 0.95));
          float spe = pow(clamp(dot(nor, hal), 0.0, 1.0), specPow) * (1.15 - uRoughness);

          vec3 baseCol;
          if (matId < 0.5) {
            float grid = smoothstep(0.95, 0.99, max(cos(pos.x * 4.0), cos(pos.z * 4.0)));
            baseCol = mix(vec3(0.04, 0.06, 0.11), vec3(0.12, 0.25, 0.42), grid);
          } else {
            vec3 c1 = vec3(0.12, 0.58, 0.96);
            vec3 c2 = vec3(0.96, 0.38, 0.62);
            vec3 c3 = vec3(0.24, 0.92, 0.78);
            baseCol = mix(c1, c2, clamp(matId - 1.0, 0.0, 1.0));
            baseCol = mix(baseCol, c3, 0.5 + 0.5 * sin(pos.y * 2.5 + uTime));
          }

          vec3 lin = vec3(0.0);
          lin += dif * sha * vec3(1.25, 1.18, 1.08);
          lin += ao * vec3(0.18, 0.24, 0.36);
          lin += fre * ao * vec3(0.35, 0.72, 1.0) * 1.1;

          col = baseCol * lin + spe * sha * vec3(1.0, 0.95, 0.9) * 0.9;
          col = mix(col, vec3(0.02, 0.03, 0.07), 1.0 - exp(-0.012 * t * t));
        }

        col = col / (1.0 + col * 0.65);
        col = pow(max(col, 0.0), vec3(0.4545));
        gl_FragColor = vec4(col, 1.0);
      }
    `,
    depthWrite: false,
    depthTest: false,
  });

  const quad = new THREE.Mesh(new THREE.PlaneGeometry(2, 2), material);
  scene.add(quad);

  let raf = 0;
  let clock = performance.now();
  let hudTick = 0;

  const animate = (now: number) => {
    raf = requestAnimationFrame(animate);
    const dt = Math.min((now - clock) * 0.001, 0.05);
    clock = now;

    const p = ctx.getParams();
    const audio = ctx.getAudioPulse();
    orbit.step(dt, 0.14);

    uniforms.uTime.value = now * 0.001;
    uniforms.uResolution.value.set(Math.max(1, ctx.container.clientWidth), Math.max(1, ctx.container.clientHeight));
    uniforms.uShapeMorph.value = Number(p.shapeMorph ?? 0.34);
    uniforms.uSmoothUnion.value = Number(p.smoothUnion ?? 0.24);
    uniforms.uTwist.value = Number(p.twist ?? 0.85);
    uniforms.uSpikeIntensity.value =
      Number(p.spikeIntensity ?? 0.08) + (audio.enabled ? audio.bass * 0.18 : 0);
    uniforms.uSatelliteScale.value = Number(p.satelliteScale ?? 1.05);
    uniforms.uRoughness.value = Number(p.roughness ?? 0.22);
    uniforms.uCamYaw.value = orbit.state.yaw;
    uniforms.uCamPitch.value = orbit.state.pitch;
    uniforms.uCamDist.value = orbit.state.distance;

    renderer.render(scene, camera);

    if (now - hudTick > 280) {
      hudTick = now;
      ctx.onHudUpdate(
        `104-Step Raymarching · SmoothMin k=${uniforms.uSmoothUnion.value.toFixed(2)} · 尖刺调制 ${(uniforms.uSpikeIntensity.value * 100).toFixed(0)}%`,
      );
    }
  };

  raf = requestAnimationFrame(animate);

  return {
    dispose() {
      cancelAnimationFrame(raf);
      orbit.dispose();
      disposeStage();
    },
  };
}

// ============================================================================
// SCENE 037: Molten Voronoi Strata & Eroded Crystal Relief
// ============================================================================
export function mountMoltenReliefScene(ctx: SceneRunContext): ActiveSceneHandle {
  const { renderer, scene, camera, disposeStage } = createThreeStage(ctx.container, 45);
  scene.background = new THREE.Color(0x050307);
  const orbit = attachOrbitCamera(renderer.domElement, camera, {
    yaw: 0.0,
    pitch: 0.0,
    distance: 5.0,
  });

  const uniforms = {
    uTime: { value: 0 },
    uResolution: { value: new THREE.Vector2(Math.max(1, ctx.container.clientWidth), Math.max(1, ctx.container.clientHeight)) },
    uReliefStrength: { value: 1.05 },
    uCellularScale: { value: 2.6 },
    uCrystalStrength: { value: 0.75 },
    uSymmetryStrength: { value: 0.35 },
    uHeatIntensity: { value: 1.15 },
    uBlueRimStrength: { value: 0.85 },
    uLightDir: { value: new THREE.Vector2(0.5, 0.4) },
  };

  const material = new THREE.ShaderMaterial({
    uniforms,
    vertexShader: FULLSCREEN_VERTEX,
    fragmentShader: /* glsl */ `
      precision highp float;
      varying vec2 vUv;
      uniform float uTime;
      uniform vec2 uResolution;
      uniform float uReliefStrength;
      uniform float uCellularScale;
      uniform float uCrystalStrength;
      uniform float uSymmetryStrength;
      uniform float uHeatIntensity;
      uniform float uBlueRimStrength;
      uniform vec2 uLightDir;

      vec2 hash2(vec2 p) {
        p = vec2(dot(p, vec2(127.1, 311.7)), dot(p, vec2(269.5, 183.3)));
        return fract(sin(p) * 43758.5453);
      }

      vec2 voronoi(vec2 x) {
        vec2 n = floor(x);
        vec2 f = fract(x);
        float md1 = 8.0;
        float md2 = 8.0;
        for (int j = -1; j <= 1; j++) {
          for (int i = -1; i <= 1; i++) {
            vec2 g = vec2(float(i), float(j));
            vec2 o = hash2(n + g);
            o = 0.5 + 0.42 * sin(uTime * 0.55 + 6.2831 * o);
            vec2 r = g + o - f;
            float d = dot(r, r);
            if (d < md1) {
              md2 = md1;
              md1 = d;
            } else if (d < md2) {
              md2 = d;
            }
          }
        }
        return vec2(sqrt(md1), sqrt(md2) - sqrt(md1));
      }

      vec2 foldHex(vec2 p) {
        float r = length(p);
        float a = atan(p.y, p.x);
        float sector = 3.14159265 / 3.0;
        a = mod(a, sector);
        a = abs(a - sector * 0.5);
        return vec2(cos(a), sin(a)) * r;
      }

      vec2 evalHeightAndCrack(vec2 p) {
        vec2 symP = mix(p, foldHex(p), uSymmetryStrength);
        vec2 v1 = voronoi(symP * uCellularScale);
        vec2 v2 = voronoi(symP * uCellularScale * 2.15 + vec2(3.7, -2.1));

        float strata = sin(v1.x * 16.0 - uTime * 0.9) * 0.12;
        float hexRosette = cos(atan(symP.y, symP.x) * 6.0) * exp(-length(symP) * 1.4) * uCrystalStrength * 0.38;
        float h = (v1.x * 0.68 + v2.x * 0.32 + strata + hexRosette) * uReliefStrength;
        float crack = smoothstep(0.16, 0.0, v1.y) + 0.45 * smoothstep(0.11, 0.0, v2.y);
        return vec2(h, crack);
      }

      void main() {
        vec2 p = (vUv - 0.5) * 2.2;
        p.x *= uResolution.x / max(1.0, uResolution.y);

        float eps = 0.005;
        vec2 hc = evalHeightAndCrack(p);
        float hx = evalHeightAndCrack(p + vec2(eps, 0.0)).x;
        float hy = evalHeightAndCrack(p + vec2(0.0, eps)).x;

        vec3 normal = normalize(vec3(-(hx - hc.x) / eps, -(hy - hc.x) / eps, 1.0));
        vec3 lightPos = normalize(vec3(uLightDir.x, uLightDir.y, 0.72));
        vec3 viewDir = vec3(0.0, 0.0, 1.0);
        vec3 halfVec = normalize(lightPos + viewDir);

        float diff = clamp(dot(normal, lightPos), 0.0, 1.0);
        float spec = pow(clamp(dot(normal, halfVec), 0.0, 1.0), 42.0);
        float rim = pow(1.0 - clamp(normal.z, 0.0, 1.0), 2.4);

        vec3 basalt = mix(vec3(0.05, 0.06, 0.09), vec3(0.18, 0.20, 0.26), clamp(hc.x * 0.75, 0.0, 1.0));
        vec3 magma = mix(vec3(0.85, 0.14, 0.02), vec3(1.0, 0.78, 0.22), clamp(hc.y * 0.85, 0.0, 1.0));

        vec3 col = basalt * (0.22 + diff * 0.95);
        col += spec * vec3(0.95, 0.88, 0.78) * 0.55;
        col += rim * vec3(0.22, 0.62, 1.0) * uBlueRimStrength;
        col += magma * hc.y * uHeatIntensity * (0.85 + 0.2 * sin(uTime * 3.2));

        col = col / (1.0 + col * 0.55);
        col = pow(max(col, 0.0), vec3(0.4545));
        gl_FragColor = vec4(col, 1.0);
      }
    `,
    depthWrite: false,
    depthTest: false,
  });

  const quad = new THREE.Mesh(new THREE.PlaneGeometry(2, 2), material);
  scene.add(quad);

  let raf = 0;
  let hudTick = 0;

  const animate = (now: number) => {
    raf = requestAnimationFrame(animate);
    const p = ctx.getParams();
    const audio = ctx.getAudioPulse();

    uniforms.uTime.value = now * 0.001;
    uniforms.uResolution.value.set(Math.max(1, ctx.container.clientWidth), Math.max(1, ctx.container.clientHeight));
    uniforms.uReliefStrength.value = Number(p.reliefStrength ?? 1.05);
    uniforms.uCellularScale.value = Number(p.cellularScale ?? 2.6);
    uniforms.uCrystalStrength.value = Number(p.crystalStrength ?? 0.75);
    uniforms.uSymmetryStrength.value = Number(p.symmetryStrength ?? 0.35);
    uniforms.uHeatIntensity.value =
      Number(p.heatIntensity ?? 1.15) + (audio.enabled ? audio.bass * 0.55 : 0);
    uniforms.uBlueRimStrength.value = Number(p.blueRimStrength ?? 0.85);
    uniforms.uLightDir.value.set(
      orbit.state.pointerNdc.x * 1.15 + 0.35,
      orbit.state.pointerNdc.y * 1.15 + 0.35,
    );

    renderer.render(scene, camera);

    if (now - hudTick > 300) {
      hudTick = now;
      ctx.onHudUpdate(
        `Voronoi 矿物高度场 · 晶胞密度 ${uniforms.uCellularScale.value.toFixed(1)} · 对称折叠 ${(uniforms.uSymmetryStrength.value * 100).toFixed(0)}%`,
      );
    }
  };

  raf = requestAnimationFrame(animate);

  return {
    dispose() {
      cancelAnimationFrame(raf);
      orbit.dispose();
      disposeStage();
    },
  };
}

// ============================================================================
// SCENE 044 + 052: Thin-Film Soap Interference & Young's Double-Slit Wavefront
// ============================================================================
export function mountThinFilmOpticsScene(ctx: SceneRunContext): ActiveSceneHandle {
  const { renderer, scene, camera, disposeStage } = createThreeStage(ctx.container, 45);
  scene.background = new THREE.Color(0x03050a);
  const orbit = attachOrbitCamera(renderer.domElement, camera);

  const uniforms = {
    uTime: { value: 0 },
    uResolution: { value: new THREE.Vector2(Math.max(1, ctx.container.clientWidth), Math.max(1, ctx.container.clientHeight)) },
    uMode: { value: 0 },
    uFilmThickness: { value: 420 },
    uThicknessVariation: { value: 190 },
    uRefractiveIndex: { value: 1.33 },
    uFlowSpeed: { value: 0.28 },
    uPointer: { value: new THREE.Vector2(0, 0) },
  };

  const material = new THREE.ShaderMaterial({
    uniforms,
    vertexShader: FULLSCREEN_VERTEX,
    fragmentShader: /* glsl */ `
      precision highp float;
      varying vec2 vUv;
      uniform float uTime;
      uniform vec2 uResolution;
      uniform int uMode;
      uniform float uFilmThickness;
      uniform float uThicknessVariation;
      uniform float uRefractiveIndex;
      uniform float uFlowSpeed;
      uniform vec2 uPointer;

      float hash(vec2 p) {
        return fract(sin(dot(p, vec2(127.1, 311.7))) * 43758.5453123);
      }

      float noise(vec2 p) {
        vec2 i = floor(p);
        vec2 f = fract(p);
        vec2 u = f * f * (3.0 - 2.0 * f);
        return mix(
          mix(hash(i + vec2(0.0, 0.0)), hash(i + vec2(1.0, 0.0)), u.x),
          mix(hash(i + vec2(0.0, 1.0)), hash(i + vec2(1.0, 1.0)), u.x),
          u.y
        );
      }

      float fbm(vec2 p) {
        float v = 0.0;
        float a = 0.5;
        mat2 m = mat2(1.6, 1.2, -1.2, 1.6);
        for (int i = 0; i < 5; i++) {
          v += a * noise(p);
          p = m * p;
          a *= 0.5;
        }
        return v;
      }

      void main() {
        vec2 p = (vUv - 0.5) * 2.0;
        p.x *= uResolution.x / max(1.0, uResolution.y);

        if (uMode == 0) {
          // Nanometer Thin-Film Interference (RGB wavelengths: 650nm, 510nm, 475nm)
          float t = uTime * uFlowSpeed;
          vec2 q = vec2(fbm(p * 1.8 + vec2(0.0, t)), fbm(p * 1.8 + vec2(5.2, -t * 0.8)));
          float swirl = fbm(p * 2.4 + 2.2 * q + vec2(t * 0.5, -t * 0.4));

          float gravityDrain = (1.0 - vUv.y) * 140.0;
          float d_nm = uFilmThickness + (swirl - 0.45) * uThicknessVariation * 2.0 + gravityDrain;

          float cosTheta = clamp(0.88 - 0.25 * length(p - uPointer * 0.6), 0.35, 1.0);
          float opd = 2.0 * uRefractiveIndex * max(10.0, d_nm) * cosTheta;

          vec3 lambdas = vec3(650.0, 510.0, 475.0);
          // Include half-wave phase shift (+0.5) at the air-film boundary
          vec3 phase = 6.2831853 * (opd / lambdas + 0.5);
          vec3 refl = 0.5 + 0.5 * cos(phase);

          float fresnel = pow(1.0 - cosTheta * 0.75, 2.2);
          vec3 col = mix(vec3(0.03, 0.04, 0.07), refl * (0.75 + 0.85 * fresnel), 0.92);
          gl_FragColor = vec4(pow(max(col, 0.0), vec3(0.4545)), 1.0);
        } else {
          // Young's Double-Slit Interference Wavefront & Far-Field Screen Envelope
          float wavelength = mix(0.06, 0.24, (uFilmThickness - 120.0) / 760.0);
          float slitSeparation = mix(0.22, 1.05, (uThicknessVariation - 40.0) / 340.0);
          float k = 6.2831853 / wavelength;
          float omega = uTime * uFlowSpeed * 12.0;

          vec2 s1 = vec2(-0.78, slitSeparation * 0.5 + uPointer.y * 0.18);
          vec2 s2 = vec2(-0.78, -slitSeparation * 0.5 + uPointer.y * 0.18);

          float r1 = length(p - s1);
          float r2 = length(p - s2);

          float w1 = cos(k * r1 - omega) / sqrt(max(0.08, r1));
          float w2 = cos(k * r2 - omega) / sqrt(max(0.08, r2));
          float coherence = clamp((uRefractiveIndex - 1.1) / 0.65, 0.15, 1.0);

          float psi = w1 + coherence * w2;
          float intensity = (w1 * w1 + w2 * w2 + 2.0 * coherence * w1 * w2 * cos(k * (r1 - r2))) * 0.28;

          vec3 waveCol = vec3(0.04, 0.07, 0.14);
          waveCol += max(0.0, psi) * vec3(0.15, 0.72, 1.0) * 0.55;
          waveCol += max(0.0, -psi) * vec3(0.92, 0.28, 0.65) * 0.48;
          waveCol += intensity * vec3(0.95, 0.88, 0.55) * 0.45;

          // Slit barrier at x = -0.78 and detector screen at x = 0.82
          float barrier = smoothstep(0.018, 0.004, abs(p.x - (-0.78)));
          float gap1 = smoothstep(0.06, 0.02, abs(p.y - s1.y));
          float gap2 = smoothstep(0.06, 0.02, abs(p.y - s2.y));
          waveCol = mix(waveCol, vec3(0.75, 0.82, 0.92), barrier * (1.0 - max(gap1, gap2)));

          // Detector intensity curve on the right
          float fringe = pow(cos(3.14159 * slitSeparation * p.y / (wavelength * 1.55)), 2.0) * exp(-p.y * p.y * 1.2);
          float traceX = 0.62 + fringe * 0.35;
          float traceLine = smoothstep(0.022, 0.004, abs(p.x - traceX));
          waveCol += traceLine * vec3(1.0, 0.86, 0.32);

          gl_FragColor = vec4(pow(max(waveCol, 0.0), vec3(0.4545)), 1.0);
        }
      }
    `,
    depthWrite: false,
    depthTest: false,
  });

  const quad = new THREE.Mesh(new THREE.PlaneGeometry(2, 2), material);
  scene.add(quad);

  let raf = 0;
  let hudTick = 0;

  const animate = (now: number) => {
    raf = requestAnimationFrame(animate);
    const p = ctx.getParams();
    const isSlit = p.opticsMode === 'double-slit';

    uniforms.uTime.value = now * 0.001;
    uniforms.uResolution.value.set(Math.max(1, ctx.container.clientWidth), Math.max(1, ctx.container.clientHeight));
    uniforms.uMode.value = isSlit ? 1 : 0;
    uniforms.uFilmThickness.value = Number(p.filmThickness ?? 420);
    uniforms.uThicknessVariation.value = Number(p.thicknessVariation ?? 190);
    uniforms.uRefractiveIndex.value = Number(p.refractiveIndex ?? 1.33);
    uniforms.uFlowSpeed.value = Number(p.flowSpeed ?? 0.28);
    uniforms.uPointer.value.copy(orbit.state.pointerNdc);

    renderer.render(scene, camera);

    if (now - hudTick > 300) {
      hudTick = now;
      ctx.onHudUpdate(
        isSlit
          ? `杨氏双缝相干场 ψ = ψ₁ + ψ₂ · 相干度 ${(((uniforms.uRefractiveIndex.value - 1.1) / 0.65) * 100).toFixed(0)}%`
          : `纳米薄膜干涉 (650/510/475nm) · 平均膜厚 ${uniforms.uFilmThickness.value.toFixed(0)}nm · n=${uniforms.uRefractiveIndex.value.toFixed(2)}`,
      );
    }
  };

  raf = requestAnimationFrame(animate);

  return {
    dispose() {
      cancelAnimationFrame(raf);
      orbit.dispose();
      disposeStage();
    },
  };
}

// ============================================================================
// SCENE 051 + 054: Gravitational Lensing Einstein Ring & 48-Layer Aurora
// ============================================================================
export function mountAstroLensingAuroraScene(ctx: SceneRunContext): ActiveSceneHandle {
  const { renderer, scene, camera, disposeStage } = createThreeStage(ctx.container, 45);
  scene.background = new THREE.Color(0x020308);
  const orbit = attachOrbitCamera(renderer.domElement, camera);

  const uniforms = {
    uTime: { value: 0 },
    uResolution: { value: new THREE.Vector2(Math.max(1, ctx.container.clientWidth), Math.max(1, ctx.container.clientHeight)) },
    uAstroMode: { value: 0 },
    uEinsteinRadius: { value: 0.34 },
    uEllipticity: { value: 0.1 },
    uSourceOffset: { value: 0.04 },
    uExposure: { value: 1.15 },
    uPointer: { value: new THREE.Vector2(0, 0) },
  };

  const material = new THREE.ShaderMaterial({
    uniforms,
    vertexShader: FULLSCREEN_VERTEX,
    fragmentShader: /* glsl */ `
      precision highp float;
      varying vec2 vUv;
      uniform float uTime;
      uniform vec2 uResolution;
      uniform int uAstroMode;
      uniform float uEinsteinRadius;
      uniform float uEllipticity;
      uniform float uSourceOffset;
      uniform float uExposure;
      uniform vec2 uPointer;

      float hash21(vec2 p) {
        p = fract(p * vec2(234.34, 435.345));
        p += dot(p, p + 34.23);
        return fract(p.x * p.y);
      }

      float noise(vec2 p) {
        vec2 i = floor(p);
        vec2 f = fract(p);
        vec2 u = f * f * (3.0 - 2.0 * f);
        return mix(
          mix(hash21(i), hash21(i + vec2(1.0, 0.0)), u.x),
          mix(hash21(i + vec2(0.0, 1.0)), hash21(i + vec2(1.0, 1.0)), u.x),
          u.y
        );
      }

      void main() {
        vec2 theta = (vUv - 0.5) * 2.0;
        theta.x *= uResolution.x / max(1.0, uResolution.y);

        if (uAstroMode == 0) {
          // Gravitational Lensing: beta = theta - alpha(theta)
          vec2 lensCenter = uPointer * 0.28;
          vec2 rel = theta - lensCenter;
          vec2 qRel = vec2(rel.x * (1.0 - uEllipticity), rel.y * (1.0 + uEllipticity));
          float r2 = dot(qRel, qRel) + 0.0025;
          vec2 alpha = (uEinsteinRadius * uEinsteinRadius) * qRel / r2;

          // External shear
          alpha += uEllipticity * 0.35 * vec2(rel.x, -rel.y);
          vec2 beta = theta - alpha;

          // Background spiral galaxy in source plane (beta)
          vec2 srcCenter = vec2(uSourceOffset, uSourceOffset * 0.65);
          vec2 dSrc = beta - srcCenter;
          float rad = length(dSrc);
          float ang = atan(dSrc.y, dSrc.x);
          float spiral = 0.5 + 0.5 * cos(2.0 * ang - rad * 18.0 + uTime * 0.8);
          float bulge = exp(-rad * rad * 42.0);
          float disk = exp(-rad * 7.5) * (0.45 + 0.55 * spiral);

          vec3 col = vec3(0.01, 0.015, 0.03);
          col += bulge * vec3(1.0, 0.82, 0.52) * 1.6;
          col += disk * mix(vec3(0.22, 0.58, 1.0), vec3(0.85, 0.42, 0.95), spiral) * 1.35;

          // Foreground lens cD galaxy warm halo
          float lensHalo = exp(-dot(rel, rel) * 16.0);
          col += lensHalo * vec3(1.0, 0.68, 0.35) * 0.65;

          // Critical curve subtle indicator
          float crit = exp(-abs(length(qRel) - uEinsteinRadius) * 48.0);
          col += crit * vec3(0.35, 0.85, 1.0) * 0.22;

          col *= uExposure;
          col = col / (1.0 + col * 0.55);
          gl_FragColor = vec4(pow(max(col, 0.0), vec3(0.4545)), 1.0);
        } else {
          // 48-Layer Volumetric Aurora Borealis
          vec3 sky = mix(vec3(0.01, 0.02, 0.06), vec3(0.03, 0.08, 0.16), clamp(1.0 - vUv.y, 0.0, 1.0));
          vec3 aurora = vec3(0.0);

          for (int i = 0; i < 48; i++) {
            float fi = float(i) / 48.0;
            float z = 1.0 + fi * 3.2;
            vec2 samplePos = vec2(
              theta.x * z + sin(z * 2.2 + uTime * 0.45) * (0.28 + uEllipticity),
              theta.y + uSourceOffset
            );
            float curtain = sin(samplePos.x * 3.6 + noise(vec2(samplePos.x * 2.4, uTime * 0.35 + fi * 4.0)) * 4.5);
            float band = exp(-abs(curtain) * (6.5 - uEinsteinRadius * 5.0));
            float altMask = smoothstep(-0.35, 0.15, theta.y + fi * 0.55) * smoothstep(0.95, 0.15, theta.y);

            // Oxygen 557.7nm green at lower altitude -> Nitrogen/Oxygen 630nm crimson at high altitude
            vec3 emission = mix(
              vec3(0.12, 0.98, 0.52),
              vec3(0.78, 0.18, 0.88),
              clamp(fi * 1.15 + theta.y * 0.5, 0.0, 1.0)
            );
            aurora += emission * band * altMask * (0.065 * uEinsteinRadius * 3.0);
          }

          vec3 col = (sky + aurora) * uExposure;
          col = col / (1.0 + col * 0.55);
          gl_FragColor = vec4(pow(max(col, 0.0), vec3(0.4545)), 1.0);
        }
      }
    `,
    depthWrite: false,
    depthTest: false,
  });

  const quad = new THREE.Mesh(new THREE.PlaneGeometry(2, 2), material);
  scene.add(quad);

  let raf = 0;
  let hudTick = 0;

  const animate = (now: number) => {
    raf = requestAnimationFrame(animate);
    const p = ctx.getParams();
    const isAurora = p.astroMode === 'aurora';

    uniforms.uTime.value = now * 0.001;
    uniforms.uResolution.value.set(Math.max(1, ctx.container.clientWidth), Math.max(1, ctx.container.clientHeight));
    uniforms.uAstroMode.value = isAurora ? 1 : 0;
    uniforms.uEinsteinRadius.value = Number(p.einsteinRadius ?? 0.34);
    uniforms.uEllipticity.value = Number(p.ellipticity ?? 0.1);
    uniforms.uSourceOffset.value = Number(p.sourceOffset ?? 0.04);
    uniforms.uExposure.value = Number(p.exposure ?? 1.15);
    uniforms.uPointer.value.copy(orbit.state.pointerNdc);

    renderer.render(scene, camera);

    if (now - hudTick > 300) {
      hudTick = now;
      ctx.onHudUpdate(
        isAurora
          ? `48 层体积极光积分 · 氧原子 557.7nm 绿谱 / 氮分子紫红谱`
          : `广义相对论引力透镜 β = θ - α(θ) · 爱因斯坦半径 θE=${uniforms.uEinsteinRadius.value.toFixed(2)}`,
      );
    }
  };

  raf = requestAnimationFrame(animate);

  return {
    dispose() {
      cancelAnimationFrame(raf);
      orbit.dispose();
      disposeStage();
    },
  };
}
