import * as THREE from 'three';
import {
  attachOrbitCamera,
  createThreeStage,
  type ActiveSceneHandle,
  type SceneRunContext,
} from './web3dEnginesA';

// ============================================================================
// 3. 023 - GPU Evolution Life Lab (96x96 GPU Cellular Automata + 3D Instanced)
// ============================================================================
export function mountGpuEvolutionLifeScene(ctx: SceneRunContext): ActiveSceneHandle {
  const { renderer, scene, camera, disposeStage } = createThreeStage(ctx.container, 42);
  scene.background = new THREE.Color('#060910');
  camera.up.set(0, 0, 1);
  const orbit = attachOrbitCamera(renderer.domElement, camera, {
    yaw: 0.0,
    pitch: 0.72,
    distance: 12.2,
    minDistance: 5.5,
    maxDistance: 18,
  });

  const GRID = 96;
  const BOARD = 7.8;

  const gpgpuScene = new THREE.Scene();
  const gpgpuCam = new THREE.OrthographicCamera(-1, 1, 1, -1, 0, 1);
  const fullVert = `varying vec2 vUv; void main(){ vUv = uv; gl_Position = vec4(position.xy, 0.0, 1.0); }`;

  const makeTarget = () =>
    new THREE.WebGLRenderTarget(GRID, GRID, {
      depthBuffer: false,
      format: THREE.RGBAFormat,
      magFilter: THREE.NearestFilter,
      minFilter: THREE.NearestFilter,
      type: THREE.UnsignedByteType,
      wrapS: THREE.RepeatWrapping,
      wrapT: THREE.RepeatWrapping,
    });
  const targets: [THREE.WebGLRenderTarget, THREE.WebGLRenderTarget] = [makeTarget(), makeTarget()];
  let curTarget: 0 | 1 = 0;
  let generation = 0;
  let seedCounter = 1;

  const initMat = new THREE.ShaderMaterial({
    vertexShader: fullVert,
    fragmentShader: `
      precision highp float;
      varying vec2 vUv;
      uniform float uSeed;
      float hash(vec2 p){ return fract(sin(dot(p, vec2(127.1, 311.7)) + uSeed * 17.13) * 43758.5453); }
      void main(){
        float cluster = 0.72 + 0.28 * sin(vUv.x * 27.0 + sin(vUv.y * 19.0 + uSeed));
        float alive = step(hash(gl_FragCoord.xy), 0.25 * cluster);
        float species = hash(gl_FragCoord.yx + vec2(73.0, 19.0));
        gl_FragColor = vec4(alive, species, alive, alive * 0.04);
      }
    `,
    uniforms: { uSeed: { value: 1 } },
  });

  const editMat = new THREE.ShaderMaterial({
    vertexShader: fullVert,
    fragmentShader: `
      precision highp float;
      varying vec2 vUv;
      uniform vec4 uBrush;
      uniform sampler2D uState;
      uniform float uSpecies;
      void main(){
        vec4 s = texture2D(uState, vUv);
        float d = distance(vUv, uBrush.xy);
        if (d <= uBrush.z) {
          if (uBrush.w > 0.5) {
            float edge = smoothstep(uBrush.z, uBrush.z * 0.5, d);
            s = vec4(edge, uSpecies, max(s.b, edge), edge * 0.05);
          } else {
            s = vec4(0.0, s.g, max(s.b, 0.2), 0.0);
          }
        }
        gl_FragColor = s;
      }
    `,
    uniforms: {
      uBrush: { value: new THREE.Vector4(0.5, 0.5, 0.03, 1) },
      uSpecies: { value: 0.5 },
      uState: { value: null },
    },
  });

  const stepMat = new THREE.ShaderMaterial({
    vertexShader: fullVert,
    fragmentShader: `
      precision highp float;
      varying vec2 vUv;
      uniform float uMutation;
      uniform float uRule;
      uniform float uSeed;
      uniform float uTrailDecay;
      uniform sampler2D uState;
      uniform vec2 uTexel;
      float aliveAt(vec2 o){ return step(0.5, texture2D(uState, vUv + o * uTexel).r); }
      vec4 stateAt(vec2 o){ return texture2D(uState, vUv + o * uTexel); }
      float hash(vec2 p){ return fract(sin(dot(p, vec2(41.13, 289.91)) + uSeed * 11.7) * 45758.5453); }
      void main(){
        vec4 c = texture2D(uState, vUv);
        float n00 = aliveAt(vec2(-1.0,-1.0)); float n10 = aliveAt(vec2(0.0,-1.0)); float n20 = aliveAt(vec2(1.0,-1.0));
        float n01 = aliveAt(vec2(-1.0, 0.0)); float n21 = aliveAt(vec2(1.0, 0.0));
        float n02 = aliveAt(vec2(-1.0, 1.0)); float n12 = aliveAt(vec2(0.0, 1.0)); float n22 = aliveAt(vec2(1.0, 1.0));
        float nb = n00 + n10 + n20 + n01 + n21 + n02 + n12 + n22;
        float alive = step(0.5, c.r);
        float born = 0.0; float survives = 0.0;
        if (uRule < 0.5) {
          born = 1.0 - step(0.1, abs(nb - 3.0));
          survives = max(1.0 - step(0.1, abs(nb - 2.0)), 1.0 - step(0.1, abs(nb - 3.0)));
        } else if (uRule < 1.5) {
          born = max(1.0 - step(0.1, abs(nb - 3.0)), 1.0 - step(0.1, abs(nb - 6.0)));
          survives = max(1.0 - step(0.1, abs(nb - 2.0)), 1.0 - step(0.1, abs(nb - 3.0)));
        } else {
          born = 1.0 - step(0.1, abs(nb - 2.0));
          survives = 0.0;
        }
        float nextAlive = max((1.0 - alive) * born, alive * survives);
        float spSum = stateAt(vec2(-1.0,-1.0)).g*n00 + stateAt(vec2(0.0,-1.0)).g*n10 + stateAt(vec2(1.0,-1.0)).g*n20 +
                      stateAt(vec2(-1.0, 0.0)).g*n01 + stateAt(vec2(1.0, 0.0)).g*n21 + stateAt(vec2(-1.0, 1.0)).g*n02 +
                      stateAt(vec2(0.0, 1.0)).g*n12 + stateAt(vec2(1.0, 1.0)).g*n22;
        float inh = spSum / max(1.0, nb);
        float mRoll = hash(gl_FragCoord.xy + vec2(uSeed, c.a * 97.0));
        float mShift = (hash(gl_FragCoord.yx + vec2(31.0, uSeed)) - 0.5) * 0.42;
        inh = fract(inh + step(mRoll, uMutation) * mShift);
        float nextSp = mix(inh, c.g, alive * nextAlive);
        float trail = max(c.b * uTrailDecay, alive * 0.96);
        float age = nextAlive > 0.5 ? min(1.0, c.a + 0.035) : 0.0;
        gl_FragColor = vec4(nextAlive, nextSp, trail, age);
      }
    `,
    uniforms: {
      uMutation: { value: 0.05 },
      uRule: { value: 0 },
      uSeed: { value: 1 },
      uState: { value: null },
      uTexel: { value: new THREE.Vector2(1 / GRID, 1 / GRID) },
      uTrailDecay: { value: 0.92 },
    },
  });

  const quad = new THREE.Mesh(new THREE.PlaneGeometry(2, 2), initMat);
  gpgpuScene.add(quad);

  const resetLife = () => {
    seedCounter += 1;
    initMat.uniforms.uSeed.value = seedCounter;
    quad.material = initMat;
    renderer.setRenderTarget(targets[0]);
    renderer.render(gpgpuScene, gpgpuCam);
    renderer.setRenderTarget(targets[1]);
    renderer.render(gpgpuScene, gpgpuCam);
    renderer.setRenderTarget(null);
    curTarget = 0;
    generation = 0;
  };
  resetLife();

  // Create InstancedBufferGeometry for 96x96 cells
  const box = new THREE.BoxGeometry(1, 1, 1);
  const instGeo = new THREE.InstancedBufferGeometry();
  instGeo.setIndex(box.index?.clone() ?? null);
  Object.entries(box.attributes).forEach(([k, attr]) => instGeo.setAttribute(k, attr.clone()));
  box.dispose();

  const cellCount = GRID * GRID;
  const offsets = new Float32Array(cellCount * 2);
  const uvs = new Float32Array(cellCount * 2);
  let cur = 0;
  for (let y = 0; y < GRID; y++) {
    for (let x = 0; x < GRID; x++) {
      offsets[cur * 2] = ((x + 0.5) / GRID - 0.5) * BOARD;
      offsets[cur * 2 + 1] = ((y + 0.5) / GRID - 0.5) * BOARD;
      uvs[cur * 2] = (x + 0.5) / GRID;
      uvs[cur * 2 + 1] = (y + 0.5) / GRID;
      cur++;
    }
  }
  instGeo.setAttribute('instanceOffset', new THREE.InstancedBufferAttribute(offsets, 2));
  instGeo.setAttribute('instanceUv', new THREE.InstancedBufferAttribute(uvs, 2));
  instGeo.instanceCount = cellCount;

  const cellMat = new THREE.ShaderMaterial({
    vertexShader: `
      attribute vec2 instanceOffset;
      attribute vec2 instanceUv;
      varying float vAge;
      varying float vAlive;
      varying vec3 vNormal;
      varying float vSpecies;
      varying float vTrail;
      uniform float uCellSize;
      uniform float uHeight;
      uniform sampler2D uState;
      void main(){
        vec4 st = texture2D(uState, instanceUv);
        float alive = step(0.5, st.r);
        float visTrail = smoothstep(0.025, 0.72, st.b) * (1.0 - alive);
        float h = alive * uHeight + visTrail * uHeight * 0.16;
        vec3 tr = position;
        tr.xy *= uCellSize * mix(0.72, 0.9, alive);
        tr.z = (position.z + 0.5) * max(0.018, h);
        tr.xy += instanceOffset;
        vAge = st.a;
        vAlive = alive;
        vNormal = normalize(normalMatrix * normal);
        vSpecies = st.g;
        vTrail = visTrail;
        gl_Position = projectionMatrix * modelViewMatrix * vec4(tr, 1.0);
      }
    `,
    fragmentShader: `
      precision highp float;
      varying float vAge;
      varying float vAlive;
      varying vec3 vNormal;
      varying float vSpecies;
      varying float vTrail;
      uniform float uPalette;
      vec3 palette(float v){
        if (uPalette < 0.5) return 0.56 + 0.44 * cos(6.28318 * (v + vec3(0.0, 0.66, 0.34)));
        if (uPalette < 1.5) return mix(vec3(0.05, 0.85, 0.72), vec3(0.22, 0.34, 1.0), smoothstep(0.05, 0.95, v));
        vec3 low = mix(vec3(0.18, 0.92, 0.38), vec3(0.95, 0.78, 0.18), smoothstep(0.0, 0.55, v));
        return mix(low, vec3(0.92, 0.22, 0.62), smoothstep(0.58, 1.0, v));
      }
      void main(){
        if (vAlive < 0.5 && vTrail < 0.025) discard;
        vec3 base = palette(vSpecies);
        vec3 lDir = normalize(vec3(-0.42, 0.58, 0.7));
        float dif = 0.34 + max(0.0, dot(vNormal, lDir)) * 0.76;
        float rim = pow(1.0 - max(0.0, vNormal.z), 2.4);
        float flash = vAlive * (1.0 - smoothstep(0.0, 0.18, vAge));
        vec3 liveCol = base * dif + base * rim * 0.32 + flash * base * 0.42;
        vec3 trailCol = mix(vec3(0.015, 0.035, 0.07), base * 0.32, vTrail);
        gl_FragColor = vec4(mix(trailCol, liveCol, vAlive), 1.0);
      }
    `,
    uniforms: {
      uCellSize: { value: BOARD / GRID },
      uHeight: { value: 0.14 },
      uPalette: { value: 0 },
      uState: { value: null },
    },
  });

  const instMesh = new THREE.Mesh(instGeo, cellMat);
  instMesh.frustumCulled = false;
  scene.add(instMesh);

  const baseBoard = new THREE.Mesh(
    new THREE.PlaneGeometry(BOARD + 0.16, BOARD + 0.16),
    new THREE.MeshBasicMaterial({ color: '#0b1019' }),
  );
  baseBoard.position.z = -0.02;
  scene.add(baseBoard);

  const raycaster = new THREE.Raycaster();
  const hitPlane = new THREE.Plane(new THREE.Vector3(0, 0, 1), 0);
  const hitPt = new THREE.Vector3();
  let accum = 0;
  let raf = 0;
  let lastT = performance.now();

  const animate = (now: number) => {
    const dt = Math.min((now - lastT) / 1000, 0.05);
    lastT = now;
    const p = ctx.getParams();
    orbit.step(dt, 0.04);

    if (orbit.state.pointerDown) {
      raycaster.setFromCamera(orbit.state.pointerNdc, camera);
      if (raycaster.ray.intersectPlane(hitPt ? hitPlane : hitPlane, hitPt)) {
        const uvX = hitPt.x / BOARD + 0.5;
        const uvY = hitPt.y / BOARD + 0.5;
        if (uvX >= 0 && uvX <= 1 && uvY >= 0 && uvY <= 1) {
          orbit.state.dragging = false; // paint instead of orbiting when hitting board
          const nxt: 0 | 1 = curTarget === 0 ? 1 : 0;
          editMat.uniforms.uState.value = targets[curTarget].texture;
          editMat.uniforms.uBrush.value.set(
            uvX,
            uvY,
            Number(p.brushSize ?? 3) / GRID,
            orbit.state.button === 2 ? 0 : 1,
          );
          editMat.uniforms.uSpecies.value = (now * 0.0003 + uvX * 0.5 + uvY * 0.7) % 1;
          quad.material = editMat;
          renderer.setRenderTarget(targets[nxt]);
          renderer.render(gpgpuScene, gpgpuCam);
          renderer.setRenderTarget(null);
          curTarget = nxt;
        }
      }
    }

    const spd = Number(p.speed ?? 12);
    accum += dt * spd;
    const steps = Math.min(8, Math.floor(accum));
    accum -= steps;
    const ruleStr = String(p.rule ?? 'conway');
    const ruleNum = ruleStr === 'conway' ? 0 : ruleStr === 'highlife' ? 1 : 2;

    for (let s = 0; s < steps; s++) {
      const nxt: 0 | 1 = curTarget === 0 ? 1 : 0;
      stepMat.uniforms.uMutation.value = Number(p.mutation ?? 0.05);
      stepMat.uniforms.uRule.value = ruleNum;
      stepMat.uniforms.uSeed.value = seedCounter + generation * 0.0013;
      stepMat.uniforms.uTrailDecay.value = Number(p.trailDecay ?? 0.92);
      stepMat.uniforms.uState.value = targets[curTarget].texture;
      quad.material = stepMat;
      renderer.setRenderTarget(targets[nxt]);
      renderer.render(gpgpuScene, gpgpuCam);
      renderer.setRenderTarget(null);
      curTarget = nxt;
      generation++;
    }

    const audio = ctx.getAudioPulse();
    cellMat.uniforms.uHeight.value = Number(p.cellHeight ?? 0.14) * (1 + audio.bass * 1.4);
    cellMat.uniforms.uPalette.value = Number(p.palette ?? 0);
    cellMat.uniforms.uState.value = targets[curTarget].texture;

    renderer.render(scene, camera);
    if (generation % 6 === 0) {
      ctx.onHudUpdate(`GRID: 96×96 · GEN: ${generation} · RULE: ${ruleStr.toUpperCase()}`);
    }
    raf = requestAnimationFrame(animate);
  };
  raf = requestAnimationFrame(animate);

  return {
    reset: resetLife,
    dispose: () => {
      cancelAnimationFrame(raf);
      orbit.dispose();
      targets.forEach((t) => t.dispose());
      initMat.dispose();
      editMat.dispose();
      stepMat.dispose();
      quad.geometry.dispose();
      disposeStage();
    },
  };
}

