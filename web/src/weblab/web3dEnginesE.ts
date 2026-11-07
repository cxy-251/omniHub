import * as THREE from 'three';
import {
  ActiveSceneHandle,
  SceneRunContext,
  attachOrbitCamera,
  createThreeStage,
  mountBlackHoleScene,
  mountToroidalFlowScene,
} from './web3dEnginesA';
import {
  mountChladniPlateScene,
  mountGpuEvolutionLifeScene,
  mountGrayScottScene,
  mountVerletClothScene,
} from './web3dEnginesB';
import {
  mountAutonomousSnakeScene,
  mountBoidsFlockingScene,
} from './web3dEnginesC';
import {
  mountAstroLensingAuroraScene,
  mountMoltenReliefScene,
  mountSdfFerrofluidScene,
  mountThinFilmOpticsScene,
} from './web3dEnginesD';

// ============================================================================
// SCENE 036 + 053: Cosmic Web Dark-Matter Filaments & Strange Attractors (RK4)
// ============================================================================
export function mountCosmicAttractorScene(ctx: SceneRunContext): ActiveSceneHandle {
  const { renderer, scene, camera, disposeStage } = createThreeStage(ctx.container, 46);
  scene.background = new THREE.Color('#02040a');
  const orbit = attachOrbitCamera(renderer.domElement, camera, {
    yaw: 0.35,
    pitch: 0.24,
    distance: 14.5,
    minDistance: 5.5,
    maxDistance: 28,
  });

  const root = new THREE.Group();
  scene.add(root);

  let builtKey = '';

  const clearRoot = () => {
    while (root.children.length > 0) {
      const child = root.children[0] as THREE.Mesh;
      root.remove(child);
      child.geometry?.dispose();
      if (Array.isArray(child.material)) child.material.forEach((m) => m.dispose());
      else child.material?.dispose();
    }
  };

  let sliceRing: THREE.Mesh | null = null;
  let tracerMeshes: { mesh: THREE.Mesh; pts: THREE.Vector3[]; speed: number; phase: number }[] = [];

  const buildScene = (mode: string, chaos: number, pointSize: number) => {
    clearRoot();
    sliceRing = null;
    tracerMeshes = [];

    if (mode === 'cosmic-web') {
      const NODES = 38;
      const nodes: THREE.Vector3[] = [];
      for (let i = 0; i < NODES; i++) {
        const a = i * 2.3999632;
        const y = 1 - (i / Math.max(1, NODES - 1)) * 2;
        const r = Math.sqrt(Math.max(0, 1 - y * y));
        const rad = 2.8 + ((i * 73) % 19) * 0.16;
        nodes.push(new THREE.Vector3(Math.cos(a) * r * rad, y * rad * 0.82, Math.sin(a) * r * rad));
      }

      const linePositions: number[] = [];
      const lineColors: number[] = [];
      const cA = new THREE.Color('#38bdf8');
      const cB = new THREE.Color('#a855f7');

      for (let i = 0; i < NODES; i++) {
        for (let j = i + 1; j < NODES; j++) {
          const dist = nodes[i].distanceTo(nodes[j]);
          if (dist < 4.2 + chaos * 1.4) {
            const mid = nodes[i]
              .clone()
              .add(nodes[j])
              .multiplyScalar(0.5)
              .multiplyScalar(0.84);
            const curve = new THREE.QuadraticBezierCurve3(nodes[i], mid, nodes[j]);
            const pts = curve.getPoints(18);
            for (let k = 0; k < pts.length - 1; k++) {
              const t0 = k / (pts.length - 1);
              const t1 = (k + 1) / (pts.length - 1);
              linePositions.push(pts[k].x, pts[k].y, pts[k].z, pts[k + 1].x, pts[k + 1].y, pts[k + 1].z);
              const col0 = cA.clone().lerp(cB, t0);
              const col1 = cA.clone().lerp(cB, t1);
              lineColors.push(col0.r, col0.g, col0.b, col1.r, col1.g, col1.b);
            }
          }
        }
      }

      const lineGeo = new THREE.BufferGeometry();
      lineGeo.setAttribute('position', new THREE.Float32BufferAttribute(linePositions, 3));
      lineGeo.setAttribute('color', new THREE.Float32BufferAttribute(lineColors, 3));
      const lineMat = new THREE.LineBasicMaterial({
        vertexColors: true,
        transparent: true,
        opacity: 0.45,
        blending: THREE.AdditiveBlending,
      });
      root.add(new THREE.LineSegments(lineGeo, lineMat));

      const haloCount = 3200;
      const haloPos = new Float32Array(haloCount * 3);
      const haloCol = new Float32Array(haloCount * 3);
      for (let i = 0; i < haloCount; i++) {
        const node = nodes[i % NODES];
        const spread = 0.25 + ((i * 29) % 100) * 0.012 * (0.6 + chaos);
        const theta = (i * 1.6180339) * Math.PI * 2;
        const phi = Math.acos(1 - 2 * (((i * 97) % 500) / 500));
        haloPos[i * 3] = node.x + Math.sin(phi) * Math.cos(theta) * spread;
        haloPos[i * 3 + 1] = node.y + Math.cos(phi) * spread;
        haloPos[i * 3 + 2] = node.z + Math.sin(phi) * Math.sin(theta) * spread;
        const c = i % 5 === 0 ? new THREE.Color('#fde047') : new THREE.Color('#60a5fa');
        haloCol[i * 3] = c.r;
        haloCol[i * 3 + 1] = c.g;
        haloCol[i * 3 + 2] = c.b;
      }
      const ptGeo = new THREE.BufferGeometry();
      ptGeo.setAttribute('position', new THREE.BufferAttribute(haloPos, 3));
      ptGeo.setAttribute('color', new THREE.BufferAttribute(haloCol, 3));
      const ptMat = new THREE.PointsMaterial({
        size: pointSize * 0.045,
        vertexColors: true,
        transparent: true,
        opacity: 0.88,
        blending: THREE.AdditiveBlending,
      });
      root.add(new THREE.Points(ptGeo, ptMat));

      const ringGeo = new THREE.RingGeometry(4.6, 4.78, 64);
      const ringMat = new THREE.MeshBasicMaterial({
        color: '#22d3ee',
        side: THREE.DoubleSide,
        transparent: true,
        opacity: 0.52,
      });
      sliceRing = new THREE.Mesh(ringGeo, ringMat);
      root.add(sliceRing);
    } else {
      // Strange Attractor RK4 multi-trajectory bundle
      const TRAJ = 6;
      const STEPS = 2200;
      const palette = ['#38bdf8', '#f472b6', '#a855f7', '#34d399', '#fbbf24', '#fb7185'];

      const deriv = (p: THREE.Vector3): THREE.Vector3 => {
        if (mode === 'rossler') {
          const a = 0.2;
          const b = 0.2;
          const c = 5.2 + chaos * 1.4;
          return new THREE.Vector3(-p.y - p.z, p.x + a * p.y, b + p.z * (p.x - c));
        }
        if (mode === 'thomas') {
          const b = 0.16 + (1 - chaos) * 0.06;
          return new THREE.Vector3(
            Math.sin(p.y) - b * p.x,
            Math.sin(p.z) - b * p.y,
            Math.sin(p.x) - b * p.z,
          );
        }
        // Lorenz default
        const sigma = 10;
        const rho = 24 + chaos * 8;
        const beta = 8 / 3;
        return new THREE.Vector3(
          sigma * (p.y - p.x),
          p.x * (rho - p.z) - p.y,
          p.x * p.y - beta * p.z,
        );
      };

      const dt = mode === 'thomas' ? 0.065 : mode === 'rossler' ? 0.022 : 0.0065;
      const scale = mode === 'thomas' ? 1.25 : mode === 'rossler' ? 0.34 : 0.165;
      const offset = mode === 'lorenz' ? new THREE.Vector3(0, 0, -25) : new THREE.Vector3(0, 0, 0);

      for (let t = 0; t < TRAJ; t++) {
        let cur = new THREE.Vector3(0.1 + t * 0.015, 0.1 - t * 0.01, 0.1 + t * 0.012);
        const pts: THREE.Vector3[] = [];
        for (let s = 0; s < STEPS; s++) {
          const k1 = deriv(cur);
          const k2 = deriv(cur.clone().addScaledVector(k1, dt * 0.5));
          const k3 = deriv(cur.clone().addScaledVector(k2, dt * 0.5));
          const k4 = deriv(cur.clone().addScaledVector(k3, dt));
          cur = cur
            .clone()
            .addScaledVector(k1, dt / 6)
            .addScaledVector(k2, dt / 3)
            .addScaledVector(k3, dt / 3)
            .addScaledVector(k4, dt / 6);
          pts.push(cur.clone().add(offset).multiplyScalar(scale));
        }

        const lineGeo = new THREE.BufferGeometry().setFromPoints(pts);
        const lineMat = new THREE.LineBasicMaterial({
          color: palette[t % palette.length],
          transparent: true,
          opacity: 0.72,
          blending: THREE.AdditiveBlending,
        });
        root.add(new THREE.Line(lineGeo, lineMat));

        const sphereGeo = new THREE.SphereGeometry(0.12 * (pointSize / 3.2), 14, 14);
        const sphereMat = new THREE.MeshBasicMaterial({ color: palette[t % palette.length] });
        const tracer = new THREE.Mesh(sphereGeo, sphereMat);
        root.add(tracer);
        tracerMeshes.push({ mesh: tracer, pts, speed: 120 + t * 18, phase: t * 260 });
      }
    }
  };

  let raf = 0;
  let lastNow = performance.now();
  let hudTick = 0;

  const animate = (now: number) => {
    raf = requestAnimationFrame(animate);
    const dt = Math.min((now - lastNow) * 0.001, 0.05);
    lastNow = now;

    const p = ctx.getParams();
    const fieldMode = String(p.fieldMode ?? 'cosmic-web');
    const chaos = Number(p.chaos ?? 0.5);
    const scanSpeed = Number(p.scanSpeed ?? 0.55);
    const pointSize = Number(p.pointSize ?? 3.2);
    const rotationSpeed = Number(p.rotationSpeed ?? 0.18);

    const key = `${fieldMode}:${chaos.toFixed(2)}:${pointSize.toFixed(1)}`;
    if (key !== builtKey) {
      builtKey = key;
      buildScene(fieldMode, chaos, pointSize);
    }

    orbit.step(dt, rotationSpeed);

    if (sliceRing) {
      sliceRing.position.z = Math.sin(now * 0.0015 * scanSpeed) * 3.4;
    }

    for (const tr of tracerMeshes) {
      const idx = Math.floor(now * 0.001 * tr.speed * scanSpeed + tr.phase) % tr.pts.length;
      tr.mesh.position.copy(tr.pts[idx]);
    }

    renderer.render(scene, camera);

    if (now - hudTick > 300) {
      hudTick = now;
      ctx.onHudUpdate(
        fieldMode === 'cosmic-web'
          ? `大尺度暗物质纤维网 · 38 星系团节点 · 3,200 暗晕粒子 · 层析扫描中`
          : `RK4 奇异吸引子 (${fieldMode.toUpperCase()}) · 6 条邻近初值轨迹 · 2,200 积分步/轨`,
      );
    }
  };

  raf = requestAnimationFrame(animate);

  return {
    dispose() {
      cancelAnimationFrame(raf);
      clearRoot();
      orbit.dispose();
      disposeStage();
    },
  };
}

