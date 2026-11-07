import * as THREE from 'three';

export interface SceneRunContext {
  container: HTMLElement;
  getParams: () => Record<string, any>;
  onHudUpdate: (text: string) => void;
  getAudioPulse: () => { bass: number; mid: number; treble: number; enabled: boolean };
  triggerBellNote: (freq: number, duration: number) => void;
}

export interface ActiveSceneHandle {
  dispose: () => void;
  reset?: () => void;
}

/**
 * Lightweight, zero-dependency 3D orbit controller attached to a DOM element.
 */
export function attachOrbitCamera(
  dom: HTMLElement,
  camera: THREE.PerspectiveCamera,
  initial: { yaw?: number; pitch?: number; distance?: number; minDistance?: number; maxDistance?: number; target?: THREE.Vector3 } = {},
) {
  const state = {
    yaw: initial.yaw ?? 0.35,
    pitch: initial.pitch ?? 0.28,
    distance: initial.distance ?? 9.0,
    targetYaw: initial.yaw ?? 0.35,
    targetPitch: initial.pitch ?? 0.28,
    targetDistance: initial.distance ?? 9.0,
    minDistance: initial.minDistance ?? 2.8,
    maxDistance: initial.maxDistance ?? 26.0,
    target: initial.target ?? new THREE.Vector3(0, 0, 0),
    dragging: false,
    pointerId: -1,
    lastX: 0,
    lastY: 0,
    pointerNdc: new THREE.Vector2(0, 0),
    pointerVelocity: new THREE.Vector2(0, 0),
    pointerDown: false,
    button: 0,
  };

  const updatePointerNdc = (e: PointerEvent) => {
    const rect = dom.getBoundingClientRect();
    const nx = ((e.clientX - rect.left) / Math.max(1, rect.width)) * 2 - 1;
    const ny = -(((e.clientY - rect.top) / Math.max(1, rect.height)) * 2 - 1);
    state.pointerVelocity.set(nx - state.pointerNdc.x, ny - state.pointerNdc.y);
    state.pointerNdc.set(nx, ny);
  };

  const onPointerDown = (e: PointerEvent) => {
    updatePointerNdc(e);
    state.pointerDown = true;
    state.button = e.button;
    state.dragging = true;
    state.pointerId = e.pointerId;
    state.lastX = e.clientX;
    state.lastY = e.clientY;
    dom.setPointerCapture?.(e.pointerId);
  };

  const onPointerMove = (e: PointerEvent) => {
    updatePointerNdc(e);
    if (!state.dragging || e.pointerId !== state.pointerId) return;
    const dx = e.clientX - state.lastX;
    const dy = e.clientY - state.lastY;
    state.lastX = e.clientX;
    state.lastY = e.clientY;
    state.targetYaw -= dx * 0.0065;
    state.targetPitch = THREE.MathUtils.clamp(state.targetPitch + dy * 0.0055, -1.35, 1.35);
  };

  const onPointerUp = (e: PointerEvent) => {
    state.pointerDown = false;
    if (e.pointerId === state.pointerId) {
      state.dragging = false;
      state.pointerId = -1;
      if (dom.hasPointerCapture?.(e.pointerId)) {
        dom.releasePointerCapture(e.pointerId);
      }
    }
  };

  const onWheel = (e: WheelEvent) => {
    e.preventDefault();
    state.targetDistance = THREE.MathUtils.clamp(
      state.targetDistance + e.deltaY * 0.006,
      state.minDistance,
      state.maxDistance,
    );
  };

  const onContextMenu = (e: MouseEvent) => e.preventDefault();

  dom.addEventListener('pointerdown', onPointerDown);
  dom.addEventListener('pointermove', onPointerMove);
  window.addEventListener('pointerup', onPointerUp);
  dom.addEventListener('wheel', onWheel, { passive: false });
  dom.addEventListener('contextmenu', onContextMenu);

  const step = (dt: number, autoRotateSpeed = 0) => {
    if (!state.dragging && autoRotateSpeed !== 0) {
      state.targetYaw += dt * autoRotateSpeed;
    }
    const ease = 1 - Math.exp(-10 * Math.min(dt, 0.05));
    state.yaw += (state.targetYaw - state.yaw) * ease;
    state.pitch += (state.targetPitch - state.pitch) * ease;
    state.distance += (state.targetDistance - state.distance) * ease;

    const cp = Math.cos(state.pitch);
    camera.position.set(
      state.target.x + Math.sin(state.yaw) * cp * state.distance,
      state.target.y + Math.sin(state.pitch) * state.distance,
      state.target.z + Math.cos(state.yaw) * cp * state.distance,
    );
    camera.lookAt(state.target);
    state.pointerVelocity.multiplyScalar(0.85);
  };

  const dispose = () => {
    dom.removeEventListener('pointerdown', onPointerDown);
    dom.removeEventListener('pointermove', onPointerMove);
    window.removeEventListener('pointerup', onPointerUp);
    dom.removeEventListener('wheel', onWheel);
    dom.removeEventListener('contextmenu', onContextMenu);
  };

  return { state, step, dispose };
}

