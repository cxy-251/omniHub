import { useEffect, useRef, useState } from 'react';

type PresetId = 'classic' | 'neon' | 'ink' | 'plasma';

interface FluidConfig {
  simRes: number;
  dyeRes: number;
  densityDissipation: number;
  velocityDissipation: number;
  pressure: number;
  pressureIterations: number;
  curl: number;
  splatRadius: number;
  splatForce: number;
  shading: boolean;
  autoPulse: boolean;
  bgHex: string;
  palette: [number, number, number][];
}

const PRESETS: Record<PresetId, { name: string; desc: string; config: FluidConfig }> = {
  classic: {
    name: '经典虹彩流体',
    desc: '高涡度低耗散，呈现丝绸般翻涌的彩色烟雾涡街',
    config: {
      simRes: 128,
      dyeRes: 512,
      densityDissipation: 0.985,
      velocityDissipation: 0.99,
      pressure: 0.8,
      pressureIterations: 18,
      curl: 32,
      splatRadius: 0.0032,
      splatForce: 6000,
      shading: true,
      autoPulse: true,
      bgHex: '#07090e',
      palette: [[0.98, 0.28, 0.48], [0.22, 0.78, 0.98], [0.55, 0.95, 0.35], [0.98, 0.75, 0.18]],
    },
  },
  neon: {
    name: '赛博霓虹激流',
    desc: '高动量喷射与强法线高光，适合快速划动观察激波交汇',
    config: {
      simRes: 128,
      dyeRes: 512,
      densityDissipation: 0.978,
      velocityDissipation: 0.986,
      pressure: 0.85,
      pressureIterations: 20,
      curl: 44,
      splatRadius: 0.0026,
      splatForce: 7200,
      shading: true,
      autoPulse: true,
      bgHex: '#04060c',
      palette: [[1.0, 0.12, 0.65], [0.08, 0.95, 0.88], [0.52, 0.25, 1.0], [0.98, 0.92, 0.15]],
    },
  },
  ink: {
    name: '青黛水墨晕染',
    desc: '低涡度高阻尼，模拟墨滴在水中缓慢扩散的层流纹理',
    config: {
      simRes: 128,
      dyeRes: 512,
      densityDissipation: 0.992,
      velocityDissipation: 0.975,
      pressure: 0.72,
      pressureIterations: 16,
      curl: 16,
      splatRadius: 0.0042,
      splatForce: 4500,
      shading: false,
      autoPulse: true,
      bgHex: '#0b1016',
      palette: [[0.38, 0.68, 0.82], [0.22, 0.45, 0.64], [0.75, 0.86, 0.92], [0.18, 0.72, 0.66]],
    },
  },
  plasma: {
    name: '日冕等离子体',
    desc: '高湍流卷曲度与炽热光谱，模拟恒星表面磁流体对流',
    config: {
      simRes: 128,
      dyeRes: 512,
      densityDissipation: 0.982,
      velocityDissipation: 0.992,
      pressure: 0.88,
      pressureIterations: 22,
      curl: 52,
      splatRadius: 0.003,
      splatForce: 6800,
      shading: true,
      autoPulse: true,
      bgHex: '#090406',
      palette: [[1.0, 0.32, 0.08], [1.0, 0.68, 0.12], [0.92, 0.12, 0.35], [1.0, 0.88, 0.42]],
    },
  },
};

const VERT_SHADER = `
precision highp float;
attribute vec2 aPosition;
varying vec2 vUv;
varying vec2 vL;
varying vec2 vR;
varying vec2 vT;
varying vec2 vB;
uniform vec2 texelSize;
void main () {
  vUv = aPosition * 0.5 + 0.5;
  vL = vUv - vec2(texelSize.x, 0.0);
  vR = vUv + vec2(texelSize.x, 0.0);
  vT = vUv + vec2(0.0, texelSize.y);
  vB = vUv - vec2(0.0, texelSize.y);
  gl_Position = vec4(aPosition, 0.0, 1.0);
}
`;