// ============================================================================
// SCENE 059 + 011: Golden-Angle Parastichy Sphere & 4-State Particle Morphing
// ============================================================================
export function mountFibonacciMorphScene(ctx: SceneRunContext): ActiveSceneHandle {
  const { renderer, scene, camera, disposeStage } = createThreeStage(ctx.container, 45);
  scene.background = new THREE.Color('#03050b');
  const orbit = attachOrbitCamera(renderer.domElement, camera, {
    yaw: 0.4,
    pitch: 0.22,
    distance: 8.8,
    minDistance: 4.0,
    maxDistance: 18.0,
  });

  const root = new THREE.Group();
  scene.add(root);

  let builtKey = '';
  let morphTargetState = 0;
  let morphProgress = 0;

  const onCanvasClick = () => {
    morphTargetState = (morphTargetState + 1) % 4;
  };
  renderer.domElement.addEventListener('click', onCanvasClick);

  const clearRoot = () => {
    while (root.children.length > 0) {
      const child = root.children[0] as THREE.Mesh;
      root.remove(child);
      child.geometry?.dispose();
      if (Array.isArray(child.material)) child.material.forEach((m) => m.dispose());
      else child.material?.dispose();
    }
  };

  let morphPoints: THREE.Points | null = null;
  let morphBank: Float32Array[] = [];

  const buildScene = (subMode: string, pairStr: string, sampleCount: number, lineOpacity: number, pointSize: number) => {
    clearRoot();
    morphPoints = null;
    morphBank = [];

    const GOLDEN_ANGLE = Math.PI * (3 - Math.sqrt(5)); // ~2.3999632297 rad (137.50776 deg)

    if (subMode === 'parastichy') {
      const N = Math.max(200, Math.min(2400, Math.round(sampleCount)));
      const [sA, sB] = pairStr.split('/').map((v) => parseInt(v, 10) || 21);
      const R = 3.2;

      const pts: THREE.Vector3[] = [];
      const posArr = new Float32Array(N * 3);
      const colArr = new Float32Array(N * 3);
      const cTop = new THREE.Color('#38bdf8');
      const cBot = new THREE.Color('#f472b6');

      for (let i = 0; i < N; i++) {
        const y = 1 - (i / (N - 1)) * 2;
        const r = Math.sqrt(Math.max(0, 1 - y * y));
        const theta = i * GOLDEN_ANGLE;
        const vx = Math.cos(theta) * r * R;
        const vy = y * R;
        const vz = Math.sin(theta) * r * R;
        pts.push(new THREE.Vector3(vx, vy, vz));
        posArr[i * 3] = vx;
        posArr[i * 3 + 1] = vy;
        posArr[i * 3 + 2] = vz;

        const c = cTop.clone().lerp(cBot, i / (N - 1));
        colArr[i * 3] = c.r;
        colArr[i * 3 + 1] = c.g;
        colArr[i * 3 + 2] = c.b;
      }

      const ptGeo = new THREE.BufferGeometry();
      ptGeo.setAttribute('position', new THREE.BufferAttribute(posArr, 3));
      ptGeo.setAttribute('color', new THREE.BufferAttribute(colArr, 3));
      const ptMat = new THREE.PointsMaterial({
        size: pointSize * 0.032,
        vertexColors: true,
        transparent: true,
        opacity: 0.95,
        blending: THREE.AdditiveBlending,
      });
      root.add(new THREE.Points(ptGeo, ptMat));

      // Parastichy family A (step sA) & family B (step sB)
      const buildSpiralFamily = (step: number, colorHex: string) => {
        const segs: number[] = [];
        for (let i = 0; i + step < N; i++) {
          const p0 = pts[i];
          const p1 = pts[i + step];
          segs.push(p0.x, p0.y, p0.z, p1.x, p1.y, p1.z);
        }
        const geo = new THREE.BufferGeometry();
        geo.setAttribute('position', new THREE.Float32BufferAttribute(segs, 3));
        const mat = new THREE.LineBasicMaterial({
          color: colorHex,
          transparent: true,
          opacity: lineOpacity,
          blending: THREE.AdditiveBlending,
        });
        root.add(new THREE.LineSegments(geo, mat));
      };

      buildSpiralFamily(sA, '#22d3ee');
      buildSpiralFamily(sB, '#f59e0b');
    } else {
      // 16,000-particle 4-state topological morphing field
      const M = 16000;
      const stateSphere = new Float32Array(M * 3);
      const stateTorus = new Float32Array(M * 3);
      const stateGalaxy = new Float32Array(M * 3);
      const stateWave = new Float32Array(M * 3);
      const colors = new Float32Array(M * 3);

      for (let i = 0; i < M; i++) {
        const u = i / M;
        const y = 1 - u * 2;
        const r = Math.sqrt(Math.max(0, 1 - y * y));
        const th = i * GOLDEN_ANGLE;

        // 0: Fibonacci Sphere
        stateSphere[i * 3] = Math.cos(th) * r * 3.2;
        stateSphere[i * 3 + 1] = y * 3.2;
        stateSphere[i * 3 + 2] = Math.sin(th) * r * 3.2;

        // 1: knotted Torus
        const tu = u * Math.PI * 2 * 13;
        const tv = (i % 128) / 128 * Math.PI * 2;
        const Rmaj = 2.55;
        const Rmin = 0.95;
        stateTorus[i * 3] = (Rmaj + Rmin * Math.cos(tv)) * Math.cos(tu);
        stateTorus[i * 3 + 1] = Rmin * Math.sin(tv);
        stateTorus[i * 3 + 2] = (Rmaj + Rmin * Math.cos(tv)) * Math.sin(tu);

        // 2: Spiral Galaxy
        const arm = i % 3;
        const rad = Math.pow(u, 0.65) * 4.2;
        const ang = rad * 2.4 + (arm * Math.PI * 2) / 3 + ((i % 17) - 8) * 0.04;
        stateGalaxy[i * 3] = Math.cos(ang) * rad;
        stateGalaxy[i * 3 + 1] = Math.sin(rad * 3.0 - i * 0.1) * 0.25 * (1 - u * 0.7);
        stateGalaxy[i * 3 + 2] = Math.sin(ang) * rad;

        // 3: Interference Wave Lattice
        const gx = ((i % 128) / 127 - 0.5) * 7.2;
        const gz = (Math.floor(i / 128) / 125 - 0.5) * 7.2;
        const gy = Math.sin(gx * 1.6) * Math.cos(gz * 1.6) * 1.1;
        stateWave[i * 3] = gx;
        stateWave[i * 3 + 1] = gy;
        stateWave[i * 3 + 2] = gz;

        const c = new THREE.Color().setHSL(0.54 + u * 0.32, 0.88, 0.62);
        colors[i * 3] = c.r;
        colors[i * 3 + 1] = c.g;
        colors[i * 3 + 2] = c.b;
      }

      morphBank = [stateSphere, stateTorus, stateGalaxy, stateWave];
      const livePos = new Float32Array(stateSphere);
      const geo = new THREE.BufferGeometry();
      geo.setAttribute('position', new THREE.BufferAttribute(livePos, 3));
      geo.setAttribute('color', new THREE.BufferAttribute(colors, 3));
      const mat = new THREE.PointsMaterial({
        size: pointSize * 0.025,
        vertexColors: true,
        transparent: true,
        opacity: 0.9,
        blending: THREE.AdditiveBlending,
      });
      morphPoints = new THREE.Points(geo, mat);
      root.add(morphPoints);
    }
  };

  let raf = 0;
  let lastNow = performance.now();
  let hudTick = 0;

  const animate = (now: number) => {
    raf = requestAnimationFrame(animate);
    const dt = Math.min((now - lastNow) * 0.001, 0.05);
    lastNow = now;

    const p = ctx.getParams();
    const subMode = String(p.subMode ?? 'parastichy');
    const fibonacciPair = String(p.fibonacciPair ?? '21/34');
    const sampleCount = Number(p.sampleCount ?? 1200);
    const lineOpacity = Number(p.lineOpacity ?? 0.34);
    const flowSpeed = Number(p.flowSpeed ?? 0.38);
    const pointSize = Number(p.pointSize ?? 3.8);

    const key = `${subMode}:${fibonacciPair}:${sampleCount}:${lineOpacity.toFixed(2)}:${pointSize.toFixed(1)}`;
    if (key !== builtKey) {
      builtKey = key;
      buildScene(subMode, fibonacciPair, sampleCount, lineOpacity, pointSize);
    }

    orbit.step(dt, flowSpeed * 0.45);

    if (morphPoints && morphBank.length === 4) {
      morphProgress += (morphTargetState - morphProgress) * Math.min(1, dt * 3.5);
      const idxA = Math.floor(((morphProgress % 4) + 4) % 4);
      const idxB = (idxA + 1) % 4;
      const frac = morphProgress - Math.floor(morphProgress);
      const posAttr = morphPoints.geometry.getAttribute('position') as THREE.BufferAttribute;
      const arr = posAttr.array as Float32Array;
      const bankA = morphBank[idxA];
      const bankB = morphBank[idxB];
      for (let i = 0; i < arr.length; i++) {
        arr[i] = bankA[i] + (bankB[i] - bankA[i]) * frac;
      }
      posAttr.needsUpdate = true;
    }

    renderer.render(scene, camera);

    if (now - hudTick > 300) {
      hudTick = now;
      const stateNames = ['球面叶序 (Sphere)', '环面纽结 (Torus)', '三臂旋涡星系 (Galaxy)', '驻波晶格 (Wave)'];
      ctx.onHudUpdate(
        subMode === 'parastichy'
          ? `黄金分割角 137.508° · 螺旋族 ${fibonacciPair} · 采样点 ${sampleCount}`
          : `四态粒子拓扑场 · 当前形态: ${stateNames[morphTargetState]} (点击画布切换下一形态)`,
      );
    }
  };

  raf = requestAnimationFrame(animate);

  return {
    dispose() {
      cancelAnimationFrame(raf);
      renderer.domElement.removeEventListener('click', onCanvasClick);
      clearRoot();
      orbit.dispose();
      disposeStage();
    },
  };
}

