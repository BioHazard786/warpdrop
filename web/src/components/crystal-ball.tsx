import { useTheme } from "@/components/theme-provider"
import { cn } from "@/lib/utils"
import { useEffect, useRef } from "react"

// Trimmed from React Bits' CrystalizedBall (ember preset, white, round dust): no pointer interaction, no ogl,
// one fixed look. Plain WebGL2: an additive field pass + a dust point pass into a float target,
// then a tone-map composite. All geometry comes from gl_VertexID, so there are no vertex buffers.

type Rgb = [number, number, number]
const g = (v: number): Rgb => [v, v, v]

// white dust; on a light page white dust is invisible, so it inks in neutral grays instead
const PALETTES = {
  dark: {
    rim: g(1),
    rimHot: g(1),
    rimMid: g(0.85),
    rimDeep: g(0.55),
    spark: g(1),
    sparkGlow: g(0.806),
    haze: g(0.4),
    edge: g(0.62),
    tones: [0.921, 0.974, 0.844, 1, 1],
  },
  light: {
    rim: g(0.52),
    rimHot: g(0.52),
    rimMid: g(0.52),
    rimDeep: g(0.64),
    spark: g(0.5),
    sparkGlow: g(0.71),
    haze: g(0.74),
    edge: g(0.64),
    tones: [0.45, 0.49, 0.38, 0.57, 0.66],
  },
}

const STRANDS = 5
const MAX_ARCS = 4
const STATE_WIDTH = 256
const INTRO_SECONDS = 2.2
const DUST_SPEED = 1.6

const clamp = (v: number, lo: number, hi: number) => Math.min(Math.max(v, lo), hi)
const smooth = (a: number, b: number, v: number) => {
  const t = clamp((v - a) / (b - a), 0, 1)
  return t * t * (3 - 2 * t)
}
const easeOut = (t: number) => 1 - (1 - t) ** 3

const seeded = (start: number) => {
  let state = start >>> 0
  return () => {
    state = (state + 0x6d2b79f5) >>> 0
    let t = state
    t = Math.imul(t ^ (t >>> 15), t | 1)
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61)
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296
  }
}

const buildStrands = () => {
  const random = seeded(99)
  const bands = [
    [6, 12, 0.6, 1.6],
    [18, 34, 1.5, 3.5],
    [40, 70, 3, 7],
    [80, 130, 6, 12],
  ]
  const d = {
    harmonics: [] as number[],
    rates: [] as number[],
    phases: [] as number[],
    flareHarmonics: [] as number[],
    flareRates: [] as number[],
    flarePhases: [] as number[],
  }
  for (let s = 0; s < STRANDS; s++) {
    for (const [low, high, slow, fast] of bands) {
      d.harmonics.push(Math.round(low + random() * (high - low)))
      d.rates.push((slow + random() * (fast - slow)) * (random() < 0.5 ? -1 : 1))
      d.phases.push(random() * Math.PI * 2)
    }
    for (let j = 0; j < 3; j++) {
      d.flareHarmonics.push(4 + Math.floor(random() * 6))
      d.flareRates.push((0.4 + random() * 0.8) * (random() < 0.5 ? -1 : 1))
      d.flarePhases.push(random() * Math.PI * 2)
    }
    const lead = s === 0
    d.flareHarmonics.push(lead ? 0.75 : 0.95 + random() * 0.3)
    d.flareRates.push(lead ? 1.15 : 0.8 + random() * 0.2)
    d.flarePhases.push(lead ? 1 : 0.7 + random() * 0.25)
  }
  return d
}