const SPLAT_FRAG = `
precision highp float;
varying vec2 vUv;
uniform sampler2D uTarget;
uniform float aspectRatio;
uniform vec3 color;
uniform vec2 point;
uniform float radius;
void main () {
  vec2 p = vUv - point.xy;
  p.x *= aspectRatio;
  vec3 splat = exp(-dot(p, p) / radius) * color;
  vec3 base = texture2D(uTarget, vUv).xyz;
  gl_FragColor = vec4(base + splat, 1.0);
}
`;

const ADVECTION_FRAG = `
precision highp float;
varying vec2 vUv;
uniform sampler2D uVelocity;
uniform sampler2D uSource;
uniform vec2 texelSize;
uniform float dt;
uniform float dissipation;
void main () {
  vec2 coord = vUv - dt * texture2D(uVelocity, vUv).xy * texelSize;
  gl_FragColor = dissipation * texture2D(uSource, coord);
}
`;

const DIVERGENCE_FRAG = `
precision mediump float;
varying highp vec2 vUv;
varying highp vec2 vL;
varying highp vec2 vR;
varying highp vec2 vT;
varying highp vec2 vB;
uniform sampler2D uVelocity;
void main () {
  float L = texture2D(uVelocity, vL).x;
  float R = texture2D(uVelocity, vR).x;
  float T = texture2D(uVelocity, vT).y;
  float B = texture2D(uVelocity, vB).y;
  vec2 C = texture2D(uVelocity, vUv).xy;
  if (vL.x < 0.0) { L = -C.x; }
  if (vR.x > 1.0) { R = -C.x; }
  if (vT.y > 1.0) { T = -C.y; }
  if (vB.y < 0.0) { B = -C.y; }
  float div = 0.5 * (R - L + T - B);
  gl_FragColor = vec4(div, 0.0, 0.0, 1.0);
}
`;

const CURL_FRAG = `
precision mediump float;
varying highp vec2 vUv;
varying highp vec2 vL;
varying highp vec2 vR;
varying highp vec2 vT;
varying highp vec2 vB;
uniform sampler2D uVelocity;
void main () {
  float L = texture2D(uVelocity, vL).y;
  float R = texture2D(uVelocity, vR).y;
  float T = texture2D(uVelocity, vT).x;
  float B = texture2D(uVelocity, vB).x;
  float vorticity = R - L - T + B;
  gl_FragColor = vec4(0.5 * vorticity, 0.0, 0.0, 1.0);
}
`;

const VORTICITY_FRAG = `
precision highp float;
varying vec2 vUv;
varying vec2 vL;
varying vec2 vR;
varying vec2 vT;
varying vec2 vB;
uniform sampler2D uVelocity;
uniform sampler2D uCurl;
uniform float curl;
uniform float dt;
void main () {
  float L = texture2D(uCurl, vL).x;
  float R = texture2D(uCurl, vR).x;
  float T = texture2D(uCurl, vT).x;
  float B = texture2D(uCurl, vB).x;
  float C = texture2D(uCurl, vUv).x;
  vec2 force = 0.5 * vec2(abs(T) - abs(B), abs(R) - abs(L));
  force /= length(force) + 0.0001;
  force *= curl * C;
  force.y *= -1.0;
  vec2 vel = texture2D(uVelocity, vUv).xy;
  gl_FragColor = vec4(vel + force * dt, 0.0, 1.0);
}
`;

const CLEAR_FRAG = `
precision mediump float;
varying highp vec2 vUv;
uniform sampler2D uTexture;
uniform float value;
void main () {
  gl_FragColor = value * texture2D(uTexture, vUv);
}
`;