// ============================================================================
// 4. 050 - Gray-Scott Reaction-Diffusion (512x512 GPU Ping-Pong Solver)
// ============================================================================
export function mountGrayScottScene(ctx: SceneRunContext): ActiveSceneHandle {
  const { renderer, scene, camera, disposeStage } = createThreeStage(ctx.container, 50);
  const SIZE = 384;
  const fullVert = `varying vec2 vUv; void main(){ vUv = uv; gl_Position = vec4(position.xy, 0.0, 1.0); }`;

  const makeFbo = () =>
    new THREE.WebGLRenderTarget(SIZE, SIZE, {
      depthBuffer: false,
      minFilter: THREE.LinearFilter,
      magFilter: THREE.LinearFilter,
      type: THREE.HalfFloatType,
      wrapS: THREE.RepeatWrapping,
      wrapT: THREE.RepeatWrapping,
    });
  const rtA = makeFbo();
  const rtB = makeFbo();
  let curRt = rtA;
  let seedIdx = 1;

  const simScene = new THREE.Scene();
  const simCam = new THREE.OrthographicCamera(-1, 1, 1, -1, 0, 1);

  const createSeedTex = (seed: number) => {
    const data = new Uint8Array(SIZE * SIZE * 4);
    for (let y = 0; y < SIZE; y++) {
      for (let x = 0; x < SIZE; x++) {
        const idx = (y * SIZE + x) * 4;
        let u = 255;
        let v = 0;
        for (let s = 0; s < 16; s++) {
          const cx = ((s * 73 + seed * 41) % (SIZE - 64)) + 32;
          const cy = ((s * 127 + seed * 59) % (SIZE - 64)) + 32;
          const dx = x - cx;
          const dy = y - cy;
          if (dx * dx + dy * dy < 65 + (s % 4) * 32) {
            u = 20;
            v = 240;
          }
        }
        data[idx] = u;
        data[idx + 1] = v;
        data[idx + 2] = 0;
        data[idx + 3] = 255;
      }
    }
    const tex = new THREE.DataTexture(data, SIZE, SIZE, THREE.RGBAFormat);
    tex.needsUpdate = true;
    return tex;
  };

  const simMat = new THREE.ShaderMaterial({
    vertexShader: fullVert,
    fragmentShader: `
      precision highp float;
      varying vec2 vUv;
      uniform sampler2D uState;
      uniform vec2 uTexel;
      uniform float uFeed;
      uniform float uKill;
      uniform vec2 uBrush;
      uniform float uBrushRadius;
      uniform float uBrushActive;
      vec2 sampleS(vec2 o){ return texture2D(uState, fract(vUv + o * uTexel)).rg; }
      void main(){
        vec2 c = sampleS(vec2(0.0));
        vec2 lap = sampleS(vec2(1.0,0.0))*0.2 + sampleS(vec2(-1.0,0.0))*0.2 +
                   sampleS(vec2(0.0,1.0))*0.2 + sampleS(vec2(0.0,-1.0))*0.2 +
                   sampleS(vec2(1.0,1.0))*0.05 + sampleS(vec2(-1.0,1.0))*0.05 +
                   sampleS(vec2(1.0,-1.0))*0.05 + sampleS(vec2(-1.0,-1.0))*0.05 - c;
        float u = c.r;
        float v = c.g;
        float r = u * v * v;
        u += 0.205 * lap.r - r + uFeed * (1.0 - u);
        v += 0.102 * lap.g + r - (uFeed + uKill) * v;
        float b = 1.0 - smoothstep(uBrushRadius * 0.5, uBrushRadius, distance(vUv, uBrush));
        v = mix(v, 0.95, b * uBrushActive);
        u = mix(u, 0.08, b * uBrushActive);
        gl_FragColor = vec4(clamp(u, 0.0, 1.0), clamp(v, 0.0, 1.0), 0.0, 1.0);
      }
    `,
    uniforms: {
      uState: { value: null },
      uTexel: { value: new THREE.Vector2(1 / SIZE, 1 / SIZE) },
      uFeed: { value: 0.0367 },
      uKill: { value: 0.0649 },
      uBrush: { value: new THREE.Vector2(0.5, 0.5) },
      uBrushRadius: { value: 0.03 },
      uBrushActive: { value: 0 },
    },
  });

  const simQuad = new THREE.Mesh(new THREE.PlaneGeometry(2, 2), simMat);
  simScene.add(simQuad);

  const dispMat = new THREE.ShaderMaterial({
    vertexShader: fullVert,
    fragmentShader: `
      precision highp float;
      varying vec2 vUv;
      uniform sampler2D uState;
      uniform vec2 uTexel;
      uniform float uContrast;
      void main(){
        vec2 st = texture2D(uState, vUv).rg;
        float conc = clamp((st.g - st.r * 0.24) * uContrast, 0.0, 1.0);
        float edge = abs(texture2D(uState, vUv + vec2(uTexel.x, 0.0)).g - texture2D(uState, vUv - vec2(uTexel.x, 0.0)).g)
                   + abs(texture2D(uState, vUv + vec2(0.0, uTexel.y)).g - texture2D(uState, vUv - vec2(0.0, uTexel.y)).g);
        vec3 deep = vec3(0.008, 0.018, 0.032);
        vec3 cyan = vec3(0.02, 0.62, 0.74);
        vec3 coral = vec3(0.95, 0.25, 0.34);
        vec3 cream = vec3(1.0, 0.88, 0.65);
        vec3 col = mix(deep, cyan, smoothstep(0.02, 0.42, conc));
        col = mix(col, coral, smoothstep(0.42, 0.75, conc));
        col = mix(col, cream, smoothstep(0.74, 1.0, conc));
        col += vec3(0.32, 0.9, 1.0) * edge * 2.5;
        gl_FragColor = vec4(col, 1.0);
      }
    `,
    uniforms: {
      uState: { value: null },
      uTexel: { value: new THREE.Vector2(1 / SIZE, 1 / SIZE) },
      uContrast: { value: 1.5 },
    },
  });

  const dispQuad = new THREE.Mesh(new THREE.PlaneGeometry(2, 2), dispMat);
  scene.add(dispQuad);

  let sourceTex: THREE.Texture = createSeedTex(seedIdx);
  const resetField = () => {
    seedIdx++;
    if (sourceTex && 'isDataTexture' in sourceTex) sourceTex.dispose();
    sourceTex = createSeedTex(seedIdx);
    curRt = rtA;
  };

  let pointerDown = false;
  const brushUv = new THREE.Vector2(0.5, 0.5);
  const onDown = (e: PointerEvent) => {
    pointerDown = true;
    const r = renderer.domElement.getBoundingClientRect();
    brushUv.set((e.clientX - r.left) / r.width, 1 - (e.clientY - r.top) / r.height);
  };
  const onMove = (e: PointerEvent) => {
    const r = renderer.domElement.getBoundingClientRect();
    brushUv.set((e.clientX - r.left) / r.width, 1 - (e.clientY - r.top) / r.height);
  };
  const onUp = () => {
    pointerDown = false;
  };
  renderer.domElement.addEventListener('pointerdown', onDown);
  renderer.domElement.addEventListener('pointermove', onMove);
  window.addEventListener('pointerup', onUp);

  let raf = 0;
  let frameCount = 0;
  const animate = () => {
    const p = ctx.getParams();
    simMat.uniforms.uFeed.value = Number(p.feed ?? 0.0367);
    simMat.uniforms.uKill.value = Number(p.kill ?? 0.0649);
    simMat.uniforms.uBrush.value.copy(brushUv);
    simMat.uniforms.uBrushRadius.value = Number(p.brushRadius ?? 0.03);
    simMat.uniforms.uBrushActive.value = pointerDown ? 1 : 0;

    const steps = Math.max(1, Math.min(18, Math.round(Number(p.stepsPerFrame ?? 8))));
    for (let i = 0; i < steps; i++) {
      simMat.uniforms.uState.value = sourceTex;
      renderer.setRenderTarget(curRt);
      renderer.render(simScene, simCam);
      sourceTex = curRt.texture;
      curRt = curRt === rtA ? rtB : rtA;
    }
    renderer.setRenderTarget(null);
    dispMat.uniforms.uState.value = sourceTex;
    dispMat.uniforms.uContrast.value = Number(p.contrast ?? 1.5);
    renderer.render(scene, camera);

    frameCount++;
    if (frameCount % 25 === 0) {
      ctx.onHudUpdate(`GRAY-SCOTT ${SIZE}×${SIZE} · F=${Number(p.feed ?? 0.0367).toFixed(4)} · k=${Number(p.kill ?? 0.0649).toFixed(4)}`);
    }
    raf = requestAnimationFrame(animate);
  };
  raf = requestAnimationFrame(animate);

  return {
    reset: resetField,
    dispose: () => {
      cancelAnimationFrame(raf);
      renderer.domElement.removeEventListener('pointerdown', onDown);
      renderer.domElement.removeEventListener('pointermove', onMove);
      window.removeEventListener('pointerup', onUp);
      rtA.dispose();
      rtB.dispose();
      simMat.dispose();
      simQuad.geometry.dispose();
      disposeStage();
    },
  };
}