// Rejection-samples dust into a settled bowl plus a band under the rim. xyz = home, w = tone.
const buildDust = (count: number) => {
  const random = seeded(1337)
  const rows = Math.max(1, Math.ceil(count / STATE_WIDTH))
  const home = new Float32Array(STATE_WIDTH * rows * 4)
  const seed = new Float32Array(STATE_WIDTH * rows * 4)
  let k = 0
  for (let guard = 0; k < count && guard < count * 80; guard++) {
    let x: number, y: number, tone: number
    if (random() < 0.28) {
      const angle = random() * Math.PI * 2
      const radius = 0.87 + Math.sqrt(random()) * 0.105
      x = Math.cos(angle) * radius
      y = Math.sin(angle) * radius
      if (random() > smooth(-0.6, 0.3, -y)) continue
      const q = random()
      tone = q < 0.5 ? 3 : q < 0.8 ? 2 : 1
    } else {
      x = random() * 2 - 1
      y = random() * 2 - 1
      const radius = Math.hypot(x, y)
      if (radius > 0.975) continue
      const bowl = smooth(-0.25, 0.85, -y) ** 1.3
      const band = smooth(0.66, 0.96, radius) * smooth(-0.7, 0.3, -y)
      const weight = Math.max(bowl, band * 0.9)
      if (random() > 0.012 + 0.988 * weight) continue
      if (weight < 0.12) tone = 4
      else {
        const q = random()
        tone = q < 0.32 ? 0 : q < 0.52 ? 1 : q < 0.8 ? 2 : 3
      }
    }
    const chord = Math.sqrt(Math.max(0, 0.95 - x * x - y * y))
    home.set([x, y, (random() * 2 - 1) * chord, tone], k * 4)
    seed.set(
      [
        tone === 4 ? 1.8 : random() < 0.22 ? 2.1 : 1.3,
        random(),
        2.5 + random() * 3.5,
        0.16 + random() * 0.26,
      ],
      k * 4,
    )
    k++
  }
  return { home, seed, count: k, rows }
}

const fullscreen = `#version 300 es
void main() {
  vec2 p = vec2((gl_VertexID << 1) & 2, gl_VertexID & 2);
  gl_Position = vec4(p * 2.0 - 1.0, 0.0, 1.0);
}`

const hash = `
uint scramble(uint v) {
  v = v * 747796405u + 2891336453u;
  uint w = ((v >> ((v >> 28u) + 4u)) ^ v) * 277803737u;
  return (w >> 22u) ^ w;
}
float random(uint v) { return float(scramble(v)) * (1.0 / 4294967295.0); }`