export function createThreeStage(container: HTMLElement, fov = 45) {
  const renderer = new THREE.WebGLRenderer({ antialias: true, powerPreference: 'high-performance' });
  renderer.setPixelRatio(Math.min(window.devicePixelRatio || 1, 2));
  renderer.setSize(Math.max(1, container.clientWidth), Math.max(1, container.clientHeight));
  renderer.domElement.style.display = 'block';
  renderer.domElement.style.width = '100%';
  renderer.domElement.style.height = '100%';
  renderer.domElement.style.touchAction = 'none';
  container.innerHTML = '';
  container.appendChild(renderer.domElement);

  const scene = new THREE.Scene();
  const camera = new THREE.PerspectiveCamera(
    fov,
    Math.max(1, container.clientWidth) / Math.max(1, container.clientHeight),
    0.1,
    120,
  );

  const observer = new ResizeObserver(() => {
    const w = Math.max(1, container.clientWidth);
    const h = Math.max(1, container.clientHeight);
    renderer.setSize(w, h);
    camera.aspect = w / h;
    camera.updateProjectionMatrix();
  });
  observer.observe(container);

  const disposeStage = () => {
    observer.disconnect();
    scene.traverse((obj) => {
      const mesh = obj as THREE.Mesh;
      if (mesh.geometry) mesh.geometry.dispose();
      if (mesh.material) {
        if (Array.isArray(mesh.material)) mesh.material.forEach((m) => m.dispose());
        else mesh.material.dispose();
      }
    });
    scene.clear();
    renderer.dispose();
    renderer.forceContextLoss();
    if (renderer.domElement.parentNode === container) {
      container.removeChild(renderer.domElement);
    }
  };

  return { renderer, scene, camera, disposeStage };
}