const PRESSURE_FRAG = `
precision mediump float;
varying highp vec2 vUv;
varying highp vec2 vL;
varying highp vec2 vR;
varying highp vec2 vT;
varying highp vec2 vB;
uniform sampler2D uPressure;
uniform sampler2D uDivergence;
void main () {
  float L = texture2D(uPressure, vL).x;
  float R = texture2D(uPressure, vR).x;
  float T = texture2D(uPressure, vT).x;
  float B = texture2D(uPressure, vB).x;
  float divergence = texture2D(uDivergence, vUv).x;
  float pressure = (L + R + B + T - divergence) * 0.25;
  gl_FragColor = vec4(pressure, 0.0, 0.0, 1.0);
}
`;

const GRADIENT_SUB_FRAG = `
precision mediump float;
varying highp vec2 vUv;
varying highp vec2 vL;
varying highp vec2 vR;
varying highp vec2 vT;
varying highp vec2 vB;
uniform sampler2D uPressure;
uniform sampler2D uVelocity;
void main () {
  float L = texture2D(uPressure, vL).x;
  float R = texture2D(uPressure, vR).x;
  float T = texture2D(uPressure, vT).x;
  float B = texture2D(uPressure, vB).x;
  vec2 velocity = texture2D(uVelocity, vUv).xy;
  velocity.xy -= vec2(R - L, T - B);
  gl_FragColor = vec4(velocity, 0.0, 1.0);
}
`;

const DISPLAY_FRAG = `
precision highp float;
varying vec2 vUv;
varying vec2 vL;
varying vec2 vR;
varying vec2 vT;
varying vec2 vB;
uniform sampler2D uTexture;
uniform vec2 texelSize;
uniform float uShading;
uniform vec3 uBg;
void main () {
  vec3 c = texture2D(uTexture, vUv).rgb;
  if (uShading > 0.5) {
    vec3 lc = texture2D(uTexture, vL).rgb;
    vec3 rc = texture2D(uTexture, vR).rgb;
    vec3 tc = texture2D(uTexture, vT).rgb;
    vec3 bc = texture2D(uTexture, vB).rgb;
    float dx = length(rc) - length(lc);
    float dy = length(tc) - length(bc);
    vec3 n = normalize(vec3(dx, dy, length(texelSize) * 2.2));
    vec3 l = normalize(vec3(0.35, 0.55, 1.0));
    float diffuse = clamp(dot(n, l) + 0.35, 0.65, 1.28);
    c *= diffuse;
  }
  vec3 outCol = uBg + c;
  gl_FragColor = vec4(clamp(outCol, 0.0, 1.0), 1.0);
}
`;