// ============================================================================
// SCENE 038 + 057: 3D Marble Music Box (WebAudio) & Sum of Squares Visual Proof
// ============================================================================
export function mountMarbleProofScene(ctx: SceneRunContext): ActiveSceneHandle {
  const { renderer, scene, camera, disposeStage } = createThreeStage(ctx.container, 44);
  scene.background = new THREE.Color('#05070f');
  const orbit = attachOrbitCamera(renderer.domElement, camera, {
    yaw: 0.55,
    pitch: 0.35,
    distance: 12.5,
    minDistance: 5.5,
    maxDistance: 24.0,
  });

  const ambient = new THREE.AmbientLight('#94a3b8', 0.85);
  const dirLight = new THREE.DirectionalLight('#f8fafc', 1.45);
  dirLight.position.set(6, 10, 7);
  scene.add(ambient, dirLight);

  const root = new THREE.Group();
  scene.add(root);

  let builtKey = '';
  let railCurve: THREE.CatmullRomCurve3 | null = null;
  let marbleMeshes: THREE.Mesh[] = [];
  let notePads: { mesh: THREE.Mesh; u: number; freq: number; glow: number }[] = [];
  let lastMarbleU: number[] = [];

  const clearRoot = () => {
    while (root.children.length > 0) {
      const child = root.children[0] as THREE.Mesh;
      root.remove(child);
      child.geometry?.dispose();
      if (Array.isArray(child.material)) child.material.forEach((m) => m.dispose());
      else child.material?.dispose();
    }
  };

  const buildScene = (stageMode: string, marblesCount: number, explodeShell: number, proofStep: number) => {
    clearRoot();
    railCurve = null;
    marbleMeshes = [];
    notePads = [];
    lastMarbleU = [];

    if (stageMode === 'marble') {
      const controlPts: THREE.Vector3[] = [];
      const N = 12;
      for (let i = 0; i < N; i++) {
        const a = (i / N) * Math.PI * 2;
        const r = 3.8 + 0.65 * Math.sin(a * 3);
        const y = Math.cos(a * 2) * 1.45 + Math.sin(a) * 0.55;
        controlPts.push(new THREE.Vector3(Math.cos(a) * r, y, Math.sin(a) * r));
      }
      railCurve = new THREE.CatmullRomCurve3(controlPts, true, 'catmullrom', 0.5);

      const tubeGeo = new THREE.TubeGeometry(railCurve, 180, 0.085, 12, true);
      const tubeMat = new THREE.MeshStandardMaterial({
        color: '#38bdf8',
        metalness: 0.75,
        roughness: 0.25,
      });
      root.add(new THREE.Mesh(tubeGeo, tubeMat));

      // 6 pentatonic bell bars (C4, D4, E4, G4, A4, C5)
      const freqs = [261.63, 293.66, 329.63, 392.0, 440.0, 523.25];
      const colors = ['#f472b6', '#fb923c', '#facc15', '#34d399', '#38bdf8', '#a855f7'];

      for (let i = 0; i < 6; i++) {
        const u = i / 6;
        const pt = railCurve.getPointAt(u);
        const tan = railCurve.getTangentAt(u);
        const padGeo = new THREE.BoxGeometry(0.95, 0.14, 0.42);
        const padMat = new THREE.MeshStandardMaterial({
          color: colors[i],
          emissive: colors[i],
          emissiveIntensity: 0.25,
          metalness: 0.4,
          roughness: 0.3,
        });
        const pad = new THREE.Mesh(padGeo, padMat);
        pad.position.copy(pt).add(new THREE.Vector3(0, -0.24, 0));
        pad.lookAt(pt.clone().add(tan));
        root.add(pad);
        notePads.push({ mesh: pad, u, freq: freqs[i], glow: 0 });
      }

      const mCount = Math.max(1, Math.min(8, Math.round(marblesCount)));
      for (let i = 0; i < mCount; i++) {
        const mGeo = new THREE.SphereGeometry(0.22, 20, 20);
        const mMat = new THREE.MeshStandardMaterial({
          color: '#f8fafc',
          emissive: '#38bdf8',
          emissiveIntensity: 0.35,
          metalness: 0.9,
          roughness: 0.1,
        });
        const mMesh = new THREE.Mesh(mGeo, mMat);
        root.add(mMesh);
        marbleMeshes.push(mMesh);
        lastMarbleU.push((i / mCount) % 1);
      }
    } else {
      // Visual proof of (k+1)^3 - k^3 = 3k^2 + 3k + 1 and sum_{k=1}^n k^2
      const n = Math.max(2, Math.min(7, Math.round(marblesCount)));
      const boxGeo = new THREE.BoxGeometry(0.62, 0.62, 0.62);
      const shellColors = ['#38bdf8', '#a855f7', '#f472b6', '#fb923c', '#facc15', '#34d399', '#60a5fa'];

      for (let k = 1; k <= n; k++) {
        const col = shellColors[(k - 1) % shellColors.length];
        for (let x = 0; x < k; x++) {
          for (let z = 0; z < k; z++) {
            const mat = new THREE.MeshStandardMaterial({
              color: col,
              metalness: 0.25,
              roughness: 0.35,
            });
            const cube = new THREE.Mesh(boxGeo, mat);
            const explodeOffset = (k - 1) * explodeShell * 0.55;
            if (proofStep === 0) {
              // Step pyramid of squares k x k
              cube.position.set(
                (x - (k - 1) * 0.5) * 0.68,
                (n - k) * (0.68 + explodeShell * 0.35) - 1.5,
                (z - (k - 1) * 0.5) * 0.68,
              );
            } else {
              // 3 copies of k^2 arranged as the 3 orthogonal faces of a (k+1)^3 cube shell
              cube.position.set(
                (x - n * 0.5) * 0.68 + explodeOffset,
                (k - n * 0.5) * 0.68 + (proofStep >= 2 ? explodeOffset * 0.6 : 0),
                (z - n * 0.5) * 0.68,
              );
            }
            root.add(cube);
          }
        }
      }
    }
  };

  let raf = 0;
  let lastNow = performance.now();
  let hudTick = 0;
  let railPhase = 0;

  const animate = (now: number) => {
    raf = requestAnimationFrame(animate);
    const dt = Math.min((now - lastNow) * 0.001, 0.05);
    lastNow = now;

    const p = ctx.getParams();
    const stageMode = String(p.stageMode ?? 'marble');
    const tempo = Number(p.tempo ?? 112);
    const marbles = Number(p.marbles ?? 4);
    const explodeShell = Number(p.explodeShell ?? 0.45);
    const proofStep = Number(p.proofStep ?? 1);

    const key = `${stageMode}:${marbles}:${explodeShell.toFixed(2)}:${proofStep}`;
    if (key !== builtKey) {
      builtKey = key;
      buildScene(stageMode, marbles, explodeShell, proofStep);
    }

    orbit.step(dt, 0.15);

    if (stageMode === 'marble' && railCurve) {
      const cycleSpeed = (tempo / 120) * 0.14;
      railPhase = (railPhase + dt * cycleSpeed) % 1;

      for (let i = 0; i < marbleMeshes.length; i++) {
        const prevU = lastMarbleU[i];
        const curU = (railPhase + i / marbleMeshes.length) % 1;
        const pt = railCurve.getPointAt(curU);
        marbleMeshes[i].position.copy(pt);

        for (const pad of notePads) {
          const crossed =
            prevU <= curU
              ? prevU < pad.u && curU >= pad.u
              : prevU < pad.u || curU >= pad.u;
          if (crossed) {
            pad.glow = 1.0;
            ctx.triggerBellNote(pad.freq, 0.24);
          }
        }
        lastMarbleU[i] = curU;
      }

      for (const pad of notePads) {
        pad.glow = Math.max(0, pad.glow - dt * 2.8);
        const mat = pad.mesh.material as THREE.MeshStandardMaterial;
        mat.emissiveIntensity = 0.22 + pad.glow * (1.2 + explodeShell);
        pad.mesh.scale.y = 1 + pad.glow * 0.45;
      }
    }

    renderer.render(scene, camera);

    if (now - hudTick > 300) {
      hudTick = now;
      const n = Math.max(2, Math.min(7, Math.round(marbles)));
      const sumSq = (n * (n + 1) * (2 * n + 1)) / 6;
      ctx.onHudUpdate(
        stageMode === 'marble'
          ? `双轨闭合样条八音盒 · ${tempo} BPM · ${marbleMeshes.length} 颗钢珠 (开启顶部合成器可听钟声)`
          : `平方和几何证明 ∑k² (1..${n}) = ${sumSq} · 恒等式 n(n+1)(2n+1)/6`,
      );
    }
  };

  raf = requestAnimationFrame(animate);

  return {
    dispose() {
      cancelAnimationFrame(raf);
      clearRoot();
      orbit.dispose();
      disposeStage();
    },
  };
}