const fieldFragment = `#version 300 es
precision highp float;
uniform vec2 uCenter;
uniform float uRadius, uDpr, uLine, uTime, uFrame, uBins, uCrackle, uGlow, uHaze;
uniform float uPresence, uUnfold, uBloom, uInside, uEncode;
uniform vec3 uRim, uRimHot, uRimMid, uRimDeep, uSpark, uSparkGlow, uHazeColor, uEdgeColor;
uniform vec4 uHarmonics[${STRANDS}], uRates[${STRANDS}], uPhases[${STRANDS}];
uniform vec4 uFlareHarmonics[${STRANDS}], uFlareRates[${STRANDS}], uFlarePhases[${STRANDS}];
uniform vec4 uArcs[${MAX_ARCS}];
out vec4 fragColor;
${hash}
const float PI = 3.14159265;
const float TAU = 6.28318531;
const vec4 AMPS = vec4(0.35, 0.3, 0.22, 0.13);
const float FLARES = 0.8;
const float GAIN = 0.48; // min(1, 2.4 / strands)

float bell(float d, float w) { float x = d / w; return exp(-x * x); }

float pool(vec2 q) {
  float t = length(q - vec2(0.0, -0.881)) / 0.9925; // fill 0.35
  return t < 0.6 ? mix(0.5, 0.22, t / 0.6) : mix(0.22, 0.0, clamp((t - 0.6) / 0.4, 0.0, 1.0));
}

void main() {
  vec2 p = gl_FragCoord.xy / uDpr - uCenter;
  float r = length(p);
  float R = uRadius;
  float theta = atan(p.y, p.x);
  float topDim = 1.0 - 0.35 * pow(max(p.y / max(r, 1e-3), 0.0), 1.5);
  vec3 col = vec3(0.0);
  float hot = 0.0;
  uint frame = uint(uFrame);

  if (r < R) {
    vec2 q = p / R;
    float chord = sqrt(max(1.0 - dot(q, q), 0.0));
    col += uHazeColor * pool(q) * (0.55 + 0.45 * chord) * uHaze * uInside;
    float edge = 0.5 * pow(smoothstep(0.62, 1.0, length(q - vec2(0.0, 0.2)) / 1.2), 1.4);
    col += uEdgeColor * edge * uInside * (0.5 + 0.5 * uHaze);
  }

  float dr = r - R;
  float halo = 1.0 - smoothstep(R * 0.05, R * 0.105, abs(dr - R * 0.0213));
  col += uRimDeep * 0.18 * halo * topDim * uGlow * uBloom;
  col += uRim * 0.12 * bell(dr + R * 0.0383, R * 0.032) * uGlow * uBloom;
  col += uRim * 0.32 * bell(dr, max(R * 0.0117, uLine)) * uPresence * uUnfold;

  float amp = R * 0.016 * (0.3 + 1.4 * uCrackle) * uUnfold;
  float jitterAmp = R * 0.0048 * (0.3 + 1.4 * uCrackle) * uUnfold;
  float flareAmp = R * 0.064 * (0.4 + 1.2 * uCrackle) * FLARES * 1.6 * uUnfold;

  if (abs(dr) < amp * 2.0 + flareAmp + R * 0.3) {
    float step = TAU / uBins;
    float binPos = (theta + PI) / step;
    float bin0 = floor(binPos);
    float binF = binPos - bin0;
    float th0 = bin0 * step - PI;
    float th1 = th0 + step;
    uint b0 = uint(mod(bin0, uBins));
    uint b1 = uint(mod(bin0 + 1.0, uBins));
    for (int k = 0; k < ${STRANDS}; k++) {
      vec4 lead = uRates[k] * uTime + uPhases[k];
      float wave0 = dot(AMPS, sin(uHarmonics[k] * th0 + lead));
      float wave1 = dot(AMPS, sin(uHarmonics[k] * th1 + lead));
      uint salt = uint(k) * 1013u + frame * 7919u;
      vec4 shape = uFlareHarmonics[k];
      vec4 look = uFlareRates[k];
      vec4 tone = uFlarePhases[k];
      float a = amp * shape.w;
      float off0 = a * wave0 + jitterAmp * (random(b0 + salt) - 0.5);
      float off1 = a * wave1 + jitterAmp * (random(b1 + salt) - 0.5);
      float rc = R + mix(off0, off1, binF);
      float grade = (off1 - off0) / (step * max(r, 1.0));
      float d = abs(r - rc) * inversesqrt(1.0 + grade * grade);
      float swell = 0.8 + 0.4 * (0.5 + 0.5 * sin(3.0 * theta + uTime * 0.7 + tone.x * 1.7));
      float width = uLine * look.w * swell;
      float core = exp(-d * d / (width * width));
      float sheath = exp(-d * d / (width * width * 3.0));
      float bright = tone.w * uPresence;
      col += (uRimHot * core * 0.8 + uRim * sheath * 0.32) * bright;
      hot += core * bright;
      vec3 fargs = shape.xyz * theta + look.xyz * uTime + tone.xyz;
      float flare = max(0.0, (sin(fargs.x) + sin(fargs.y) + sin(fargs.z)) / 3.0);
      flare = flare * flare * flareAmp;
      float dw = abs(r - rc - flare);
      vec3 glow = uRimDeep * (0.07 * bell(dw, R * 0.09) + 0.16 * bell(dw, R * 0.05));
      glow += uRimMid * 0.28 * bell(abs(r - rc - flare * 0.6), R * 0.027);
      glow += uRim * 0.38 * bell(abs(r - rc - flare * 0.25), R * 0.013);
      col += glow * GAIN * topDim * uGlow * uBloom;
    }
  }

  for (int i = 0; i < ${MAX_ARCS}; i++) {
    vec4 arc = uArcs[i];
    if (arc.w <= 0.002) continue;
    float mid = arc.x + arc.y * 0.5;
    if (distance(p, R * 0.93 * vec2(cos(mid), sin(mid))) > R * (abs(arc.y) * 0.6 + 0.1) + 12.0) continue;
    float ar = R * 0.955;
    vec2 a0 = ar * vec2(cos(arc.x), sin(arc.x));
    vec2 a2 = ar * vec2(cos(arc.x + arc.y), sin(arc.x + arc.y));
    vec2 a1 = R * (0.955 - arc.z) * vec2(cos(mid), sin(mid));
    float dmin = 1e5;
    float along = 0.0;
    vec2 prev = a0;
    for (int s = 1; s <= 10; s++) {
      float u = float(s) / 10.0;
      vec2 b = mix(mix(a0, a1, u), mix(a1, a2, u), u);
      vec2 pa = p - prev;
      vec2 ba = b - prev;
      float h = clamp(dot(pa, ba) / max(dot(ba, ba), 1e-5), 0.0, 1.0);
      float dist = length(pa - ba * h);
      if (dist < dmin) { dmin = dist; along = (float(s) - 1.0 + h) / 10.0; }
      prev = b;
    }
    float taper = sqrt(max(sin(PI * along), 0.0));
    float arcWidth = uLine * (0.55 + 0.6 * taper);
    float arcCore = exp(-dmin * dmin / (arcWidth * arcWidth));
    float arcGlow = exp(-dmin * dmin / (arcWidth * arcWidth * 12.0));
    col += (uSpark * 0.95 * arcCore + uSparkGlow * 0.4 * arcGlow) * arc.w * taper;
    hot += arcCore * arc.w * taper * 0.6;
  }

  fragColor = vec4(col, hot) * uEncode;
}`