// ============================================================================
// 5. 045 - Chladni Standing-Wave Plate (28,000 Sand Grains Eigenmode Projection)
// ============================================================================
export function mountChladniPlateScene(ctx: SceneRunContext): ActiveSceneHandle {
  const { renderer, scene, camera, disposeStage } = createThreeStage(ctx.container, 43);
  scene.background = new THREE.Color('#05070b');
  const orbit = attachOrbitCamera(renderer.domElement, camera, {
    yaw: 0.72,
    pitch: 0.62,
    distance: 7.4,
    minDistance: 4.5,
    maxDistance: 13,
  });

  const COUNT = 28000;
  const origins = new Float32Array(COUNT * 2);
  const seeds = new Float32Array(COUNT);
  for (let i = 0; i < COUNT; i++) {
    origins[i * 2] = (((i * 16807) % 2147483647) / 2147483647) * 1.94 - 0.97;
    origins[i * 2 + 1] = (((i * 48271 + 17) % 2147483629) / 2147483629) * 1.94 - 0.97;
    seeds[i] = ((i * 69621 + 31) % 104729) / 104729;
  }

  const modeGlsl = `
    float chladni(vec2 pt, float n, float m) {
      float mSafe = abs(m - n) < 0.25 ? m + 1.0 : m;
      vec2 c = (pt + 1.0) * 1.5707963;
      return cos(n * c.x) * cos(mSafe * c.y) - cos(mSafe * c.x) * cos(n * c.y);
    }
  `;

  const plateMat = new THREE.ShaderMaterial({
    vertexShader: `varying vec2 vUv; void main(){ vUv = uv; gl_Position = projectionMatrix * modelViewMatrix * vec4(position, 1.0); }`,
    fragmentShader: `
      precision highp float;
      varying vec2 vUv;
      uniform float uTime;
      uniform float uModeN;
      uniform float uModeM;
      uniform float uVibration;
      uniform float uFrequency;
      ${modeGlsl}
      void main(){
        vec2 pt = vUv * 2.0 - 1.0;
        float f = chladni(pt, uModeN, uModeM);
        float osc = sin(uTime * uFrequency * 6.2831);
        float node = 1.0 - smoothstep(0.018, 0.08, abs(f));
        vec3 col = mix(vec3(0.035, 0.08, 0.16), vec3(0.18, 0.04, 0.16), f * osc * 0.5 + 0.5);
        col *= 0.36 + uVibration * 0.42;
        col += vec3(0.18, 0.76, 0.88) * node * 0.22;
        gl_FragColor = vec4(col, 1.0);
      }
    `,
    uniforms: {
      uTime: { value: 0 },
      uModeN: { value: 3 },
      uModeM: { value: 5 },
      uVibration: { value: 0.18 },
      uFrequency: { value: 0.65 },
    },
  });

  const plateMesh = new THREE.Mesh(new THREE.PlaneGeometry(4.9, 4.9), plateMat);
  plateMesh.rotation.x = -Math.PI / 2;
  scene.add(plateMesh);

  const baseBox = new THREE.Mesh(
    new THREE.BoxGeometry(5.08, 0.12, 5.08),
    new THREE.MeshBasicMaterial({ color: '#141b24' }),
  );
  baseBox.position.y = -0.065;
  scene.add(baseBox);

  const partGeo = new THREE.BufferGeometry();
  partGeo.setAttribute('position', new THREE.BufferAttribute(new Float32Array(COUNT * 3), 3));
  partGeo.setAttribute('aOrigin', new THREE.BufferAttribute(origins, 2));
  partGeo.setAttribute('aSeed', new THREE.BufferAttribute(seeds, 1));

  const partMat = new THREE.ShaderMaterial({
    vertexShader: `
      precision highp float;
      attribute vec2 aOrigin;
      attribute float aSeed;
      uniform float uTime;
      uniform float uModeN;
      uniform float uModeM;
      uniform float uSettling;
      uniform float uVibration;
      uniform float uFrequency;
      uniform float uParticleSize;
      varying float vSeed;
      varying float vNode;
      ${modeGlsl}
      vec2 projectToNode(vec2 initPt) {
        vec2 pt = initPt;
        const float eps = 0.006;
        for (int i = 0; i < 7; i++) {
          float f = chladni(pt, uModeN, uModeM);
          vec2 grad = vec2(
            chladni(pt + vec2(eps, 0.0), uModeN, uModeM) - chladni(pt - vec2(eps, 0.0), uModeN, uModeM),
            chladni(pt + vec2(0.0, eps), uModeN, uModeM) - chladni(pt - vec2(0.0, eps), uModeN, uModeM)
          ) / (2.0 * eps);
          vec2 corr = clamp(grad * f / (dot(grad, grad) + 0.035), vec2(-0.085), vec2(0.085));
          pt = clamp(pt - corr * 0.78, vec2(-0.98), vec2(0.98));
        }
        return pt;
      }
      void main(){
        vec2 nodePt = projectToNode(aOrigin);
        vec2 pt = mix(aOrigin, nodePt, uSettling);
        float nodeDist = abs(chladni(pt, uModeN, uModeM));
        float vib = sin(uTime * uFrequency * 6.2831 + aSeed * 12.0) * uVibration;
        float h = 0.035 + vib * (0.025 + nodeDist * 0.08);
        vec3 pos = vec3(pt.x * 2.35, h, pt.y * 2.35);
        vec4 mv = modelViewMatrix * vec4(pos, 1.0);
        gl_PointSize = clamp(uParticleSize * (6.2 / max(2.0, -mv.z)) * (0.75 + aSeed * 0.45), 1.0, 7.0);
        gl_Position = projectionMatrix * mv;
        vSeed = aSeed;
        vNode = 1.0 - smoothstep(0.0, 0.12, nodeDist);
      }
    `,
    fragmentShader: `
      precision highp float;
      varying float vSeed;
      varying float vNode;
      void main(){
        float r = length(gl_PointCoord - 0.5);
        if (r > 0.5) discard;
        float alpha = 1.0 - smoothstep(0.22, 0.5, r);
        float core = 1.0 - smoothstep(0.05, 0.2, r);
        vec3 sand = mix(vec3(0.92, 0.48, 0.14), vec3(1.0, 0.86, 0.44), vSeed);
        vec3 col = mix(sand, vec3(0.76, 0.96, 1.0), vNode * 0.28 + core * 0.16);
        gl_FragColor = vec4(col, alpha * (0.75 + core * 0.25));
      }
    `,
    uniforms: {
      uTime: { value: 0 },
      uModeN: { value: 3 },
      uModeM: { value: 5 },
      uSettling: { value: 0.94 },
      uVibration: { value: 0.18 },
      uFrequency: { value: 0.65 },
      uParticleSize: { value: 3.0 },
    },
    transparent: true,
    depthWrite: false,
    blending: THREE.AdditiveBlending,
  });

  const points = new THREE.Points(partGeo, partMat);
  points.frustumCulled = false;
  scene.add(points);

  const smoothN = { val: 3 };
  const smoothM = { val: 5 };
  let raf = 0;
  let lastT = performance.now();

  const animate = (now: number) => {
    const dt = Math.min((now - lastT) / 1000, 0.04);
    lastT = now;
    const t = now * 0.001;
    const p = ctx.getParams();
    const audio = ctx.getAudioPulse();

    orbit.step(dt, 0.07);
    smoothN.val = THREE.MathUtils.damp(smoothN.val, Number(p.modeN ?? 3), 4.5, dt);
    smoothM.val = THREE.MathUtils.damp(smoothM.val, Number(p.modeM ?? 5), 4.5, dt);

    const vib = Number(p.vibration ?? 0.18) + audio.bass * 0.22;
    [plateMat, partMat].forEach((m) => {
      m.uniforms.uTime.value = t;
      m.uniforms.uModeN.value = smoothN.val;
      m.uniforms.uModeM.value = smoothM.val;
      m.uniforms.uVibration.value = vib;
      m.uniforms.uFrequency.value = Number(p.frequency ?? 0.65);
    });
    partMat.uniforms.uSettling.value = Number(p.settling ?? 0.94);
    partMat.uniforms.uParticleSize.value = Number(p.particleSize ?? 3.0);

    renderer.render(scene, camera);
    ctx.onHudUpdate(`CHLADNI EIGENMODE (n=${p.modeN ?? 3}, m=${p.modeM ?? 5}) · GRAINS: 28,000`);
    raf = requestAnimationFrame(animate);
  };
  raf = requestAnimationFrame(animate);

  return {
    dispose: () => {
      cancelAnimationFrame(raf);
      orbit.dispose();
      disposeStage();
    },
  };
}