export default function FluidLab() {
  const [preset, setPreset] = useState<PresetId>('classic');
  const [cfg, setCfg] = useState<FluidConfig>(() => ({ ...PRESETS.classic.config }));
  const [paused, setPaused] = useState(false);

  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const cfgRef = useRef(cfg);
  cfgRef.current = cfg;
  const pausedRef = useRef(paused);
  pausedRef.current = paused;

  const actionsRef = useRef<{ burst: () => void; clear: () => void }>({ burst: () => {}, clear: () => {} });

  const applyPreset = (id: PresetId) => {
    setPreset(id);
    setCfg({ ...PRESETS[id].config });
  };

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;

    const gl2 = canvas.getContext('webgl2', { alpha: false, antialias: false, depth: false, stencil: false });
    const gl = (gl2 || canvas.getContext('webgl', { alpha: false, antialias: false, depth: false, stencil: false })) as WebGLRenderingContext | null;
    if (!gl) return;

    let internalFormat: number = gl.RGBA;
    let format: number = gl.RGBA;
    let texType: number = gl.UNSIGNED_BYTE;

    if (gl2) {
      gl2.getExtension('EXT_color_buffer_float');
      gl2.getExtension('OES_texture_float_linear');
      internalFormat = gl2.RGBA16F;
      format = gl2.RGBA;
      texType = gl2.HALF_FLOAT;
    } else {
      const halfFloat = gl.getExtension('OES_texture_half_float');
      gl.getExtension('OES_texture_half_float_linear');
      if (halfFloat) texType = halfFloat.HALF_FLOAT_OES;
    }

    const compileShader = (type: number, source: string) => {
      const s = gl.createShader(type)!;
      gl.shaderSource(s, source);
      gl.compileShader(s);
      return s;
    };

    const createProg = (fragSrc: string) => {
      const vs = compileShader(gl.VERTEX_SHADER, VERT_SHADER);
      const fs = compileShader(gl.FRAGMENT_SHADER, fragSrc);
      const prog = gl.createProgram()!;
      gl.attachShader(prog, vs);
      gl.attachShader(prog, fs);
      gl.bindAttribLocation(prog, 0, 'aPosition');
      gl.linkProgram(prog);
      gl.deleteShader(vs);
      gl.deleteShader(fs);
      const uniforms: Record<string, WebGLUniformLocation | null> = {};
      const count = gl.getProgramParameter(prog, gl.ACTIVE_UNIFORMS);
      for (let i = 0; i < count; i++) {
        const info = gl.getActiveUniform(prog, i);
        if (info) uniforms[info.name] = gl.getUniformLocation(prog, info.name);
      }
      return { prog, uniforms };
    };

    const splatProg = createProg(SPLAT_FRAG);
    const advectProg = createProg(ADVECTION_FRAG);
    const divProg = createProg(DIVERGENCE_FRAG);
    const curlProg = createProg(CURL_FRAG);
    const vortProg = createProg(VORTICITY_FRAG);
    const clearProg = createProg(CLEAR_FRAG);
    const pressureProg = createProg(PRESSURE_FRAG);
    const gradSubProg = createProg(GRADIENT_SUB_FRAG);
    const displayProg = createProg(DISPLAY_FRAG);

    const quadBuf = gl.createBuffer()!;
    gl.bindBuffer(gl.ARRAY_BUFFER, quadBuf);
    gl.bufferData(gl.ARRAY_BUFFER, new Float32Array([-1, -1, -1, 1, 1, 1, -1, -1, 1, 1, 1, -1]), gl.STATIC_DRAW);
    gl.enableVertexAttribArray(0);
    gl.vertexAttribPointer(0, 2, gl.FLOAT, false, 0, 0);

    interface FBO {
      tex: WebGLTexture;
      fbo: WebGLFramebuffer;
      w: number;
      h: number;
      attach: (unit: number) => number;
    }

    const allTextures: WebGLTexture[] = [];
    const allFbos: WebGLFramebuffer[] = [];

    const createFBO = (w: number, h: number, filter: number): FBO => {
      const tex = gl.createTexture()!;
      allTextures.push(tex);
      gl.activeTexture(gl.TEXTURE0);
      gl.bindTexture(gl.TEXTURE_2D, tex);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, filter);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, filter);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);
      gl.texImage2D(gl.TEXTURE_2D, 0, internalFormat, w, h, 0, format, texType, null);

      const fbo = gl.createFramebuffer()!;
      allFbos.push(fbo);
      gl.bindFramebuffer(gl.FRAMEBUFFER, fbo);
      gl.framebufferTexture2D(gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.TEXTURE_2D, tex, 0);
      const status = gl.checkFramebufferStatus(gl.FRAMEBUFFER);
      if (status !== gl.FRAMEBUFFER_COMPLETE && internalFormat !== gl.RGBA) {
        internalFormat = gl.RGBA;
        texType = gl.UNSIGNED_BYTE;
        gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, w, h, 0, gl.RGBA, gl.UNSIGNED_BYTE, null);
      }
      gl.viewport(0, 0, w, h);
      gl.clear(gl.COLOR_BUFFER_BIT);

      return {
        tex,
        fbo,
        w,
        h,
        attach: (unit: number) => {
          gl.activeTexture(gl.TEXTURE0 + unit);
          gl.bindTexture(gl.TEXTURE_2D, tex);
          return unit;
        },
      };
    };

    const createDoubleFBO = (w: number, h: number, filter: number) => {
      let fbo1 = createFBO(w, h, filter);
      let fbo2 = createFBO(w, h, filter);
      return {
        w,
        h,
        get read() { return fbo1; },
        get write() { return fbo2; },
        swap() { const t = fbo1; fbo1 = fbo2; fbo2 = t; },
      };
    };

    const simW = 128, simH = 128;
    const dyeW = 512, dyeH = 512;
    const velocity = createDoubleFBO(simW, simH, gl.LINEAR);
    const dye = createDoubleFBO(dyeW, dyeH, gl.LINEAR);
    const divergence = createFBO(simW, simH, gl.NEAREST);
    const curlFbo = createFBO(simW, simH, gl.NEAREST);
    const pressureFbo = createDoubleFBO(simW, simH, gl.NEAREST);

    const blit = (target: FBO | null) => {
      if (target) {
        gl.viewport(0, 0, target.w, target.h);
        gl.bindFramebuffer(gl.FRAMEBUFFER, target.fbo);
      } else {
        gl.viewport(0, 0, canvas.width, canvas.height);
        gl.bindFramebuffer(gl.FRAMEBUFFER, null);
      }
      gl.drawArrays(gl.TRIANGLES, 0, 6);
    };

    const splat = (x: number, y: number, dx: number, dy: number, col: [number, number, number]) => {
      const c = cfgRef.current;
      const aspect = canvas.width / Math.max(1, canvas.height);
      gl.useProgram(splatProg.prog);
      gl.uniform1i(splatProg.uniforms.uTarget, velocity.read.attach(0));
      gl.uniform1f(splatProg.uniforms.aspectRatio, aspect);
      gl.uniform2f(splatProg.uniforms.point, x, y);
      gl.uniform3f(splatProg.uniforms.color, dx, dy, 0);
      gl.uniform1f(splatProg.uniforms.radius, c.splatRadius);
      blit(velocity.write);
      velocity.swap();

      gl.uniform1i(splatProg.uniforms.uTarget, dye.read.attach(0));
      gl.uniform3f(splatProg.uniforms.color, col[0], col[1], col[2]);
      blit(dye.write);
      dye.swap();
    };

    const burstMultiple = (count = 5) => {
      const pal = cfgRef.current.palette;
      for (let i = 0; i < count; i++) {
        const col = pal[i % pal.length];
        const angle = (Math.PI * 2 * i) / count + Math.random() * 0.4;
        const speed = 360 + Math.random() * 260;
        splat(
          0.32 + Math.random() * 0.36,
          0.32 + Math.random() * 0.36,
          Math.cos(angle) * speed,
          Math.sin(angle) * speed,
          col
        );
      }
    };

    const clearAll = () => {
      gl.useProgram(clearProg.prog);
      gl.uniform1f(clearProg.uniforms.value, 0);
      gl.uniform1i(clearProg.uniforms.uTexture, dye.read.attach(0));
      blit(dye.write);
      dye.swap();
      gl.uniform1i(clearProg.uniforms.uTexture, velocity.read.attach(0));
      blit(velocity.write);
      velocity.swap();
    };

    actionsRef.current = { burst: () => burstMultiple(6), clear: clearAll };

    // 初始爆发
    burstMultiple(6);

    let rafId = 0;
    let lastTime = performance.now();
    let pulseTimer = 0;

    const step = (now: number) => {
      const dt = Math.min(0.016, (now - lastTime) / 1000);
      lastTime = now;

      const dpr = Math.min(window.devicePixelRatio || 1, 2);
      const rect = canvas.getBoundingClientRect();
      const w = Math.max(300, Math.floor(rect.width * dpr));
      const h = Math.max(260, Math.floor(rect.height * dpr));
      if (canvas.width !== w || canvas.height !== h) {
        canvas.width = w;
        canvas.height = h;
      }

      const c = cfgRef.current;

      if (!pausedRef.current) {
        pulseTimer += dt;
        if (c.autoPulse && pulseTimer > 2.2) {
          pulseTimer = 0;
          const col = c.palette[Math.floor(Math.random() * c.palette.length)];
          const a = Math.random() * Math.PI * 2;
          splat(0.25 + Math.random() * 0.5, 0.25 + Math.random() * 0.5, Math.cos(a) * 320, Math.sin(a) * 320, col);
        }

        // 1. Curl
        gl.useProgram(curlProg.prog);
        gl.uniform2f(curlProg.uniforms.texelSize, 1 / simW, 1 / simH);
        gl.uniform1i(curlProg.uniforms.uVelocity, velocity.read.attach(0));
        blit(curlFbo);

        // 2. Vorticity confinement
        gl.useProgram(vortProg.prog);
        gl.uniform2f(vortProg.uniforms.texelSize, 1 / simW, 1 / simH);
        gl.uniform1i(vortProg.uniforms.uVelocity, velocity.read.attach(0));
        gl.uniform1i(vortProg.uniforms.uCurl, curlFbo.attach(1));
        gl.uniform1f(vortProg.uniforms.curl, c.curl);
        gl.uniform1f(vortProg.uniforms.dt, dt);
        blit(velocity.write);
        velocity.swap();

        // 3. Divergence
        gl.useProgram(divProg.prog);
        gl.uniform2f(divProg.uniforms.texelSize, 1 / simW, 1 / simH);
        gl.uniform1i(divProg.uniforms.uVelocity, velocity.read.attach(0));
        blit(divergence);

        // 4. Clear pressure
        gl.useProgram(clearProg.prog);
        gl.uniform1i(clearProg.uniforms.uTexture, pressureFbo.read.attach(0));
        gl.uniform1f(clearProg.uniforms.value, c.pressure);
        blit(pressureFbo.write);
        pressureFbo.swap();

        // 5. Jacobi iterations
        gl.useProgram(pressureProg.prog);
        gl.uniform2f(pressureProg.uniforms.texelSize, 1 / simW, 1 / simH);
        gl.uniform1i(pressureProg.uniforms.uDivergence, divergence.attach(0));
        for (let i = 0; i < c.pressureIterations; i++) {
          gl.uniform1i(pressureProg.uniforms.uPressure, pressureFbo.read.attach(1));
          blit(pressureFbo.write);
          pressureFbo.swap();
        }

        // 6. Subtract pressure gradient
        gl.useProgram(gradSubProg.prog);
        gl.uniform2f(gradSubProg.uniforms.texelSize, 1 / simW, 1 / simH);
        gl.uniform1i(gradSubProg.uniforms.uPressure, pressureFbo.read.attach(0));
        gl.uniform1i(gradSubProg.uniforms.uVelocity, velocity.read.attach(1));
        blit(velocity.write);
        velocity.swap();

        // 7. Advect velocity & dye
        gl.useProgram(advectProg.prog);
        gl.uniform2f(advectProg.uniforms.texelSize, 1 / simW, 1 / simH);
        gl.uniform1i(advectProg.uniforms.uVelocity, velocity.read.attach(0));
        gl.uniform1i(advectProg.uniforms.uSource, velocity.read.attach(0));
        gl.uniform1f(advectProg.uniforms.dt, dt);
        gl.uniform1f(advectProg.uniforms.dissipation, c.velocityDissipation);
        blit(velocity.write);
        velocity.swap();

        gl.uniform2f(advectProg.uniforms.texelSize, 1 / dyeW, 1 / dyeH);
        gl.uniform1i(advectProg.uniforms.uVelocity, velocity.read.attach(0));
        gl.uniform1i(advectProg.uniforms.uSource, dye.read.attach(1));
        gl.uniform1f(advectProg.uniforms.dissipation, c.densityDissipation);
        blit(dye.write);
        dye.swap();
      }

      // 8. Display
      const bgInt = parseInt(c.bgHex.slice(1), 16);
      gl.useProgram(displayProg.prog);
      gl.uniform2f(displayProg.uniforms.texelSize, 1 / canvas.width, 1 / canvas.height);
      gl.uniform1i(displayProg.uniforms.uTexture, dye.read.attach(0));
      gl.uniform1f(displayProg.uniforms.uShading, c.shading ? 1.0 : 0.0);
      gl.uniform3f(
        displayProg.uniforms.uBg,
        ((bgInt >> 16) & 255) / 255,
        ((bgInt >> 8) & 255) / 255,
        (bgInt & 255) / 255
      );
      blit(null);

      rafId = window.requestAnimationFrame(step);
    };

    rafId = window.requestAnimationFrame(step);

    // 指针交互监听
    let pointerDown = false;
    let lastX = 0;
    let lastY = 0;
    let colorIdx = 0;

    const onDown = (e: PointerEvent) => {
      pointerDown = true;
      const r = canvas.getBoundingClientRect();
      lastX = (e.clientX - r.left) / r.width;
      lastY = 1.0 - (e.clientY - r.top) / r.height;
      colorIdx = (colorIdx + 1) % cfgRef.current.palette.length;
    };
    const onMove = (e: PointerEvent) => {
      const r = canvas.getBoundingClientRect();
      const nx = (e.clientX - r.left) / r.width;
      const ny = 1.0 - (e.clientY - r.top) / r.height;
      const dx = (nx - lastX) * cfgRef.current.splatForce;
      const dy = (ny - lastY) * cfgRef.current.splatForce;
      lastX = nx;
      lastY = ny;
      if (Math.hypot(dx, dy) > 1.5) {
        const pal = cfgRef.current.palette;
        splat(nx, ny, dx, dy, pal[colorIdx % pal.length]);
      }
    };
    const onUp = () => { pointerDown = false; };

    canvas.addEventListener('pointerdown', onDown);
    canvas.addEventListener('pointermove', onMove);
    window.addEventListener('pointerup', onUp);

    return () => {
      window.cancelAnimationFrame(rafId);
      canvas.removeEventListener('pointerdown', onDown);
      canvas.removeEventListener('pointermove', onMove);
      window.removeEventListener('pointerup', onUp);
      allTextures.forEach(t => gl.deleteTexture(t));
      allFbos.forEach(f => gl.deleteFramebuffer(f));
      gl.deleteBuffer(quadBuf);
      [splatProg, advectProg, divProg, curlProg, vortProg, clearProg, pressureProg, gradSubProg, displayProg].forEach(p =>
        gl.deleteProgram(p.prog)
      );
    };
  }, []);

  return (
    <div className="grid gap-4 lg:grid-cols-[1fr_340px]">
      <div className="relative flex min-h-[480px] flex-col overflow-hidden rounded-xl border border-line bg-[#07090e]">
        <div className="z-10 flex flex-wrap items-center justify-between gap-2 border-b border-white/10 bg-black/45 px-4 py-2.5 backdrop-blur">
          <div className="flex flex-wrap items-center gap-2">
            {(Object.keys(PRESETS) as PresetId[]).map(id => (
              <button
                key={id}
                onClick={() => applyPreset(id)}
                className={`rounded-md px-2.5 py-1 text-xs font-medium transition ${
                  preset === id ? 'bg-primary text-white' : 'border border-white/15 text-slate-300 hover:border-primary'
                }`}
              >
                {PRESETS[id].name}
              </button>
            ))}
          </div>
          <div className="flex items-center gap-2 text-xs">
            <button
              className="rounded-md bg-primary px-2.5 py-1 text-white hover:opacity-90"
              onClick={() => actionsRef.current.burst()}
            >
              脉冲喷射
            </button>
            <button
              className="rounded-md border border-white/15 px-2.5 py-1 text-slate-200 hover:border-primary"
              onClick={() => setPaused(p => !p)}
            >
              {paused ? '继续演化' : '暂停演化'}
            </button>
            <button
              className="rounded-md border border-white/15 px-2.5 py-1 text-slate-200 hover:border-primary"
              onClick={() => actionsRef.current.clear()}
            >
              清空画布
            </button>
          </div>
        </div>

        <canvas ref={canvasRef} className="h-[460px] w-full flex-1 cursor-crosshair touch-none" />

        <div className="z-10 border-t border-white/10 bg-black/45 px-4 py-2 text-xs text-slate-300 backdrop-blur">
          {PRESETS[preset].desc} · 鼠标或手指在画布上划动即可注入动量与染料涡旋
        </div>
      </div>

      <div className="space-y-4">
        <section className="rounded-xl border border-line bg-panel p-4 space-y-3">
          <h3 className="text-sm font-semibold">Navier-Stokes 求解器参数</h3>

          <label className="block space-y-1 text-xs">
            <div className="flex justify-between">
              <span className="text-muted">涡度卷曲强度 (Vorticity Curl)</span>
              <span>{cfg.curl}</span>
            </div>
            <input
              type="range"
              min="0"
              max="65"
              step="1"
              value={cfg.curl}
              onChange={e => setCfg(c => ({ ...c, curl: Number(e.target.value) }))}
              className="w-full"
            />
          </label>

          <label className="block space-y-1 text-xs">
            <div className="flex justify-between">
              <span className="text-muted">染料保持率 (Density Retention)</span>
              <span>{cfg.densityDissipation.toFixed(3)}</span>
            </div>
            <input
              type="range"
              min="0.94"
              max="0.998"
              step="0.002"
              value={cfg.densityDissipation}
              onChange={e => setCfg(c => ({ ...c, densityDissipation: Number(e.target.value) }))}
              className="w-full"
            />
          </label>

          <label className="block space-y-1 text-xs">
            <div className="flex justify-between">
              <span className="text-muted">速度场保持率 (Velocity Retention)</span>
              <span>{cfg.velocityDissipation.toFixed(3)}</span>
            </div>
            <input
              type="range"
              min="0.94"
              max="0.998"
              step="0.002"
              value={cfg.velocityDissipation}
              onChange={e => setCfg(c => ({ ...c, velocityDissipation: Number(e.target.value) }))}
              className="w-full"
            />
          </label>

          <label className="block space-y-1 text-xs">
            <div className="flex justify-between">
              <span className="text-muted">雅可比压力迭代次数 (Jacobi Steps)</span>
              <span>{cfg.pressureIterations}</span>
            </div>
            <input
              type="range"
              min="6"
              max="32"
              step="2"
              value={cfg.pressureIterations}
              onChange={e => setCfg(c => ({ ...c, pressureIterations: Number(e.target.value) }))}
              className="w-full"
            />
          </label>

          <label className="block space-y-1 text-xs">
            <div className="flex justify-between">
              <span className="text-muted">笔触作用半径 (Splat Radius)</span>
              <span>{(cfg.splatRadius * 1000).toFixed(1)}</span>
            </div>
            <input
              type="range"
              min="0.001"
              max="0.008"
              step="0.0004"
              value={cfg.splatRadius}
              onChange={e => setCfg(c => ({ ...c, splatRadius: Number(e.target.value) }))}
              className="w-full"
            />
          </label>

          <div className="flex flex-col gap-2 pt-2 text-xs">
            <label className="flex items-center justify-between">
              <span>表面梯度法线光照着色</span>
              <input
                type="checkbox"
                checked={cfg.shading}
                onChange={e => setCfg(c => ({ ...c, shading: e.target.checked }))}
              />
            </label>
            <label className="flex items-center justify-between">
              <span>自动周期脉冲注入</span>
              <input
                type="checkbox"
                checked={cfg.autoPulse}
                onChange={e => setCfg(c => ({ ...c, autoPulse: e.target.checked }))}
              />
            </label>
          </div>
        </section>
      </div>
    </div>
  );
}