const dustVertex = `#version 300 es
precision highp float;
uniform sampler2D tHome, tSeed;
uniform float uDustTime, uRadius, uDpr, uPointScale, uReveal, uEncode;
uniform vec2 uTurn, uCenter, uViewport;
uniform vec3 uTones[5];
uniform vec3 uRimTone;
out vec3 vColor;
out float vSize;
${hash}
const float DEPTH = 0.7;
const float TWINKLE = 0.7;
const float BEND = 1.2058; // exp2((0.5 - fill) * 1.8) at fill 0.35: dust sinks lower in the ball

// squashes the dust column down toward the floor, thinning it where it stretches
vec3 settle(vec3 p, out float spread) {
  float h = clamp((p.y + 1.0) * 0.5, 0.0, 1.0);
  float y = 2.0 * pow(h, BEND) - 1.0;
  float from = sqrt(max(1.0 - p.y * p.y, 1e-4));
  float to = sqrt(max(1.0 - y * y, 0.0));
  spread = clamp(BEND * pow(max(h, 1e-3), BEND - 1.0) * to / from, 0.25, 1.0);
  return vec3(p.xz * (to / from), y).xzy;
}

void main() {
  ivec2 cell = ivec2(gl_VertexID % ${STATE_WIDTH}, gl_VertexID / ${STATE_WIDTH});
  uint id = uint(gl_VertexID);
  vec4 home = texelFetch(tHome, cell, 0);
  vec4 seed = texelFetch(tSeed, cell, 0);
  float t = uDustTime;
  float spread;
  vec3 p = settle(home.xyz, spread);
  float u = fract(t / seed.z + seed.y);
  float life = sin(3.14159265 * u);
  p.y += seed.w * (u - 0.5); // rise
  float wobble = 0.002 + 0.006 * random(id * 7u + 1u);
  float w1 = 0.8 + 2.4 * random(id * 7u + 2u);
  float w2 = 0.8 + 2.4 * random(id * 7u + 4u);
  p += wobble * vec3(sin(t * w1 + seed.y * 40.0), cos(t * w2 + seed.z * 30.0), sin(t * (w1 + w2) * 0.5 + seed.w * 20.0));
  p.xz = mat2(cos(uTurn.x), -sin(uTurn.x), sin(uTurn.x), cos(uTurn.x)) * p.xz;
  p.yz = mat2(cos(uTurn.y), -sin(uTurn.y), sin(uTurn.y), cos(uTurn.y)) * p.yz;
  float inside = 1.0 - smoothstep(0.955, 0.985, length(p.xy));
  float len = length(p);
  if (len > 0.975) p *= 0.975 / len;

  float front = p.z * 0.5 + 0.5;
  float blink = sin(t * (2.0 + 6.0 * random(id * 7u + 5u)) + random(id * 7u + 6u) * 6.2831853);
  float twinkle = mix(1.0, smoothstep(-0.9, -0.6, blink), TWINKLE);
  float order = (home.y + 1.0) * 0.5 * 0.55 + random(id * 7u + 8u) * 0.25;
  float reveal = smoothstep(order, order + 0.2, uReveal);
  float alpha = random(id * 7u + 9u) < 0.5 || home.w > 3.5 ? 1.0 : 0.6;

  float px = seed.x * uPointScale * (0.35 + 0.95 * life) * mix(1.0, 0.7 + 0.6 * front, DEPTH) * mix(0.5, 1.0, reveal) * uDpr;
  gl_Position = vec4((uCenter + p.xy * uRadius) / uViewport * 2.0 - 1.0, 0.0, 1.0);
  gl_PointSize = px + 2.0;
  vSize = px;
  float rimLit = smoothstep(0.8, 0.97, length(p.xy));
  vec3 color = uTones[int(home.w + 0.5)] + uRimTone * rimLit * rimLit * 0.12;
  vColor = color * alpha * smoothstep(0.04, 0.12, life) * twinkle * mix(1.0, 0.35 + 0.8 * front, DEPTH) * reveal * inside * sqrt(spread) * uEncode;
}`