// ============================================================================
// 1. 002 - GPGPU Black Hole Accretion Disk (512x512 = 262,144 GPU particles)
// ============================================================================
export function mountBlackHoleScene(ctx: SceneRunContext): ActiveSceneHandle {
  const { renderer, scene, camera, disposeStage } = createThreeStage(ctx.container, 50);
  scene.background = new THREE.Color('#020308');
  const orbit = attachOrbitCamera(renderer.domElement, camera, {
    yaw: 0.4,
    pitch: 0.36,
    distance: 18,
    minDistance: 5,
    maxDistance: 36,
  });

  const TEX_SIZE = 512;
  const PARTICLE_COUNT = TEX_SIZE * TEX_SIZE;
  const gpgpuScene = new THREE.Scene();
  const gpgpuCamera = new THREE.OrthographicCamera(-1, 1, 1, -1, 0, 1);
  const gpgpuQuad = new THREE.Mesh(new THREE.PlaneGeometry(2, 2));
  gpgpuScene.add(gpgpuQuad);

  const rtOpts: THREE.RenderTargetOptions = {
    wrapS: THREE.ClampToEdgeWrapping,
    wrapT: THREE.ClampToEdgeWrapping,
    minFilter: THREE.NearestFilter,
    magFilter: THREE.NearestFilter,
    format: THREE.RGBAFormat,
    type: THREE.HalfFloatType,
    depthBuffer: false,
  };
  const rtPos = [new THREE.WebGLRenderTarget(TEX_SIZE, TEX_SIZE, rtOpts), new THREE.WebGLRenderTarget(TEX_SIZE, TEX_SIZE, rtOpts)];
  const rtVel = [new THREE.WebGLRenderTarget(TEX_SIZE, TEX_SIZE, rtOpts), new THREE.WebGLRenderTarget(TEX_SIZE, TEX_SIZE, rtOpts)];

  const fullVert = `varying vec2 vUv; void main(){ vUv = uv; gl_Position = vec4(position, 1.0); }`;
  const velFrag = `
    precision highp float;
    uniform sampler2D uPosition;
    uniform sampler2D uVelocity;
    uniform float uDelta;
    uniform float uTime;
    uniform float uGravity;
    uniform float uFriction;
    uniform float uTurbulence;
    uniform vec3 uPointer;
    varying vec2 vUv;

    float hash(vec3 p) {
      p = fract(p * 0.3183099 + 0.1);
      p *= 17.0;
      return fract(p.x * p.y * p.z * (p.x + p.y + p.z));
    }

    vec3 curlField(vec3 p, float t) {
      return vec3(
        sin(p.y * 0.65 + t * 0.7) - cos(p.z * 0.55 - t * 0.4),
        sin(p.z * 0.65 + t * 0.5) - cos(p.x * 0.55 + t * 0.6),
        sin(p.x * 0.65 - t * 0.6) - cos(p.y * 0.55 + t * 0.5)
      ) * 0.5;
    }

    void main() {
      vec4 posData = texture2D(uPosition, vUv);
      vec3 pos = posData.xyz;
      float seed = posData.w;
      vec3 vel = texture2D(uVelocity, vUv).xyz;

      float dist = max(length(pos), 0.001);
      vec3 normDir = -pos / dist;

      float respawnRand = fract(sin(dot(vec2(seed, uTime * 0.17), vec2(12.9898, 78.233))) * 43758.5453);
      if (dist > 34.0 || dist < 1.08 || respawnRand < 0.0015) {
        float angle = fract(seed * 17.137 + uTime * 0.018) * 6.2831853;
        float rJit = fract(seed * 123.456 + uTime * 0.071);
        float useInner = step(fract(seed * 41.77 + uTime * 0.037), 0.48);
        float radius = mix(9.5 + rJit * 7.5, 1.65 + pow(rJit, 1.7) * 4.2, useInner);
        vec3 tangent = normalize(vec3(-sin(angle), 0.0, cos(angle)));
        float speed = sqrt(max(uGravity / max(radius, 1.0), 0.0)) * mix(1.26, 1.42, useInner);
        vel = tangent * speed + vec3(0.0, (fract(seed * 761.3 + uTime * 0.05) - 0.5) * 0.18, 0.0);
        gl_FragColor = vec4(vel, 1.0);
        return;
      }

      float safeDist = max(dist, 1.25);
      float gForce = uGravity / (safeDist * safeDist + 5.5) + exp(-safeDist * 0.45) * uGravity * 0.08;
      vec3 force = normDir * gForce;
      vec3 tangent = normalize(cross(normDir, vec3(0.0, 1.0, 0.0)));
      if (length(tangent) < 0.001) tangent = vec3(1.0, 0.0, 0.0);

      vec3 curl = curlField(pos * 0.35, uTime) * (0.2 + exp(-safeDist * 0.22) * 0.85) * uTurbulence;
      force += curl;

      vec3 ptrDiff = uPointer - pos;
      float ptrDist = length(ptrDiff);
      if (ptrDist < 5.5 && ptrDist > 0.1) {
        force += normalize(ptrDiff) * (1.0 - ptrDist / 5.5) * 4.2;
      }

      vel += force * uDelta;
      float idealSpeed = sqrt(gForce * safeDist) * 1.12;
      vec3 idealTangent = tangent * idealSpeed;
      float radialSpeed = dot(vel, normDir);
      vec3 radialVel = radialSpeed * normDir * uFriction;
      vec3 tangentVel = (vel - radialSpeed * normDir) * uFriction;
      float photonBand = smoothstep(1.28, 1.65, safeDist) * (1.0 - smoothstep(3.4, 5.8, safeDist));
      tangentVel.y *= mix(0.94, 0.82, photonBand);
      tangentVel = mix(tangentVel, idealTangent, uDelta * (1.3 + photonBand * 1.6));
      vel = radialVel + tangentVel;
      float spd = length(vel);
      if (spd > 12.0) vel = (vel / spd) * 12.0;
      gl_FragColor = vec4(vel, 1.0);
    }
  `;

  const posFrag = `
    precision highp float;
    uniform sampler2D uPosition;
    uniform sampler2D uVelocity;
    uniform float uDelta;
    uniform float uTime;
    varying vec2 vUv;
    void main() {
      vec4 posData = texture2D(uPosition, vUv);
      vec3 pos = posData.xyz;
      float seed = posData.w;
      vec3 vel = texture2D(uVelocity, vUv).xyz;
      pos += vel * uDelta;
      float dist = length(pos);
      float rand = fract(sin(dot(vec2(seed, uTime * 0.17), vec2(12.9898, 78.233))) * 43758.5453);
      if (dist > 34.0 || dist < 1.08 || rand < 0.0015) {
        float angle = fract(seed * 17.137 + uTime * 0.018) * 6.2831853;
        float rJit = fract(seed * 123.456 + uTime * 0.071);
        float useInner = max(step(fract(seed * 41.77 + uTime * 0.037), 0.48), step(dist, 1.08));
        float radius = mix(9.5 + rJit * 7.5, 1.65 + pow(rJit, 1.7) * 4.2, useInner);
        float diskH = mix(0.35 + radius * 0.035, 0.22 + radius * 0.05, useInner);
        pos = vec3(cos(angle) * radius, (fract(seed * 987.65 + uTime * 0.11) - 0.5) * diskH, sin(angle) * radius);
      }
      gl_FragColor = vec4(pos, seed);
    }
  `;

  const matVel = new THREE.ShaderMaterial({
    vertexShader: fullVert,
    fragmentShader: velFrag,
    uniforms: {
      uPosition: { value: null },
      uVelocity: { value: null },
      uDelta: { value: 0.016 },
      uTime: { value: 0 },
      uGravity: { value: 18 },
      uFriction: { value: 0.985 },
      uTurbulence: { value: 0.72 },
      uPointer: { value: new THREE.Vector3(100, 100, 100) },
    },
  });

  const matPos = new THREE.ShaderMaterial({
    vertexShader: fullVert,
    fragmentShader: posFrag,
    uniforms: {
      uPosition: { value: null },
      uVelocity: { value: null },
      uDelta: { value: 0.016 },
      uTime: { value: 0 },
    },
  });

  const initSim = () => {
    const posArr = new Float32Array(PARTICLE_COUNT * 4);
    const velArr = new Float32Array(PARTICLE_COUNT * 4);
    for (let i = 0; i < PARTICLE_COUNT; i++) {
      const s1 = ((i * 16807) % 2147483647) / 2147483647;
      const s2 = ((i * 48271 + 13) % 2147483647) / 2147483647;
      const s3 = ((i * 69621 + 29) % 2147483647) / 2147483647;
      const inner = s1 < 0.45;
      const radius = inner ? 1.65 + Math.pow(s2, 1.7) * 4.2 : 7.2 + Math.sqrt(s2) * 9.5;
      const angle = s3 * Math.PI * 2;
      const h = inner ? 0.22 + radius * 0.045 : 0.32 + radius * 0.04;
      posArr[i * 4] = Math.cos(angle) * radius;
      posArr[i * 4 + 1] = (s1 - 0.5) * h;
      posArr[i * 4 + 2] = Math.sin(angle) * radius;
      posArr[i * 4 + 3] = s2;
      const spd = Math.sqrt(18.0 / radius) * (inner ? 1.5 : 1.32);
      velArr[i * 4] = -Math.sin(angle) * spd;
      velArr[i * 4 + 1] = 0;
      velArr[i * 4 + 2] = Math.cos(angle) * spd;
      velArr[i * 4 + 3] = 1;
    }
    const posTex = new THREE.DataTexture(posArr, TEX_SIZE, TEX_SIZE, THREE.RGBAFormat, THREE.FloatType);
    const velTex = new THREE.DataTexture(velArr, TEX_SIZE, TEX_SIZE, THREE.RGBAFormat, THREE.FloatType);
    posTex.needsUpdate = true;
    velTex.needsUpdate = true;
    const copyMat = new THREE.ShaderMaterial({
      vertexShader: fullVert,
      fragmentShader: `varying vec2 vUv; uniform sampler2D uTex; void main(){ gl_FragColor = texture2D(uTex, vUv); }`,
      uniforms: { uTex: { value: posTex } },
    });
    gpgpuQuad.material = copyMat;
    renderer.setRenderTarget(rtPos[0]);
    renderer.render(gpgpuScene, gpgpuCamera);
    renderer.setRenderTarget(rtPos[1]);
    renderer.render(gpgpuScene, gpgpuCamera);
    copyMat.uniforms.uTex.value = velTex;
    renderer.setRenderTarget(rtVel[0]);
    renderer.render(gpgpuScene, gpgpuCamera);
    renderer.setRenderTarget(rtVel[1]);
    renderer.render(gpgpuScene, gpgpuCamera);
    renderer.setRenderTarget(null);
    posTex.dispose();
    velTex.dispose();
    copyMat.dispose();
  };
  initSim();

  const geo = new THREE.BufferGeometry();
  const dummyPos = new Float32Array(PARTICLE_COUNT * 3);
  const uvs = new Float32Array(PARTICLE_COUNT * 2);
  for (let i = 0; i < PARTICLE_COUNT; i++) {
    uvs[i * 2] = (i % TEX_SIZE) / TEX_SIZE;
    uvs[i * 2 + 1] = Math.floor(i / TEX_SIZE) / TEX_SIZE;
  }
  geo.setAttribute('position', new THREE.BufferAttribute(dummyPos, 3));
  geo.setAttribute('uv', new THREE.BufferAttribute(uvs, 2));

  const renderMat = new THREE.ShaderMaterial({
    vertexShader: `
      uniform sampler2D uPosition;
      uniform sampler2D uVelocity;
      uniform vec3 uColorCore;
      uniform vec3 uColorOuter;
      uniform float uParticleSize;
      uniform float uHorizonGlow;
      varying vec3 vColor;
      varying float vAlpha;
      varying float vDist;
      varying float vGlow;
      void main() {
        vec3 pos = texture2D(uPosition, uv).xyz;
        vec3 vel = texture2D(uVelocity, uv).xyz;
        vec4 mv = modelViewMatrix * vec4(pos, 1.0);
        gl_Position = projectionMatrix * mv;
        vDist = length(pos);
        float speed = length(vel);
        float diskPlane = exp(-abs(pos.y) * 2.1);
        float photonRing = exp(-pow((vDist - 1.62) / 0.42, 2.0)) * diskPlane;
        vGlow = clamp(photonRing * 1.45 * uHorizonGlow, 0.0, 4.0);
        gl_PointSize = clamp(uParticleSize * (24.0 / max(1.0, -mv.z)) * (1.0 + speed * 0.05 + vGlow * 0.1), 1.0, 5.2);
        float df = clamp(1.0 - vDist / 16.0, 0.0, 1.0);
        vec3 base = mix(uColorOuter, uColorCore, pow(df, 1.4));
        float sf = smoothstep(0.6, 7.5, speed);
        vColor = base + uColorCore * (pow(sf, 1.25) * 0.55 + vGlow * 0.85);
        vAlpha = mix(0.28, 0.92, clamp(sf + pow(df, 1.8) + vGlow * 0.35, 0.0, 1.0));
      }
    `,
    fragmentShader: `
      precision highp float;
      varying vec3 vColor;
      varying float vAlpha;
      varying float vDist;
      varying float vGlow;
      void main() {
        float d = length(gl_PointCoord - 0.5);
        if (d > 0.5) discard;
        float hFade = smoothstep(1.0, 1.48, vDist);
        float intensity = 1.0 - smoothstep(0.0, 0.5, d);
        gl_FragColor = vec4(vColor * intensity * (1.0 + vGlow * 0.8), vAlpha * intensity * hFade * 0.28);
      }
    `,
    uniforms: {
      uPosition: { value: null },
      uVelocity: { value: null },
      uColorCore: { value: new THREE.Color('#ffbc66') },
      uColorOuter: { value: new THREE.Color('#3d7dff') },
      uParticleSize: { value: 2.1 },
      uHorizonGlow: { value: 1.05 },
    },
    transparent: true,
    depthWrite: false,
    blending: THREE.AdditiveBlending,
  });

  const points = new THREE.Points(geo, renderMat);
  points.frustumCulled = false;
  points.renderOrder = 2;
  scene.add(points);

  const horizonCore = new THREE.Mesh(
    new THREE.SphereGeometry(1.02, 48, 48),
    new THREE.MeshBasicMaterial({ color: 0x000000, depthWrite: true }),
  );
  horizonCore.renderOrder = 1;
  scene.add(horizonCore);

  let stepIdx = 0;
  let raf = 0;
  let lastT = performance.now();
  const raycaster = new THREE.Raycaster();
  const plane = new THREE.Plane(new THREE.Vector3(0, 1, 0), 0);
  const intersectPt = new THREE.Vector3();

  const animate = (now: number) => {
    const dt = Math.min((now - lastT) / 1000, 0.033);
    lastT = now;
    const t = now * 0.001;
    const p = ctx.getParams();
    const audio = ctx.getAudioPulse();

    orbit.step(dt, 0.08);
    raycaster.setFromCamera(orbit.state.pointerNdc, camera);
    if (raycaster.ray.intersectPlane(plane, intersectPt)) {
      matVel.uniforms.uPointer.value.copy(intersectPt);
    }

    const cur = stepIdx % 2;
    const nxt = (stepIdx + 1) % 2;

    matVel.uniforms.uDelta.value = dt;
    matVel.uniforms.uTime.value = t;
    matVel.uniforms.uGravity.value = Number(p.gravity ?? 18) * (1 + audio.bass * 0.45);
    matVel.uniforms.uFriction.value = Number(p.friction ?? 0.985);
    matVel.uniforms.uTurbulence.value = Number(p.turbulence ?? 0.72) + audio.mid * 0.8;
    matVel.uniforms.uPosition.value = rtPos[cur].texture;
    matVel.uniforms.uVelocity.value = rtVel[cur].texture;

    gpgpuQuad.material = matVel;
    renderer.setRenderTarget(rtVel[nxt]);
    renderer.render(gpgpuScene, gpgpuCamera);

    matPos.uniforms.uDelta.value = dt;
    matPos.uniforms.uTime.value = t;
    matPos.uniforms.uPosition.value = rtPos[cur].texture;
    matPos.uniforms.uVelocity.value = rtVel[nxt].texture;

    gpgpuQuad.material = matPos;
    renderer.setRenderTarget(rtPos[nxt]);
    renderer.render(gpgpuScene, gpgpuCamera);
    renderer.setRenderTarget(null);

    renderMat.uniforms.uPosition.value = rtPos[nxt].texture;
    renderMat.uniforms.uVelocity.value = rtVel[nxt].texture;
    renderMat.uniforms.uColorCore.value.set(p.colorCore || '#ffbc66');
    renderMat.uniforms.uColorOuter.value.set(p.colorOuter || '#3d7dff');
    renderMat.uniforms.uParticleSize.value = Number(p.particleSize ?? 2.1);
    renderMat.uniforms.uHorizonGlow.value = Number(p.horizonGlow ?? 1.05) + audio.treble * 0.6;

    renderer.render(scene, camera);
    stepIdx++;
    if (stepIdx % 20 === 0) {
      ctx.onHudUpdate(`PARTICLES: 262,144 · G=${Number(p.gravity ?? 18).toFixed(1)} · ISCO=1.65Rs`);
    }
    raf = requestAnimationFrame(animate);
  };
  raf = requestAnimationFrame(animate);

  return {
    reset: initSim,
    dispose: () => {
      cancelAnimationFrame(raf);
      orbit.dispose();
      rtPos.forEach((rt) => rt.dispose());
      rtVel.forEach((rt) => rt.dispose());
      matVel.dispose();
      matPos.dispose();
      gpgpuQuad.geometry.dispose();
      disposeStage();
    },
  };
}

