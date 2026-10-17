// 浏览器能解码的视频编码（名称与服务端 ffprobe 的 codec_name 一致）。
// Linux 上的 Chrome 默认不能解 HEVC，播放时只有声音、画面全黑，因此按实际能力筛选。
const probes: Record<string, string> = {
  h264: 'video/mp4; codecs="avc1.640028"',
  hevc: 'video/mp4; codecs="hvc1.1.6.L120.90"',
  av1: 'video/mp4; codecs="av01.0.08M.08"',
  vp9: 'video/mp4; codecs="vp09.00.40.08"',
};

let cached: string[] | undefined;
export function playableCodecs(): string[] {
  if (!cached) {
    const v = document.createElement('video');
    cached = Object.keys(probes).filter(name => v.canPlayType(probes[name]) !== '');
  }
  return cached;
}

// canPlayCodec：未知编码（尚未探测）按可播放处理。
export function canPlayCodec(codec?: string) {
  return !codec || codec === '?' || playableCodecs().includes(codec);
}

// 网页端展示的视频编码：浏览器能播的，且一律不要 HEVC（体积大、多数浏览器不能解，交给桌面端）。
export function webCodecs(): string {
  return playableCodecs().filter(c => c !== 'hevc').join(',');
}