const dustFragment = `#version 300 es
precision highp float;
in vec3 vColor;
in float vSize;
out vec4 fragColor;
void main() {
  vec2 q = (gl_PointCoord - 0.5) * (vSize + 2.0);
  float cover = clamp(max(vSize * 0.5, 0.5) - length(q) + 0.5, 0.0, 1.0); // round
  if (cover <= 0.0) discard;
  fragColor = vec4(vColor * cover, 0.0);
}`

const compositeFragment = `#version 300 es
precision highp float;
uniform sampler2D tScene;
uniform float uLight, uDecode;
out vec4 fragColor;
vec3 soften(vec3 x) {
  vec3 over = max(x - 0.6, 0.0);
  return min(x, 0.6) + 0.4 * (1.0 - exp(-over / 0.4));
}
void main() {
  vec4 scene = texelFetch(tScene, ivec2(gl_FragCoord.xy), 0) * uDecode;
  vec3 light = max(scene.rgb, 0.0);
  float peak = max(light.r, max(light.g, light.b));
  if (uLight > 0.5) {
    // monochrome: ink near-black (the original inks in the preset's hue, which is white here)
    float cover = 1.0 - exp(-peak * 2.4);
    float core = clamp(scene.a * 0.7, 0.0, 1.0) * cover;
    float alpha = core + cover * (1.0 - core);
    fragColor = vec4(vec3(0.09) * alpha, alpha);
    return;
  }
  vec3 shown = soften(light + vec3(max(peak - 1.8, 0.0) * 0.25));
  fragColor = vec4(shown, max(shown.r, max(shown.g, shown.b)));
}`

type Program = { p: WebGLProgram; u: Record<string, WebGLUniformLocation> }

