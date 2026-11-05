import type { CSSProperties } from 'react';

export type GradientDirection = 'to right' | 'to left' | 'to bottom' | '135deg' | 'radial';
export const GRADIENT_DIRECTIONS: readonly GradientDirection[] = ['to right', 'to left', 'to bottom', '135deg', 'radial'];

export interface GradientPreset {
  id: string;
  name: string;
  colors: string[];
  direction: GradientDirection;
  tags: string[];
  featured: boolean;
  source: 'curated' | 'custom';
}

export interface HslColor {
  h: number;
  s: number;
  l: number;
}

const clamp = (value: number, min: number, max: number) => Math.min(Math.max(value, min), max);

export const normalizeHex = (hex: string): string => {
  const clean = hex.trim().replace(/^#/, '');
  if (/^[0-9a-fA-F]{3}$/.test(clean)) {
    return `#${clean.split('').map(c => c + c).join('')}`.toUpperCase();
  }
  if (/^[0-9a-fA-F]{6}$/.test(clean)) {
    return `#${clean}`.toUpperCase();
  }
  return '#111827';
};

export const hexToRgbObj = (hex: string): { r: number; g: number; b: number } => {
  const normalized = normalizeHex(hex).replace(/^#/, '');
  return {
    r: Number.parseInt(normalized.slice(0, 2), 16),
    g: Number.parseInt(normalized.slice(2, 4), 16),
    b: Number.parseInt(normalized.slice(4, 6), 16),
  };
};

export const relativeLuminance = (hex: string): number => {
  const { r, g, b } = hexToRgbObj(hex);
  const channels = [r, g, b].map(channel => {
    const value = channel / 255;
    return value <= 0.03928 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4;
  });
  return channels[0] * 0.2126 + channels[1] * 0.7152 + channels[2] * 0.0722;
};

export const hexToHslObj = (hex: string): HslColor => {
  const { r, g, b } = hexToRgbObj(hex);
  const rUnit = r / 255;
  const gUnit = g / 255;
  const bUnit = b / 255;
  const max = Math.max(rUnit, gUnit, bUnit);
  const min = Math.min(rUnit, gUnit, bUnit);
  const delta = max - min;
  const lightness = (max + min) / 2;

  let hue = 0;
  let saturation = 0;

  if (delta !== 0) {
    saturation = delta / (1 - Math.abs(2 * lightness - 1));
    switch (max) {
      case rUnit:
        hue = 60 * (((gUnit - bUnit) / delta) % 6);
        break;
      case gUnit:
        hue = 60 * ((bUnit - rUnit) / delta + 2);
        break;
      default:
        hue = 60 * ((rUnit - gUnit) / delta + 4);
        break;
    }
  }

  return {
    h: Math.round((hue + 360) % 360),
    s: Math.round(saturation * 100),
    l: Math.round(lightness * 100),
  };
};

export const getContrastColor = (hex: string): string =>
  relativeLuminance(hex) > 0.48 ? '#0B1020' : '#FFFFFF';

export const isGradientDark = (colors: string[]): boolean => {
  const lum = colors.reduce((sum, color) => sum + relativeLuminance(color), 0) / Math.max(colors.length, 1);
  return lum < 0.42;
};

export const getAverageHsl = (colors: string[]): HslColor => {
  const hsl = colors.map(hexToHslObj);
  const total = hsl.reduce(
    (acc, color) => ({ h: acc.h + color.h, s: acc.s + color.s, l: acc.l + color.l }),
    { h: 0, s: 0, l: 0 },
  );
  const count = Math.max(hsl.length, 1);
  return {
    h: Math.round(total.h / count),
    s: Math.round(total.s / count),
    l: Math.round(total.l / count),
  };
};

export const toCssGradient = (preset: { colors: string[]; direction: GradientDirection }, direction = preset.direction): string => {
  const colorStops = preset.colors.join(', ');
  if (direction === 'radial') {
    return `radial-gradient(circle at 28% 22%, ${colorStops})`;
  }
  return `linear-gradient(${direction}, ${colorStops})`;
};

export const toFallbackCss = (preset: GradientPreset, direction = preset.direction): string => [
  `background-color: ${preset.colors[0]};`,
  `background-image: ${toCssGradient(preset, direction)};`,
].join('\n');

export const toTailwindConfig = (preset: GradientPreset, direction = preset.direction): string => {
  const token = preset.id.replace(/[^a-z0-9-]/g, '-');
  return [
    'export default {',
    '  theme: {',
    '    extend: {',
    '      backgroundImage: {',
    `        "${token}": "${toCssGradient(preset, direction)}",`,
    '      },',
    '    },',
    '  },',
    '};',
  ].join('\n');
};

export const toCssVariables = (preset: GradientPreset, direction = preset.direction): string => {
  const lines = preset.colors.map((color, index) => `  --gradient-atlas-color-${index + 1}: ${color};`);
  return [
    ':root {',
    ...lines,
    `  --gradient-atlas-direction: ${direction};`,
    `  --gradient-atlas: ${toCssGradient(preset, direction)};`,
    '}',
  ].join('\n');
};

export const toReactStyle = (preset: GradientPreset, direction = preset.direction): string => [
  'const gradientStyle: React.CSSProperties = {',
  `  backgroundImage: "${toCssGradient(preset, direction)}",`,
  `  backgroundColor: "${preset.colors[0]}",`,
  '};',
].join('\n');

export const toJsonSnippet = (preset: GradientPreset, direction = preset.direction): string =>
  JSON.stringify({ ...preset, direction }, null, 2);

const hueTag = (hue: number): string => {
  if (hue < 18 || hue >= 345) return 'red';
  if (hue < 45) return 'orange';
  if (hue < 75) return 'yellow';
  if (hue < 145) return 'green';
  if (hue < 175) return 'mint';
  if (hue < 200) return 'cyan';
  if (hue < 245) return 'blue';
  if (hue < 285) return 'purple';
  if (hue < 330) return 'pink';
  return 'rose';
};

const isWarmHue = (hue: number) => hue < 75 || hue >= 330;
const isCoolHue = (hue: number) => hue >= 145 && hue < 285;

export const generateTags = (colors: string[]): string[] => {
  const hsl = colors.map(hexToHslObj);
  const tags = new Set<string>();
  hsl.forEach(color => tags.add(hueTag(color.h)));

  const avgSaturation = hsl.reduce((sum, color) => sum + color.s, 0) / Math.max(hsl.length, 1);
  const avgLightness = hsl.reduce((sum, color) => sum + color.l, 0) / Math.max(hsl.length, 1);
  const hasWarm = hsl.some(color => isWarmHue(color.h));
  const hasCool = hsl.some(color => isCoolHue(color.h));
  const hasBlueGreen = hsl.some(color => color.h >= 160 && color.h < 245);
  const hasOrangePink = hsl.some(color => color.h < 45 || color.h >= 300);

  if (avgLightness > 74 && avgSaturation < 70) tags.add('pastel');
  if (avgSaturation > 68 && avgLightness > 38) tags.add('neon');
  if (avgLightness < 34) tags.add('dark');
  if (avgLightness > 76) tags.add('light');
  if (hasWarm) tags.add('warm');
  if (hasCool) tags.add('cold');
  if (hasWarm && hasCool) tags.add('warm-cold');
  if (hasBlueGreen) tags.add('ocean');
  if (hasOrangePink && hasWarm) tags.add('sunset');
  if (avgSaturation > 62 && avgLightness < 48 && hasCool) tags.add('cyberpunk');
  if (avgSaturation > 40 && avgLightness > 28 && avgLightness < 76) tags.add('brand');

  return Array.from(tags).slice(0, 8);
};

export const parseHexList = (value: string): string[] =>
  value
    .split(/[,\s]+/)
    .map(item => item.trim())
    .filter(Boolean)
    .map(normalizeHex)
    .filter((item, index, all) => /^#[0-9A-F]{6}$/.test(item) && all.indexOf(item) === index)
    .slice(0, 5);

export const getGradientSurfaceVars = (colors: string[]): CSSProperties => {
  const dark = isGradientDark(colors);
  const average = getAverageHsl(colors);
  const accent = colors[Math.min(1, colors.length - 1)] ?? colors[0] ?? '#7DD3FC';

  return {
    '--atlas-ink': dark ? 'rgba(255,255,255,0.95)' : 'rgba(11,16,32,0.92)',
    '--atlas-muted': dark ? 'rgba(255,255,255,0.64)' : 'rgba(11,16,32,0.62)',
    '--atlas-faint': dark ? 'rgba(255,255,255,0.12)' : 'rgba(11,16,32,0.12)',
    '--atlas-panel': dark ? 'rgba(8,10,18,0.52)' : 'rgba(255,255,255,0.54)',
    '--atlas-panel-strong': dark ? 'rgba(8,10,18,0.74)' : 'rgba(255,255,255,0.78)',
    '--atlas-border': dark ? 'rgba(255,255,255,0.18)' : 'rgba(11,16,32,0.16)',
    '--atlas-shadow': dark ? 'rgba(0,0,0,0.36)' : 'rgba(43,45,66,0.18)',
    '--atlas-accent': accent,
    '--atlas-glow': `hsla(${clamp(average.h + 24, 0, 360)}, ${clamp(average.s + 8, 34, 88)}%, ${clamp(average.l + 10, 38, 78)}%, 0.36)`,
  } as CSSProperties;
};

const RAW_GRADIENTS: Array<[string, string[], string[]?, boolean?]> = [
  ['Blu', ['#00416A', '#E4E5E6'], ['cold', 'ocean'], true],
  ['Ver', ['#FFE000', '#799F0C'], ['warm', 'light'], true],
  ['Ver Black', ['#F7F8F8', '#ACBB78'], ['light', 'pastel'], false],
  ['Combi', ['#00416A', '#799F0C', '#FFE000'], ['warm-cold', 'brand'], true],
  ['Anwar', ['#334D50', '#CBCAA5'], ['cold', 'pastel'], false],
  ['Bluelagoo', ['#0052D4', '#4364F7', '#6FB1FC'], ['cold', 'ocean', 'brand'], true],
  ['Lunada', ['#5433FF', '#20BDFF', '#A5FECB'], ['neon', 'ocean', 'cyberpunk'], true],
  ['Reaqua', ['#799F0C', '#ACBB78'], ['green', 'pastel'], false],
  ['Mango', ['#FFE259', '#FFA751'], ['warm', 'sunset'], true],
  ['Bupe', ['#00416A', '#E4E5E6'], ['cold', 'light'], false],
  ['Rea', ['#FFE000', '#799F0C'], ['warm', 'yellow'], false],
  ['Windy', ['#ACB6E5', '#86FDE8'], ['pastel', 'ocean'], true],
  ['Royal Blue', ['#536976', '#292E49'], ['dark', 'cold'], false],
  ['Royal Blue + Petrol', ['#BBD2C5', '#536976', '#292E49'], ['dark', 'cold'], false],
  ['Copper', ['#B79891', '#94716B'], ['warm', 'pastel'], false],
  ['Anamnisar', ['#9796F0', '#FBC7D4'], ['pastel', 'pink'], true],
  ['Petrol', ['#BBD2C5', '#536976'], ['cold', 'pastel'], false],
  ['Sky', ['#076585', '#FFFFFF'], ['ocean', 'light'], false],
  ['Sel', ['#00467F', '#A5CC82'], ['ocean', 'green'], false],
  ['Afternoon', ['#000C40', '#607D8B'], ['dark', 'cold'], false],
  ['Skyline', ['#1488CC', '#2B32B2'], ['ocean', 'brand'], true],
  ['DIMIGO', ['#EC008C', '#FC6767'], ['neon', 'sunset', 'brand'], true],
  ['Purple Love', ['#CC2B5E', '#753A88'], ['purple', 'sunset'], false],
  ['Sexy Blue', ['#2193B0', '#6DD5ED'], ['ocean', 'cyan'], true],
  ['Blooker20', ['#E65C00', '#F9D423'], ['warm', 'sunset', 'neon'], false],
  ['Sea Blue', ['#2B5876', '#4E4376'], ['dark', 'ocean'], false],
  ['Nimvelo', ['#314755', '#26A0DA'], ['ocean', 'cold'], false],
  ['Hazel', ['#77A1D3', '#79CBCA', '#E684AE'], ['pastel', 'warm-cold'], true],
  ['Noon to Dusk', ['#FF6E7F', '#BFE9FF'], ['sunset', 'pastel', 'warm-cold'], true],
  ['YouTube', ['#E52D27', '#B31217'], ['red', 'brand'], false],
  ['Cool Brown', ['#603813', '#B29F94'], ['warm', 'dark'], false],
  ['Harmonic Energy', ['#16A085', '#F4D03F'], ['warm-cold', 'green'], false],
  ['Playing with Reds', ['#D31027', '#EA384D'], ['red', 'neon'], false],
  ['Sunny Days', ['#EDE574', '#E1F5C4'], ['light', 'pastel'], false],
  ['Green Beach', ['#02AAB0', '#00CDAC'], ['ocean', 'mint', 'neon'], true],
  ['Intuitive Purple', ['#DA22FF', '#9733EE'], ['neon', 'purple', 'cyberpunk'], true],
  ['Emerald Water', ['#348F50', '#56B4D3'], ['ocean', 'green'], false],
  ['Lemon Twist', ['#3CA55C', '#B5AC49'], ['green', 'yellow'], false],
  ['Monte Carlo', ['#CC95C0', '#DBD4B4', '#7AA1D2'], ['pastel', 'warm-cold'], true],
  ['Horizon', ['#003973', '#E5E5BE'], ['ocean', 'warm-cold'], false],
  ['Rose Water', ['#E55D87', '#5FC3E4'], ['warm-cold', 'pastel'], true],
  ['Frozen', ['#403B4A', '#E7E9BB'], ['warm-cold', 'pastel'], false],
  ['Mango Pulp', ['#F09819', '#EDDE5D'], ['warm', 'sunset'], false],
  ['Bloody Mary', ['#FF512F', '#DD2476'], ['sunset', 'neon', 'brand'], true],
  ['Aubergine', ['#AA076B', '#61045F'], ['dark', 'purple'], false],
  ['Aqua Marine', ['#1A2980', '#26D0CE'], ['ocean', 'cyberpunk', 'brand'], true],
  ['Sunrise', ['#FF512F', '#F09819'], ['warm', 'sunset', 'neon'], true],
  ['Purple Paradise', ['#1D2B64', '#F8CDDA'], ['purple', 'pastel'], false],
  ['Stripe', ['#1FA2FF', '#12D8FA', '#A6FFCB'], ['ocean', 'neon', 'brand'], true],
  ['Sea Weed', ['#4CB8C4', '#3CD3AD'], ['mint', 'ocean'], false],
  ['Pinky', ['#DD5E89', '#F7BB97'], ['pink', 'sunset', 'pastel'], false],
  ['Cherry', ['#EB3349', '#F45C43'], ['red', 'sunset', 'neon'], false],
  ['Mojito', ['#1D976C', '#93F9B9'], ['green', 'mint'], true],
  ['Juicy Orange', ['#FF8008', '#FFC837'], ['orange', 'sunset', 'neon'], false],
  ['Mirage', ['#16222A', '#3A6073'], ['dark', 'cold'], true],
  ['Steel Gray', ['#1F1C2C', '#928DAB'], ['dark', 'purple'], false],
  ['Kashmir', ['#614385', '#516395'], ['purple', 'cold'], false],
  ['Electric Violet', ['#4776E6', '#8E54E9'], ['neon', 'purple', 'brand'], true],
  ['Venice Blue', ['#085078', '#85D8CE'], ['ocean', 'mint'], false],
  ['Bora Bora', ['#2BC0E4', '#EAECC6'], ['ocean', 'light'], false],
  ['Moss', ['#134E5E', '#71B280'], ['green', 'cold'], false],
  ['Shroom Haze', ['#5C258D', '#4389A2'], ['cyberpunk', 'purple'], true],
  ['Mystic', ['#757F9A', '#D7DDE8'], ['light', 'pastel'], false],
  ['Midnight City', ['#232526', '#414345'], ['dark'], false],
  ['Sea Blizz', ['#1CD8D2', '#93EDC7'], ['mint', 'ocean', 'light'], false],
  ['Opa', ['#3D7EAA', '#FFE47A'], ['warm-cold', 'sunset'], false],
  ['Titanium', ['#283048', '#859398'], ['cold', 'dark'], false],
  ['Mantle', ['#24C6DC', '#514A9D'], ['cyberpunk', 'ocean'], true],
  ['Dracula', ['#DC2424', '#4A569D'], ['warm-cold', 'cyberpunk'], false],
  ['Peach', ['#ED4264', '#FFEDBC'], ['sunset', 'pastel'], true],
  ['Moonrise', ['#DAE2F8', '#D6A4A4'], ['pastel', 'light'], false],
  ['Clouds', ['#ECE9E6', '#FFFFFF'], ['light', 'pastel'], false],
  ['Stellar', ['#7474BF', '#348AC7'], ['cold', 'brand'], false],
  ['Bourbon', ['#EC6F66', '#F3A183'], ['warm', 'sunset'], false],
  ['Calm Darya', ['#5F2C82', '#49A09D'], ['ocean', 'purple'], true],
  ['Influenza', ['#C04848', '#480048'], ['dark', 'red'], false],
  ['Shrimpy', ['#E43A15', '#E65245'], ['red', 'warm'], false],
  ['Army', ['#414D0B', '#727A17'], ['green', 'dark'], false],
  ['Miaka', ['#FC354C', '#0ABFBC'], ['warm-cold', 'neon'], true],
  ['Pinot Noir', ['#182848', '#4B6CB7'], ['dark', 'blue'], false],
  ['Day Tripper', ['#F857A6', '#FF5858'], ['pink', 'neon', 'sunset'], true],
  ['Namn', ['#A73737', '#7A2828'], ['red', 'dark'], false],
  ['Blurry Beach', ['#D53369', '#CWRD51'.replace('WR', 'BA')], ['warm', 'sunset'], false],
  ['Vasily', ['#E9D362', '#333333'], ['dark', 'yellow'], false],
  ['A Lost Memory', ['#DE6262', '#FFB88C'], ['sunset', 'warm'], false],
  ['Petrichor', ['#666600', '#999966'], ['green', 'dark'], false],
  ['Jonquil', ['#FFEEEE', '#DDEFBB'], ['light', 'pastel'], false],
  ['Sirius Tamed', ['#EFEFBB', '#D4D3DD'], ['light', 'pastel'], false],
  ['Kyoo Pal', ['#DD3E54', '#6BE585'], ['warm-cold', 'neon'], false],
  ['Kye Meh', ['#8360C3', '#2EBF91'], ['cyberpunk', 'mint', 'brand'], true],
  ['Kyoo Tah', ['#544A7D', '#FFD452'], ['warm-cold', 'sunset'], false],
  ['By Design', ['#009FFF', '#EC2F4B'], ['neon', 'cyberpunk', 'warm-cold'], true],
  ['Ultra Voilet', ['#654EA3', '#EAAFC8'], ['purple', 'pastel'], true],
  ['Burning Orange', ['#FF416C', '#FF4B2B'], ['neon', 'sunset', 'brand'], true],
  ['Wiretap', ['#8A2387', '#E94057', '#F27121'], ['sunset', 'neon', 'brand'], true],
  ['Summer Dog', ['#A8FF78', '#78FFD6'], ['mint', 'neon', 'light'], true],
  ['Rastafari', ['#1E9600', '#FFF200', '#FF0000'], ['neon', 'warm'], false],
  ['Sin City Red', ['#ED213A', '#93291E'], ['red', 'dark'], false],
  ['Citrus Peel', ['#FDC830', '#F37335'], ['orange', 'sunset', 'warm'], true],
  ['Blue Raspberry', ['#00B4DB', '#0083B0'], ['ocean', 'blue', 'brand'], true],
  ['Margo', ['#FFEFBA', '#FFFFFF'], ['light', 'pastel'], false],
  ['Magic', ['#59C173', '#A17FE0', '#5D26C1'], ['cyberpunk', 'purple', 'neon'], true],
  ['Evening Night', ['#005AA7', '#FFFDE4'], ['ocean', 'warm-cold'], false],
  ['Vanusa', ['#DA4453', '#89216B'], ['pink', 'purple', 'dark'], false],
  ['Shifty', ['#636363', '#A2AB58'], ['green', 'dark'], false],
  ['eXpresso', ['#AD5389', '#3C1053'], ['purple', 'dark'], false],
  ['Slight Ocean View', ['#A8C0FF', '#3F2B96'], ['blue', 'purple', 'brand'], true],
  ['Pure Lust', ['#333333', '#DD1818'], ['dark', 'red'], false],
  ['Moon Purple', ['#4E54C8', '#8F94FB'], ['purple', 'brand', 'neon'], true],
  ['Red Sunset', ['#355C7D', '#6C5B7B', '#C06C84'], ['sunset', 'warm-cold'], true],
  ['Shifter', ['#BC4E9C', '#F80759'], ['pink', 'neon', 'cyberpunk'], true],
  ['Wedding Day Blues', ['#40E0D0', '#FF8C00', '#FF0080'], ['neon', 'warm-cold', 'sunset'], true],
  ['Sand to Blue', ['#3E5151', '#DECBA4'], ['warm-cold', 'pastel'], false],
  ['Quepal', ['#11998E', '#38EF7D'], ['mint', 'green', 'neon', 'brand'], true],
  ['Pun Yeta', ['#108DC7', '#EF8E38'], ['warm-cold', 'sunset'], false],
  ['Sublime Light', ['#FC5C7D', '#6A82FB'], ['neon', 'cyberpunk', 'brand'], true],
  ['Sublime Vivid', ['#FC466B', '#3F5EFB'], ['neon', 'cyberpunk', 'brand'], true],
  ['Bighead', ['#C94B4B', '#4B134F'], ['dark', 'purple'], false],
  ['Taran Tado', ['#23074D', '#CC5333'], ['dark', 'sunset'], false],
  ['Relaxing Red', ['#FFFBD5', '#B20A2C'], ['red', 'warm'], false],
];

export const CURATED_GRADIENTS: GradientPreset[] = RAW_GRADIENTS.map(([name, colors, extraTags = [], featured = false], index) => {
  const normalizedColors = colors.map(normalizeHex);
  const generated = generateTags(normalizedColors);
  const mergedTags = Array.from(new Set([...extraTags, ...generated]));
  const slug = name.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/(^-|-$)/g, '');
  return {
    id: `${slug}-${index + 1}`,
    name,
    colors: normalizedColors,
    direction: index % 5 === 0 ? '135deg' : 'to right',
    tags: mergedTags,
    featured,
    source: 'curated',
  };
});

export const CURATED_GRADIENT_TAGS = [
  'all',
  'featured',
  'favorites',
  'neon',
  'cyberpunk',
  'ocean',
  'sunset',
  'pastel',
  'dark',
  'warm',
  'cold',
  'brand',
  'mint',
  'purple',
] as const;

export function exportGradientWallpaper(preset: GradientPreset, direction: GradientDirection): Promise<void> {
  return new Promise((resolve, reject) => {
    const canvas = document.createElement('canvas');
    const width = 1600;
    const height = 900;
    canvas.width = width;
    canvas.height = height;
    const ctx = canvas.getContext('2d');
    if (!ctx) {
      reject(new Error('Canvas context unavailable'));
      return;
    }

    let grad: CanvasGradient;
    if (direction === 'radial') {
      grad = ctx.createRadialGradient(width * 0.28, height * 0.22, 40, width * 0.5, height * 0.5, width * 0.85);
    } else if (direction === 'to left') {
      grad = ctx.createLinearGradient(width, 0, 0, 0);
    } else if (direction === 'to bottom') {
      grad = ctx.createLinearGradient(0, 0, 0, height);
    } else if (direction === '135deg') {
      grad = ctx.createLinearGradient(0, 0, width, height);
    } else {
      grad = ctx.createLinearGradient(0, 0, width, 0);
    }

    preset.colors.forEach((hex, idx) => {
      const stop = preset.colors.length === 1 ? 0 : idx / (preset.colors.length - 1);
      grad.addColorStop(stop, hex);
    });

    ctx.fillStyle = grad;
    ctx.fillRect(0, 0, width, height);

    // Subtle optical glow
    const glow = ctx.createRadialGradient(width * 0.2, height * 0.18, 10, width * 0.2, height * 0.18, width * 0.45);
    glow.addColorStop(0, 'rgba(255,255,255,0.22)');
    glow.addColorStop(1, 'rgba(255,255,255,0)');
    ctx.fillStyle = glow;
    ctx.fillRect(0, 0, width, height);

    // Film grain overlay
    const imgData = ctx.getImageData(0, 0, width, height);
    const data = imgData.data;
    for (let i = 0; i < data.length; i += 4) {
      const noise = (Math.random() - 0.5) * 14;
      data[i] = clamp(data[i] + noise, 0, 255);
      data[i + 1] = clamp(data[i + 1] + noise, 0, 255);
      data[i + 2] = clamp(data[i + 2] + noise, 0, 255);
    }
    ctx.putImageData(imgData, 0, 0);

    // Editorial watermark badge
    const dark = isGradientDark(preset.colors);
    ctx.fillStyle = dark ? 'rgba(255,255,255,0.86)' : 'rgba(11,16,32,0.84)';
    ctx.font = '800 28px Inter, system-ui, sans-serif';
    ctx.fillText(preset.name, 64, height - 76);
    ctx.fillStyle = dark ? 'rgba(255,255,255,0.58)' : 'rgba(11,16,32,0.56)';
    ctx.font = '700 15px monospace';
    ctx.fillText(`${preset.colors.join('  →  ')}   ·   OMNIHUB GRADIENT ATLAS`, 64, height - 44);

    canvas.toBlob(blob => {
      if (!blob) {
        reject(new Error('Blob export failed'));
        return;
      }
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `${preset.id}-1600x900.png`;
      a.click();
      setTimeout(() => URL.revokeObjectURL(url), 1000);
      resolve();
    }, 'image/png');
  });
}