// ============================================================================
// SCENE 046 + 025: ALife Genetic Natural Selection & 3D Depth-Sorted ASCII
// ============================================================================
export function mountAlifeAsciiScene(ctx: SceneRunContext): ActiveSceneHandle {
  const canvas = document.createElement('canvas');
  canvas.style.display = 'block';
  canvas.style.width = '100%';
  canvas.style.height = '100%';
  canvas.style.touchAction = 'none';
  ctx.container.innerHTML = '';
  ctx.container.appendChild(canvas);

  const g = canvas.getContext('2d')!;
  let width = Math.max(1, ctx.container.clientWidth);
  let height = Math.max(1, ctx.container.clientHeight);

  const resize = () => {
    const dpr = Math.min(window.devicePixelRatio || 1, 2);
    width = Math.max(1, ctx.container.clientWidth);
    height = Math.max(1, ctx.container.clientHeight);
    canvas.width = Math.floor(width * dpr);
    canvas.height = Math.floor(height * dpr);
    g.setTransform(dpr, 0, 0, dpr, 0, 0);
  };
  resize();
  const observer = new ResizeObserver(resize);
  observer.observe(ctx.container);

  interface Microbe {
    x: number;
    y: number;
    vx: number;
    vy: number;
    speedGene: number;
    senseGene: number;
    energy: number;
    gen: number;
  }

  let microbes: Microbe[] = [];
  let foods: { x: number; y: number }[] = [];
  let maxGen = 1;

  const initEcosystem = (popCount: number) => {
    microbes = [];
    foods = [];
    maxGen = 1;
    for (let i = 0; i < popCount; i++) {
      const ang = Math.random() * Math.PI * 2;
      const spd = 1.2 + Math.random() * 1.8;
      microbes.push({
        x: Math.random() * width,
        y: Math.random() * height,
        vx: Math.cos(ang) * spd,
        vy: Math.sin(ang) * spd,
        speedGene: spd,
        senseGene: 36 + Math.random() * 64,
        energy: 75 + Math.random() * 40,
        gen: 1,
      });
    }
    for (let i = 0; i < 95; i++) {
      foods.push({ x: Math.random() * width, y: Math.random() * height });
    }
  };

  initEcosystem(120);

  let pointerDown = false;
  let pointerX = width * 0.5;
  let pointerY = height * 0.5;
  let asciiYaw = 0.4;
  let asciiPitch = 0.25;

  const onDown = (e: PointerEvent) => {
    pointerDown = true;
    const r = canvas.getBoundingClientRect();
    pointerX = e.clientX - r.left;
    pointerY = e.clientY - r.top;
  };
  const onMove = (e: PointerEvent) => {
    const r = canvas.getBoundingClientRect();
    const nx = e.clientX - r.left;
    const ny = e.clientY - r.top;
    if (pointerDown) {
      asciiYaw += (nx - pointerX) * 0.008;
      asciiPitch = Math.max(-1.2, Math.min(1.2, asciiPitch + (ny - pointerY) * 0.008));
    }
    pointerX = nx;
    pointerY = ny;
  };
  const onUp = () => {
    pointerDown = false;
  };

  canvas.addEventListener('pointerdown', onDown);
  canvas.addEventListener('pointermove', onMove);
  window.addEventListener('pointerup', onUp);

  let raf = 0;
  let hudTick = 0;

  const animate = (now: number) => {
    raf = requestAnimationFrame(animate);
    const p = ctx.getParams();
    const canvasMode = String(p.canvasMode ?? 'alife');
    const population = Number(p.population ?? 160);
    const foodDensity = Number(p.foodDensity ?? 0.65);
    const mutationRate = Number(p.mutationRate ?? 0.15);
    const selectionPressure = Number(p.selectionPressure ?? 0.58);

    if (canvasMode === 'alife') {
      if (pointerDown && foods.length < 280) {
        for (let k = 0; k < 3; k++) {
          foods.push({
            x: pointerX + (Math.random() - 0.5) * 36,
            y: pointerY + (Math.random() - 0.5) * 36,
          });
        }
      }

      if (Math.random() < foodDensity && foods.length < 220) {
        foods.push({ x: Math.random() * width, y: Math.random() * height });
      }

      g.fillStyle = 'rgba(4, 8, 18, 0.34)';
      g.fillRect(0, 0, width, height);

      // Draw food
      g.fillStyle = '#34d399';
      for (const f of foods) {
        g.beginPath();
        g.arc(f.x, f.y, 2.4, 0, Math.PI * 2);
        g.fill();
      }

      const nextMicrobes: Microbe[] = [];
      let speedSum = 0;
      let senseSum = 0;

      for (const m of microbes) {
        let bestIdx = -1;
        let bestDist2 = m.senseGene * m.senseGene;
        for (let i = 0; i < foods.length; i++) {
          const dx = foods[i].x - m.x;
          const dy = foods[i].y - m.y;
          const d2 = dx * dx + dy * dy;
          if (d2 < bestDist2) {
            bestDist2 = d2;
            bestIdx = i;
          }
        }

        if (bestIdx >= 0) {
          const target = foods[bestIdx];
          const ang = Math.atan2(target.y - m.y, target.x - m.x);
          m.vx += (Math.cos(ang) * m.speedGene - m.vx) * 0.16;
          m.vy += (Math.sin(ang) * m.speedGene - m.vy) * 0.16;
          if (bestDist2 < 42) {
            m.energy += 34;
            foods.splice(bestIdx, 1);
          }
        }

        m.x = (m.x + m.vx + width) % width;
        m.y = (m.y + m.vy + height) % height;

        // Explicit metabolic cost proportional to speed^2 and sensing radius
        const cost = (0.08 + m.speedGene * m.speedGene * 0.045 + m.senseGene * 0.0012) * selectionPressure;
        m.energy -= cost;

        if (m.energy > 145 && microbes.length + nextMicrobes.length < Math.max(60, population * 1.6)) {
          m.energy *= 0.5;
          const mut = () => (Math.random() - 0.5) * 2 * mutationRate;
          const childSpeed = Math.max(0.6, Math.min(4.2, m.speedGene * (1 + mut())));
          const childSense = Math.max(18, Math.min(140, m.senseGene * (1 + mut())));
          const childGen = m.gen + 1;
          if (childGen > maxGen) maxGen = childGen;
          nextMicrobes.push({
            x: m.x + (Math.random() - 0.5) * 8,
            y: m.y + (Math.random() - 0.5) * 8,
            vx: -m.vx,
            vy: -m.vy,
            speedGene: childSpeed,
            senseGene: childSense,
            energy: m.energy,
            gen: childGen,
          });
        }

        if (m.energy > 0) {
          nextMicrobes.push(m);
          speedSum += m.speedGene;
          senseSum += m.senseGene;

          const hue = Math.max(175, Math.min(330, 180 + (m.speedGene - 1.0) * 52));
          g.fillStyle = `hsl(${hue}, 88%, 62%)`;
          g.beginPath();
          g.arc(m.x, m.y, 3.6, 0, Math.PI * 2);
          g.fill();
        }
      }

      microbes = nextMicrobes;
      if (microbes.length < 16) {
        initEcosystem(Math.round(population * 0.65));
      }

      if (now - hudTick > 300) {
        hudTick = now;
        const avgSpd = microbes.length ? speedSum / microbes.length : 0;
        const avgSense = microbes.length ? senseSum / microbes.length : 0;
        ctx.onHudUpdate(
          `自然选择生态圈 · 存活个体 ${microbes.length} · 最高代数 G${maxGen} · 平均速度基因 ${avgSpd.toFixed(2)} · 感知半径 ${avgSense.toFixed(0)}px`,
        );
      }
    } else {
      // 3D Depth-Sorted ASCII Torus Knot & Double Helix Projector
      g.fillStyle = '#03060e';
      g.fillRect(0, 0, width, height);

      asciiYaw += 0.008 * (0.3 + mutationRate * 2.5);
      const count = Math.max(120, Math.min(520, Math.round(population * 1.8)));
      const glyphs = ' .:-=+*#%@';
      const pts: { sx: number; sy: number; z: number; ch: string; col: string }[] = [];

      const cy = Math.cos(asciiYaw);
      const sy = Math.sin(asciiYaw);
      const cp = Math.cos(asciiPitch);
      const sp = Math.sin(asciiPitch);

      for (let i = 0; i < count; i++) {
        const u = (i / count) * Math.PI * 2;
        const v = ((i * 13) % count) / count * Math.PI * 2;
        const R = 1.65;
        const r = 0.65;
        const x0 = (R + r * Math.cos(v + now * 0.0012)) * Math.cos(u);
        const y0 = r * Math.sin(v + now * 0.0012);
        const z0 = (R + r * Math.cos(v + now * 0.0012)) * Math.sin(u);

        const x1 = x0 * cy - z0 * sy;
        const z1 = x0 * sy + z0 * cy;
        const y2 = y0 * cp - z1 * sp;
        const z2 = y0 * sp + z1 * cp;

        const depth = 4.2 + z2;
        const scale = Math.min(width, height) * 0.72 / Math.max(1.2, depth);
        const sx = width * 0.5 + x1 * scale;
        const syScreen = height * 0.5 + y2 * scale;

        const lum = Math.max(0, Math.min(1, (2.4 - z2) / 4.8));
        const ch = glyphs[Math.min(glyphs.length - 1, Math.floor(lum * glyphs.length))];
        const hue = (200 + (u / (Math.PI * 2)) * 120) % 360;
        pts.push({
          sx,
          sy: syScreen,
          z: z2,
          ch,
          col: `hsla(${hue.toFixed(0)}, 88%, ${(38 + lum * 42).toFixed(0)}%, ${(0.35 + lum * 0.65).toFixed(2)})`,
        });
      }

      pts.sort((a, b) => b.z - a.z);
      const fontSize = Math.round(11 + foodDensity * 9);
      g.font = `600 ${fontSize}px ui-monospace, SFMono-Regular, Menlo, monospace`;
      g.textAlign = 'center';
      g.textBaseline = 'middle';

      for (const pt of pts) {
        g.fillStyle = pt.col;
        g.fillText(pt.ch, pt.sx, pt.sy);
      }

      if (now - hudTick > 300) {
        hudTick = now;
        ctx.onHudUpdate(
          `三维深度排序 ASCII 投影仪 · 采样体素 ${count} · 字号 ${fontSize}px (按住拖拽可自由旋转环面)`,
        );
      }
    }
  };

  raf = requestAnimationFrame(animate);

  return {
    reset() {
      initEcosystem(Number(ctx.getParams().population ?? 160));
    },
    dispose() {
      cancelAnimationFrame(raf);
      observer.disconnect();
      canvas.removeEventListener('pointerdown', onDown);
      canvas.removeEventListener('pointermove', onMove);
      window.removeEventListener('pointerup', onUp);
      if (canvas.parentNode === ctx.container) {
        ctx.container.removeChild(canvas);
      }
    },
  };
}