const compile = (gl: WebGL2RenderingContext, vs: string, fs: string): Program | null => {
  const p = gl.createProgram()
  for (const [type, src] of [
    [gl.VERTEX_SHADER, vs],
    [gl.FRAGMENT_SHADER, fs],
  ] as const) {
    const s = gl.createShader(type)!
    gl.shaderSource(s, src)
    gl.compileShader(s)
    gl.attachShader(p, s)
    gl.deleteShader(s)
  }
  gl.linkProgram(p)
  if (!gl.getProgramParameter(p, gl.LINK_STATUS)) {
    console.warn("crystal-ball:", gl.getProgramInfoLog(p))
    return null
  }
  const u: Program["u"] = {}
  for (let i = 0, n = gl.getProgramParameter(p, gl.ACTIVE_UNIFORMS); i < n; i++) {
    const name = gl.getActiveUniform(p, i)!.name.replace(/\[0\]$/, "")
    u[name] = gl.getUniformLocation(p, name)!
  }
  return { p, u }
}

const texture = (
  gl: WebGL2RenderingContext,
  w: number,
  h: number,
  internal: number,
  format: number,
  type: number,
  data: ArrayBufferView | null,
) => {
  const t = gl.createTexture()
  gl.bindTexture(gl.TEXTURE_2D, t)
  gl.texImage2D(gl.TEXTURE_2D, 0, internal, w, h, 0, format, type, data)
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.NEAREST)
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.NEAREST)
  return t
}

/**
 * A glass orb with a crackling white rim and slow drifting dust. Decorative.
 * `speed` scales the rim's crackle; `particles` is the dust count (scale it with the orb's size).
 */