// ============================================================================
// 2. 019 - Toroidal Flow Atlas (RK4 Torus Vector Field Streamlines)
// ============================================================================
export function mountToroidalFlowScene(ctx: SceneRunContext): ActiveSceneHandle {
  const { renderer, scene, camera, disposeStage } = createThreeStage(ctx.container, 44);
  scene.background = new THREE.Color('#02060d');
  const orbit = attachOrbitCamera(renderer.domElement, camera, {
    yaw: 0.6,
    pitch: 0.42,
    distance: 8.6,
    minDistance: 4.2,
    maxDistance: 16,
  });

  const streamlineMat = new THREE.ShaderMaterial({
    uniforms: {
      uTime: { value: 0 },
      uFlowSpeed: { value: 0.1 },
    },
    vertexShader: `
      attribute float aPathT;
      attribute float aSeed;
      attribute float aSpeed;
      attribute float aRadius;
      varying float vPathT;
      varying float vSeed;
      varying float vSpeed;
      varying float vRadius;
      void main() {
        vPathT = aPathT;
        vSeed = aSeed;
        vSpeed = aSpeed;
        vRadius = aRadius;
        gl_Position = projectionMatrix * modelViewMatrix * vec4(position, 1.0);
      }
    `,
    fragmentShader: `
      precision highp float;
      uniform float uTime;
      uniform float uFlowSpeed;
      varying float vPathT;
      varying float vSeed;
      varying float vSpeed;
      varying float vRadius;
      void main() {
        float phase = fract(vPathT - uTime * uFlowSpeed + vSeed);
        float head = exp(-pow(phase / 0.035, 2.0)) + exp(-pow((phase - 1.0) / 0.035, 2.0));
        float trail = 1.0 - smoothstep(0.0, 0.25, phase);
        float alpha = (0.09 + trail * 0.58 + head * 1.35) * 0.65;
        vec3 col = mix(vec3(0.14, 0.58, 0.78), vec3(0.75, 0.98, 1.0), smoothstep(0.1, 0.9, vSpeed));
        col = mix(col, vec3(0.35, 0.95, 0.82), (1.0 - vRadius) * 0.45);
        col *= 0.62 + head * 1.75 + trail * 0.4;
        gl_FragColor = vec4(col, alpha);
      }
    `,
    transparent: true,
    blending: THREE.AdditiveBlending,
    depthWrite: false,
  });

  const shellMesh = new THREE.Mesh(
    new THREE.TorusGeometry(2.45, 1.15, 36, 96),
    new THREE.MeshBasicMaterial({
      color: '#1b8ca8',
      wireframe: true,
      transparent: true,
      opacity: 0.06,
      depthWrite: false,
    }),
  );
  scene.add(shellMesh);

  let lineSegments: THREE.LineSegments | null = null;
  let lastKey = '';

  const rebuildStreamlines = () => {
    const p = ctx.getParams();
    const key = `${p.resonanceP}:${p.resonanceQ}:${p.poloidalSpeed}:${p.toroidalSpeed}:${p.radialPinch}:${p.shearStrength}:${p.seedMode}`;
    if (key === lastKey && lineSegments) return;
    lastKey = key;

    const R = 2.45;
    const rMax = 1.15;
    const rMin = 0.06;
    const lineCount = 460;
    const samples = 160;
    const dt = 0.028;
    const resP = Number(p.resonanceP ?? 3);
    const resQ = Number(p.resonanceQ ?? 2);
    const polSpd = Number(p.poloidalSpeed ?? 1.48);
    const torSpd = Number(p.toroidalSpeed ?? 0.86);
    const pinch = Number(p.radialPinch ?? 1.15);
    const shear = Number(p.shearStrength ?? 0.5);
    const seedMode = String(p.seedMode ?? 'surface-shells');

    const segCount = lineCount * (samples - 1);
    const pos = new Float32Array(segCount * 6);
    const pathT = new Float32Array(segCount * 2);
    const seeds = new Float32Array(segCount * 2);
    const speeds = new Float32Array(segCount * 2);
    const radii = new Float32Array(segCount * 2);

    const evalField = (u: number, v: number, rho: number) => {
      const ratio = Math.min(1, Math.max(0, rho / rMax));
      const resPhase = resP * v - resQ * u + 0.35;
      const radPhase = 2 * u - 3 * v + 0.35;
      const eqRho = rMax * (0.52 - 0.36 * Math.cos(radPhase));
      return {
        du: torSpd + shear * ratio * ratio + 0.32 * Math.sin(resPhase),
        dv: polSpd + 0.32 * Math.cos(resQ * u - resP * v + 0.35) + shear * (1 - ratio * ratio) * 0.32,
        drho: -pinch * (rho - eqRho) + rMax * 0.2 * Math.sin(radPhase),
      };
    };

    const toCart = (u: number, v: number, rho: number) => {
      const ring = R + rho * Math.cos(v);
      return [ring * Math.cos(u), rho * Math.sin(v), ring * Math.sin(u)] as const;
    };

    let cursor = 0;
    let maxSpd = 0.01;
    const golden = 0.61803398875;

    for (let i = 0; i < lineCount; i++) {
      let u = (i * golden * Math.PI * 2) % (Math.PI * 2);
      let v = (i * golden * golden * Math.PI * 2) % (Math.PI * 2);
      let rho = rMin + (rMax - rMin) * 0.5;
      if (seedMode === 'surface-shells') {
        const shells = [0.16, 0.34, 0.55, 0.74, 0.92];
        rho = rMin + (rMax - rMin) * shells[i % shells.length];
      } else if (seedMode === 'cross-section-grid') {
        const side = Math.ceil(Math.sqrt(lineCount));
        u = 0.35;
        v = ((i % side) / side) * Math.PI * 2;
        rho = rMin + ((rMax - rMin) * Math.floor(i / side)) / Math.max(1, side - 1);
      } else if (seedMode === 'focused-core') {
        rho = rMin + (rMax - rMin) * Math.pow((i % 37) / 37, 2.2) * 0.55;
      } else {
        rho = rMin + (rMax - rMin) * Math.sqrt(((i * 97) % 460) / 460);
      }

      const seedVal = ((i * 73) % 460) / 460;
      let prev = toCart(u, v, rho);

      for (let s = 1; s < samples; s++) {
        const k1 = evalField(u, v, rho);
        const k2 = evalField(u + k1.du * dt * 0.5, v + k1.dv * dt * 0.5, rho + k1.drho * dt * 0.5);
        const k3 = evalField(u + k2.du * dt * 0.5, v + k2.dv * dt * 0.5, rho + k2.drho * dt * 0.5);
        const k4 = evalField(u + k3.du * dt, v + k3.dv * dt, rho + k3.drho * dt);
        u += (dt / 6) * (k1.du + 2 * k2.du + 2 * k3.du + k4.du);
        v += (dt / 6) * (k1.dv + 2 * k2.dv + 2 * k3.dv + k4.dv);
        rho = Math.min(rMax, Math.max(rMin, rho + (dt / 6) * (k1.drho + 2 * k2.drho + 2 * k3.drho + k4.drho)));

        const nxt = toCart(u, v, rho);
        const spd = Math.hypot(nxt[0] - prev[0], nxt[1] - prev[1], nxt[2] - prev[2]) / dt;
        if (spd > maxSpd) maxSpd = spd;

        pos.set([prev[0], prev[1], prev[2], nxt[0], nxt[1], nxt[2]], cursor * 3);
        pathT[cursor] = (s - 1) / (samples - 1);
        pathT[cursor + 1] = s / (samples - 1);
        seeds[cursor] = seedVal;
        seeds[cursor + 1] = seedVal;
        speeds[cursor] = spd;
        speeds[cursor + 1] = spd;
        radii[cursor] = rho / rMax;
        radii[cursor + 1] = rho / rMax;
        cursor += 2;
        prev = nxt;
      }
    }

    for (let i = 0; i < speeds.length; i++) speeds[i] /= maxSpd;

    if (lineSegments) {
      scene.remove(lineSegments);
      lineSegments.geometry.dispose();
    }
    const geo = new THREE.BufferGeometry();
    geo.setAttribute('position', new THREE.BufferAttribute(pos, 3));
    geo.setAttribute('aPathT', new THREE.BufferAttribute(pathT, 1));
    geo.setAttribute('aSeed', new THREE.BufferAttribute(seeds, 1));
    geo.setAttribute('aSpeed', new THREE.BufferAttribute(speeds, 1));
    geo.setAttribute('aRadius', new THREE.BufferAttribute(radii, 1));
    lineSegments = new THREE.LineSegments(geo, streamlineMat);
    scene.add(lineSegments);
    ctx.onHudUpdate(`RK4 STREAMLINES: ${lineCount} · WINDING p:q = ${resP}:${resQ} · SEGMENTS: ${segCount}`);
  };

  rebuildStreamlines();
  let raf = 0;
  let lastT = performance.now();

  const animate = (now: number) => {
    const dt = Math.min((now - lastT) / 1000, 0.04);
    lastT = now;
    rebuildStreamlines();
    const p = ctx.getParams();
    const audio = ctx.getAudioPulse();
    orbit.step(dt, 0.12);
    streamlineMat.uniforms.uTime.value = now * 0.001;
    streamlineMat.uniforms.uFlowSpeed.value = Number(p.flowSpeed ?? 0.1) * (1 + audio.bass * 1.5);
    renderer.render(scene, camera);
    raf = requestAnimationFrame(animate);
  };
  raf = requestAnimationFrame(animate);

  return {
    reset: () => {
      lastKey = '';
      rebuildStreamlines();
    },
    dispose: () => {
      cancelAnimationFrame(raf);
      orbit.dispose();
      disposeStage();
    },
  };
}