// ============================================================================
// Master Dispatcher for all 16 Upgraded Web3D Studio Scenes
// ============================================================================
export function mountStudioScene(sceneId: string, ctx: SceneRunContext): ActiveSceneHandle {
  switch (sceneId) {
    case 'gpgpu-black-hole':
      return mountBlackHoleScene(ctx);
    case 'toroidal-flow-atlas':
      return mountToroidalFlowScene(ctx);
    case 'gpu-evolution-life':
      return mountGpuEvolutionLifeScene(ctx);
    case 'gray-scott-reaction':
      return mountGrayScottScene(ctx);
    case 'chladni-standing-wave':
      return mountChladniPlateScene(ctx);
    case 'verlet-cloth-banner':
      return mountVerletClothScene(ctx);
    case 'boids-flocking':
      return mountBoidsFlockingScene(ctx);
    case 'autonomous-snake':
      return mountAutonomousSnakeScene(ctx);
    case 'sdf-ferrofluid-sculpture':
      return mountSdfFerrofluidScene(ctx);
    case 'molten-relief':
      return mountMoltenReliefScene(ctx);
    case 'thin-film-interference':
      return mountThinFilmOpticsScene(ctx);
    case 'gravitational-lensing-aurora':
      return mountAstroLensingAuroraScene(ctx);
    case 'cosmic-web-attractor':
      return mountCosmicAttractorScene(ctx);
    case 'fibonacci-parastichy-sphere':
      return mountFibonacciMorphScene(ctx);
    case 'marble-music-proof':
      return mountMarbleProofScene(ctx);
    case 'alife-ascii-workbench':
      return mountAlifeAsciiScene(ctx);
    default:
      return mountBlackHoleScene(ctx);
  }
}