export function CrystalBall({
  size = 0.75,
  speed = 1,
  particles = 6000,
  className,
}: {
  size?: number
  speed?: number
  particles?: number
  className?: string
}) {
  const ref = useRef<HTMLDivElement>(null)
  const { theme } = useTheme()
  const props = useRef({ size, speed, light: theme === "light", wake: () => {} })
  props.current.size = size
  props.current.speed = speed
  props.current.light = theme === "light"
  useEffect(() => props.current.wake(), [theme])

  useEffect(() => {
    const container = ref.current
    if (!container) return
    const canvas = document.createElement("canvas")
    canvas.style.cssText = "display:block;width:100%;height:100%"
    const gl = canvas.getContext("webgl2", {
      alpha: true,
      premultipliedAlpha: true,
      antialias: false,
      depth: false,
    })
    if (!gl) return
    const field = compile(gl, fullscreen, fieldFragment)
    const dust = compile(gl, dustVertex, dustFragment)
    const composite = compile(gl, fullscreen, compositeFragment)
    if (!field || !dust || !composite) return
    container.appendChild(canvas)

    // without float render targets, store light at 1/4 scale in RGBA8 so it doesn't clip
    const float =
      !!gl.getExtension("EXT_color_buffer_float") ||
      !!gl.getExtension("EXT_color_buffer_half_float")
    const encode = float ? 1 : 0.25

    const data = buildDust(particles)
    const home = texture(gl, STATE_WIDTH, data.rows, gl.RGBA32F, gl.RGBA, gl.FLOAT, data.home)
    const seed = texture(gl, STATE_WIDTH, data.rows, gl.RGBA32F, gl.RGBA, gl.FLOAT, data.seed)
    let scene: WebGLTexture | null = null
    const fbo = gl.createFramebuffer()

    const strands = buildStrands()
    gl.useProgram(field.p)
    gl.uniform4fv(field.u.uHarmonics, strands.harmonics)
    gl.uniform4fv(field.u.uRates, strands.rates)
    gl.uniform4fv(field.u.uPhases, strands.phases)
    gl.uniform4fv(field.u.uFlareHarmonics, strands.flareHarmonics)
    gl.uniform4fv(field.u.uFlareRates, strands.flareRates)
    gl.uniform4fv(field.u.uFlarePhases, strands.flarePhases)
    gl.uniform1f(field.u.uEncode, encode)
    gl.useProgram(dust.p)
    gl.uniform1i(dust.u.tHome, 0)
    gl.uniform1i(dust.u.tSeed, 1)
    gl.uniform1f(dust.u.uEncode, encode)
    gl.useProgram(composite.p)
    gl.uniform1i(composite.u.tScene, 2)
    gl.uniform1f(composite.u.uDecode, 1 / encode)
    gl.activeTexture(gl.TEXTURE0)
    gl.bindTexture(gl.TEXTURE_2D, home)
    gl.activeTexture(gl.TEXTURE1)
    gl.bindTexture(gl.TEXTURE_2D, seed)
    gl.blendFunc(gl.ONE, gl.ONE)
    gl.clearColor(0, 0, 0, 0)

    let width = 1
    let height = 1
    let dpr = 1
    const resize = () => {
      width = Math.max(1, container.clientWidth)
      height = Math.max(1, container.clientHeight)
      dpr = Math.min(window.devicePixelRatio || 1, 2)
      canvas.width = Math.round(width * dpr)
      canvas.height = Math.round(height * dpr)
      gl.deleteTexture(scene)
      gl.activeTexture(gl.TEXTURE2)
      scene = float
        ? texture(gl, canvas.width, canvas.height, gl.RGBA16F, gl.RGBA, gl.HALF_FLOAT, null)
        : texture(gl, canvas.width, canvas.height, gl.RGBA8, gl.RGBA, gl.UNSIGNED_BYTE, null)
      gl.bindFramebuffer(gl.FRAMEBUFFER, fbo)
      gl.framebufferTexture2D(gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.TEXTURE_2D, scene, 0)
      gl.viewport(0, 0, canvas.width, canvas.height)
    }

    const reducedMotion = window.matchMedia("(prefers-reduced-motion: reduce)").matches
    const random = seeded(913)
    const arcs: {
      start: number
      span: number
      curl: number
      life: number
      duration: number
      drift: number
    }[] = []
    const arcData = new Float32Array(MAX_ARCS * 4)
    let raf = 0
    let last = performance.now()
    let rimTime = 0
    let dustTime = 0
    let intro = reducedMotion ? 1 : 0
    let nextArc = 0.3
    let visible = true
    let sized = false

    const frame = (now: number) => {
      raf = 0
      if (!sized) {
        resize()
        sized = true
      }
      const s = props.current
      const dt = Math.min(0.05, Math.max(1 / 240, (now - last) / 1000))
      last = now
      intro = Math.min(1, intro + dt / INTRO_SECONDS)
      const presence = easeOut(smooth(0, 0.42, intro))
      const unfold = easeOut(smooth(0, 0.32, intro))
      const charge = Math.sin(Math.PI * smooth(0.22, 0.78, intro)) * 0.35
      const radius = Math.max(8, (clamp(s.size, 0.1, 1.5) * Math.min(width, height)) / 2)

      if (!reducedMotion) {
        const step = dt * Math.max(0, s.speed) * 2
        rimTime += step
        dustTime += dt * DUST_SPEED
        if (intro > 0.55) {
          nextArc -= step * 0.8 * 2 * 1.14
          if (nextArc <= 0 && arcs.length < MAX_ARCS) {
            arcs.push({
              start: random() < 0.7 ? Math.PI / 2 + (random() - 0.5) * 1.5 : random() * Math.PI * 2,
              span: (0.05 + random() * 0.07) * (random() < 0.5 ? -1 : 1),
              curl: 0.02 + random() * 0.04,
              life: 0,
              duration: 0.2 + random() * 0.2,
              drift: (random() - 0.5) * 0.3,
            })
            nextArc = 0.15 + random() * 0.6
          }
        }
        for (let i = arcs.length - 1; i >= 0; i--) {
          arcs[i].life += step
          arcs[i].start += arcs[i].drift * step
          if (arcs[i].life >= arcs[i].duration) arcs.splice(i, 1)
        }
      }
      arcData.fill(0)
      arcs.forEach((a, i) =>
        arcData.set(
          [a.start, a.span, a.curl, Math.sin((Math.PI * a.life) / a.duration) * 0.8],
          i * 4,
        ),
      )

      const pal = s.light ? PALETTES.light : PALETTES.dark
      const u = field.u
      gl.bindFramebuffer(gl.FRAMEBUFFER, fbo)
      gl.clear(gl.COLOR_BUFFER_BIT)
      gl.enable(gl.BLEND)
      gl.useProgram(field.p)
      gl.uniform2f(u.uCenter, width / 2, height / 2)
      gl.uniform1f(u.uRadius, radius)
      gl.uniform1f(u.uDpr, dpr)
      gl.uniform1f(u.uLine, Math.max(0.6, radius * 0.0035))
      gl.uniform1f(u.uTime, rimTime)
      gl.uniform1f(u.uFrame, Math.floor(rimTime * 24) % 100000)
      gl.uniform1f(u.uBins, clamp(Math.round((Math.PI * 2 * radius) / 2.6), 240, 900))
      gl.uniform1f(u.uCrackle, 0.9 + charge)
      gl.uniform1f(u.uGlow, (1 + charge) * (s.light ? 0.35 : 1))
      gl.uniform1f(u.uHaze, 0.9 * (s.light ? 0.6 : 1))
      gl.uniform1f(u.uPresence, presence)
      gl.uniform1f(u.uUnfold, unfold)
      gl.uniform1f(u.uBloom, presence * presence)
      gl.uniform1f(u.uInside, smooth(0.2, 0.95, intro))
      gl.uniform3fv(u.uRim, pal.rim)
      gl.uniform3fv(u.uRimHot, pal.rimHot)
      gl.uniform3fv(u.uRimMid, pal.rimMid)
      gl.uniform3fv(u.uRimDeep, pal.rimDeep)
      gl.uniform3fv(u.uSpark, pal.spark)
      gl.uniform3fv(u.uSparkGlow, pal.sparkGlow)
      gl.uniform3fv(u.uHazeColor, pal.haze)
      gl.uniform3fv(u.uEdgeColor, pal.edge)
      gl.uniform4fv(u.uArcs, arcData)
      gl.drawArrays(gl.TRIANGLES, 0, 3)

      const d = dust.u
      gl.useProgram(dust.p)
      gl.uniform1f(d.uDustTime, dustTime)
      gl.uniform2f(
        d.uTurn,
        0.126 * Math.sin(dustTime * 0.23),
        0.036 * Math.sin(dustTime * 0.17 + 1.3),
      )
      gl.uniform2f(d.uCenter, width / 2, height / 2)
      gl.uniform2f(d.uViewport, width, height)
      gl.uniform1f(d.uRadius, radius)
      gl.uniform1f(d.uDpr, dpr)
      gl.uniform1f(d.uPointScale, Math.max(1, radius / 491))
      gl.uniform1f(d.uReveal, smooth(0.18, 1, intro))
      gl.uniform3fv(d.uTones, pal.tones.flatMap(g))
      gl.uniform3fv(d.uRimTone, pal.rim)
      gl.drawArrays(gl.POINTS, 0, data.count)

      gl.bindFramebuffer(gl.FRAMEBUFFER, null)
      gl.disable(gl.BLEND)
      gl.useProgram(composite.p)
      gl.uniform1f(composite.u.uLight, s.light ? 1 : 0)
      gl.drawArrays(gl.TRIANGLES, 0, 3)

      if (visible && (!reducedMotion || intro < 1)) raf = requestAnimationFrame(frame)
    }

    const start = () => {
      if (raf || !visible) return
      last = performance.now()
      raf = requestAnimationFrame(frame)
    }
    props.current.wake = start

    const ro = new ResizeObserver(() => {
      sized = false
      start()
    })
    ro.observe(container)
    const io = new IntersectionObserver(([e]) => {
      visible = e.isIntersecting
      start()
    })
    io.observe(container)
    const onVisibility = () => !document.hidden && start()
    document.addEventListener("visibilitychange", onVisibility)

    return () => {
      visible = false
      cancelAnimationFrame(raf)
      ro.disconnect()
      io.disconnect()
      document.removeEventListener("visibilitychange", onVisibility)
      props.current.wake = () => {}
      gl.getExtension("WEBGL_lose_context")?.loseContext()
      canvas.remove()
    }
  }, [particles])

  return <div ref={ref} aria-hidden className={cn("size-44 shrink-0", className)} />
}