// ============================================================================
// 6. 010 - Verlet Cloth Banner (Verlet Spring Physics + Holographic Silk)
// ============================================================================
export function mountVerletClothScene(ctx: SceneRunContext): ActiveSceneHandle {
  const { renderer, scene, camera, disposeStage } = createThreeStage(ctx.container, 45);
  scene.background = new THREE.Color('#03050c');
  const orbit = attachOrbitCamera(renderer.domElement, camera, {
    yaw: 0.12,
    pitch: 0.08,
    distance: 7.4,
    minDistance: 4.0,
    maxDistance: 14,
  });

  const COLS = 32;
  const ROWS = 22;
  const W = 5.6;
  const H = 3.4;
  const count = COLS * ROWS;
  const pos = new Float32Array(count * 3);
  const prev = new Float32Array(count * 3);
  const rest = new Float32Array(count * 3);
  const pinned = new Uint8Array(count);
  const uvs = new Float32Array(count * 2);

  for (let r = 0; r < ROWS; r++) {
    const v = r / (ROWS - 1);
    for (let c = 0; c < COLS; c++) {
      const u = c / (COLS - 1);
      const idx = r * COLS + c;
      const x = (u - 0.5) * W;
      const y = (0.5 - v) * H;
      const z = Math.sin(u * Math.PI * 2) * 0.03;
      pos[idx * 3] = prev[idx * 3] = rest[idx * 3] = x;
      pos[idx * 3 + 1] = prev[idx * 3 + 1] = rest[idx * 3 + 1] = y;
      pos[idx * 3 + 2] = prev[idx * 3 + 2] = rest[idx * 3 + 2] = z;
      pinned[idx] = r === 0 ? 1 : 0;
      uvs[idx * 2] = u;
      uvs[idx * 2 + 1] = 1 - v;
    }
  }

  type Constraint = { a: number; b: number; restLen: number };
  const constraints: Constraint[] = [];
  const addC = (a: number, b: number) => {
    const dx = pos[b * 3] - pos[a * 3];
    const dy = pos[b * 3 + 1] - pos[a * 3 + 1];
    const dz = pos[b * 3 + 2] - pos[a * 3 + 2];
    constraints.push({ a, b, restLen: Math.hypot(dx, dy, dz) });
  };
  for (let r = 0; r < ROWS; r++) {
    for (let c = 0; c < COLS; c++) {
      const idx = r * COLS + c;
      if (c < COLS - 1) addC(idx, idx + 1);
      if (r < ROWS - 1) addC(idx, idx + COLS);
      if (c < COLS - 1 && r < ROWS - 1) {
        addC(idx, idx + COLS + 1);
        addC(idx + 1, idx + COLS);
      }
    }
  }

  const indices: number[] = [];
  for (let r = 0; r < ROWS - 1; r++) {
    for (let c = 0; c < COLS - 1; c++) {
      const a = r * COLS + c;
      const b = a + 1;
      const d = a + COLS;
      const e = d + 1;
      indices.push(a, d, b, b, d, e);
    }
  }

  const geo = new THREE.BufferGeometry();
  const posAttr = new THREE.BufferAttribute(pos, 3);
  posAttr.setUsage(THREE.DynamicDrawUsage);
  geo.setAttribute('position', posAttr);
  geo.setAttribute('uv', new THREE.BufferAttribute(uvs, 2));
  geo.setIndex(indices);
  geo.computeVertexNormals();

  const mat = new THREE.ShaderMaterial({
    vertexShader: `
      varying vec2 vUv;
      varying vec3 vNormal;
      varying vec3 vWorldPos;
      void main(){
        vUv = uv;
        vNormal = normalize(normalMatrix * normal);
        vec4 wp = modelMatrix * vec4(position, 1.0);
        vWorldPos = wp.xyz;
        gl_Position = projectionMatrix * viewMatrix * wp;
      }
    `,
    fragmentShader: `
      precision highp float;
      varying vec2 vUv;
      varying vec3 vNormal;
      varying vec3 vWorldPos;
      uniform float uGlowStrength;
      uniform float uTime;
      vec3 palette(float t){
        return vec3(0.5) + vec3(0.5) * cos(6.28318 * (t + vec3(0.263, 0.416, 0.557)));
      }
      void main(){
        vec3 n = normalize(vNormal);
        vec3 vDir = normalize(cameraPosition - vWorldPos);
        vec3 lDir = normalize(vec3(0.5, 1.0, 0.8));
        vec3 hVec = normalize(lDir + vDir);
        float spec = pow(max(dot(n, hVec), 0.0), 32.0);
        float rim = pow(1.0 - abs(dot(n, vDir)), 2.0);
        vec3 silk = palette(rim * 0.65 + sin(vUv.x * 5.0 + uTime * 0.35) * 0.22);
        float grid = max(
          smoothstep(0.94, 1.0, cos(vUv.x * 120.0)),
          smoothstep(0.94, 1.0, cos(vUv.y * 80.0))
        );
        vec3 col = vec3(0.02, 0.03, 0.06) + silk * (0.35 + rim * 0.65 * uGlowStrength) + vec3(1.0) * spec * 0.45 + silk * grid * 0.25;
        gl_FragColor = vec4(col, 0.92);
      }
    `,
    uniforms: {
      uGlowStrength: { value: 1.0 },
      uTime: { value: 0 },
    },
    side: THREE.DoubleSide,
    transparent: true,
  });

  const mesh = new THREE.Mesh(geo, mat);
  scene.add(mesh);

  const wireMesh = new THREE.Mesh(
    geo,
    new THREE.MeshBasicMaterial({ color: '#38bdf8', wireframe: true, transparent: true, opacity: 0.14 }),
  );
  scene.add(wireMesh);

  let rippleAge = 10;
  let rippleX = 0;
  let rippleY = 0;
  const onPointerDown = () => {
    rippleX = orbit.state.pointerNdc.x * 2.8;
    rippleY = orbit.state.pointerNdc.y * 1.7;
    rippleAge = 0;
  };
  renderer.domElement.addEventListener('pointerdown', onPointerDown);

  let raf = 0;
  let lastT = performance.now();
  const animate = (now: number) => {
    const dt = Math.min((now - lastT) / 1000, 0.033);
    lastT = now;
    const t = now * 0.001;
    const p = ctx.getParams();
    const audio = ctx.getAudioPulse();

    orbit.step(dt, 0);
    rippleAge += dt;

    const damping = Number(p.damping ?? 0.982);
    const windStr = Number(p.windStrength ?? 1.1) + audio.bass * 1.6;
    const radius = Number(p.interactionRadius ?? 1.1);
    const radiusSq = radius * radius;
    const interStr = Number(p.interactionStrength ?? 1.2);
    const ptrX = orbit.state.pointerNdc.x * 2.8;
    const ptrY = orbit.state.pointerNdc.y * 1.7;
    const dScale = dt * 60;
    const dSq = dScale * dScale;

    for (let i = 0; i < count; i++) {
      const c = i * 3;
      if (pinned[i]) {
        pos[c] = prev[c] = rest[c];
        pos[c + 1] = prev[c + 1] = rest[c + 1];
        pos[c + 2] = prev[c + 2] = rest[c + 2];
        continue;
      }
      const x = pos[c];
      const y = pos[c + 1];
      const z = pos[c + 2];
      const vx = (x - prev[c]) * damping;
      const vy = (y - prev[c + 1]) * damping;
      const vz = (z - prev[c + 2]) * damping;
      prev[c] = x;
      prev[c + 1] = y;
      prev[c + 2] = z;

      const wind = Math.sin(t * 1.6 + rest[c] * 1.9 + rest[c + 1] * 0.8) + Math.sin(t * 0.9 - rest[c] * 2.3) * 0.55;
      let ax = 0;
      let ay = -0.0024;
      let az = wind * windStr * 0.008;

      const dx = x - ptrX;
      const dy = y - ptrY;
      const d2 = dx * dx + dy * dy;
      if (d2 < radiusSq) {
        const falloff = 1 - Math.sqrt(d2) / radius;
        const f = falloff * falloff * interStr;
        ax += orbit.state.pointerVelocity.x * f * 0.15;
        ay += orbit.state.pointerVelocity.y * f * 0.15;
        az += f * 0.028;
      }

      if (rippleAge < 2.2) {
        const rDist = Math.hypot(x - rippleX, y - rippleY);
        const env = Math.exp(-Math.abs(rDist - rippleAge * 2.4) * 1.8) * Math.exp(-rippleAge * 1.4);
        az += Math.sin(rDist * 7.5 - rippleAge * 11.0) * env * 0.045;
      }

      pos[c] = x + vx + ax * dSq;
      pos[c + 1] = y + vy + ay * dSq;
      pos[c + 2] = THREE.MathUtils.clamp(z + vz + az * dSq, -1.8, 1.8);
    }

    for (let iter = 0; iter < 5; iter++) {
      for (const con of constraints) {
        const a = con.a * 3;
        const b = con.b * 3;
        const dx = pos[b] - pos[a];
        const dy = pos[b + 1] - pos[a + 1];
        const dz = pos[b + 2] - pos[a + 2];
        const d = Math.hypot(dx, dy, dz);
        if (d < 1e-5) continue;
        const diff = (d - con.restLen) / d;
        const wa = pinned[con.a] ? 0 : pinned[con.b] ? 1 : 0.5;
        const wb = pinned[con.b] ? 0 : pinned[con.a] ? 1 : 0.5;
        pos[a] += dx * diff * wa;
        pos[a + 1] += dy * diff * wa;
        pos[a + 2] += dz * diff * wa;
        pos[b] -= dx * diff * wb;
        pos[b + 1] -= dy * diff * wb;
        pos[b + 2] -= dz * diff * wb;
      }
    }

    posAttr.needsUpdate = true;
    geo.computeVertexNormals();
    mat.uniforms.uTime.value = t;
    mat.uniforms.uGlowStrength.value = Number(p.glowStrength ?? 1.0);

    renderer.render(scene, camera);
    ctx.onHudUpdate(`VERLET CLOTH ${COLS}×${ROWS} · SPRINGS: ${constraints.length} · WIND: ${windStr.toFixed(2)}`);
    raf = requestAnimationFrame(animate);
  };
  raf = requestAnimationFrame(animate);

  return {
    reset: () => {
      for (let i = 0; i < count * 3; i++) pos[i] = prev[i] = rest[i];
    },
    dispose: () => {
      cancelAnimationFrame(raf);
      renderer.domElement.removeEventListener('pointerdown', onPointerDown);
      orbit.dispose();
      disposeStage();
    },
  };
}
