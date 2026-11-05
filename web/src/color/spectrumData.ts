export type SpectrumFamily =
  | 'crimson'
  | 'vermilion'
  | 'amber'
  | 'jade'
  | 'celadon'
  | 'azure'
  | 'indigo'
  | 'violet'
  | 'rouge'
  | 'ink';

export type RgbTuple = [number, number, number];
export type CmykTuple = [number, number, number, number];
export type HslTuple = [number, number, number];

export interface SpectrumColor {
  id: string;
  slug: string;
  name: string;
  pinyin: string;
  poem: string;
  hex: string;
  rgb: RgbTuple;
  cmyk: CmykTuple;
  hsl: HslTuple;
  family: SpectrumFamily;
  familyLabel: string;
  ring: number;
  sector: number;
}

export interface SpectrumDiscPlacement {
  color: SpectrumColor;
  ringIndex: number;
  angle: number;
  radius: number;
  baseX: number;
  baseY: number;
  baseZ: number;
  discScale: number;
}

export const SPECTRUM_FAMILIES: { id: SpectrumFamily | 'all'; label: string; cn: string }[] = [
  { id: 'all', label: 'ALL', cn: '全谱百色' },
  { id: 'amber', label: 'AMBER', cn: '明黄·秋香' },
  { id: 'vermilion', label: 'VERMILION', cn: '丹橘·琥珀' },
  { id: 'crimson', label: 'CRIMSON', cn: '朱砂·胭脂' },
  { id: 'rouge', label: 'ROUGE', cn: '海棠·桃夭' },
  { id: 'violet', label: 'VIOLET', cn: '暮山·丁香' },
  { id: 'indigo', label: 'INDIGO', cn: '群青·靛蓝' },
  { id: 'azure', label: 'AZURE', cn: '天水·窃蓝' },
  { id: 'celadon', label: 'CELADON', cn: '天青·秘色' },
  { id: 'jade', label: 'JADE', cn: '竹青·苍葭' },
  { id: 'ink', label: 'INK', cn: '月白·玄墨' },
];

const clamp = (value: number, min = 0, max = 255) => Math.min(max, Math.max(min, value));

export const hexToRgbTuple = (hex: string): RgbTuple => {
  const clean = hex.trim().replace(/^#/, '');
  const val = Number.parseInt(clean.padEnd(6, '0').slice(0, 6), 16);
  return [(val >> 16) & 255, (val >> 8) & 255, val & 255];
};

export const rgbToHexStr = ([r, g, b]: RgbTuple): string =>
  `#${[r, g, b].map(v => Math.round(clamp(v)).toString(16).padStart(2, '0')).join('').toUpperCase()}`;

export const rgbToCmykTuple = ([red, green, blue]: RgbTuple): CmykTuple => {
  const r = red / 255;
  const g = green / 255;
  const b = blue / 255;
  const k = 1 - Math.max(r, g, b);
  if (k >= 0.999) return [0, 0, 0, 100];
  const c = (1 - r - k) / (1 - k);
  const m = (1 - g - k) / (1 - k);
  const y = (1 - b - k) / (1 - k);
  return [c, m, y, k].map(v => Math.round(clamp(v * 100, 0, 100))) as CmykTuple;
};

export const rgbToHslTuple = ([red, green, blue]: RgbTuple): HslTuple => {
  const r = red / 255;
  const g = green / 255;
  const b = blue / 255;
  const max = Math.max(r, g, b);
  const min = Math.min(r, g, b);
  const delta = max - min;
  const lightness = (max + min) / 2;
  if (delta === 0) return [0, 0, Math.round(lightness * 100)];
  const saturation = delta / (1 - Math.abs(2 * lightness - 1));
  let hue = 0;
  if (max === r) hue = 60 * (((g - b) / delta) % 6);
  else if (max === g) hue = 60 * ((b - r) / delta + 2);
  else hue = 60 * ((r - g) / delta + 4);
  return [Math.round((hue + 360) % 360), Math.round(saturation * 100), Math.round(lightness * 100)];
};

export const getSpectrumLuminance = (hex: string): number => {
  const [r, g, b] = hexToRgbTuple(hex).map(channel => {
    const v = channel / 255;
    return v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4;
  });
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
};

export const getReadableInk = (hex: string): string =>
  getSpectrumLuminance(hex) > 0.46 ? '#14110D' : '#FBF7EF';

const mixRgb = (hex: string, target: RgbTuple, amount: number): string => {
  const src = hexToRgbTuple(hex);
  const ratio = clamp(amount, 0, 1);
  return rgbToHexStr(src.map((v, idx) => v + (target[idx] - v) * ratio) as RgbTuple);
};

export const lightenHex = (hex: string, amount: number): string => mixRgb(hex, [255, 255, 255], amount);
export const darkenHex = (hex: string, amount: number): string => mixRgb(hex, [0, 0, 0], amount);

// 10 families × 12 authentic classical Chinese colors = 120 authentic colors
const RAW_CLASSICAL_COLORS: Record<
  SpectrumFamily,
  { label: string; items: Array<[string, string, string, string]> }
> = {
  amber: {
    label: '明黄·秋香',
    items: [
      ['葵扇黄', 'KUI SHAN HUANG', '#F8D86A', '轻摇葵扇纳微凉 · 宫廷初夏扇面浅黄'],
      ['鹅黄', 'E HUANG', '#FFF143', '鹅儿黄似酒，对酒爱新鹅 · 初生雏羽嫩黄'],
      ['松花', 'SONG HUA', '#BCE672', '轻如松花落金粉 · 春日马尾松花粉黄绿'],
      ['缃色', 'XIANG SE', '#F0C239', '缃叶剪成黄，浅帛染初桑 · 浅黄生绢本色'],
      ['秋香', 'QIU XIANG', '#D9B611', '桂子月中落，秋香满画廊 · 晚秋霁色暗金黄'],
      ['藤黄', 'TENG HUANG', '#FFD111', '丹青点染海藤脂 · 传统国画矿物植物重彩黄'],
      ['栀子', 'ZHI ZI', '#EBEB68', '栀子同心好，染衣留清芳 · 栀子果浸染明澈黄'],
      ['赤金', 'CHI JIN', '#F2BE45', '金樽清酒斗十千 · 泥金笺上纯正器物金'],
      ['雄黄', 'XIONG HUANG', '#E9BB1D', '端阳调雄黄，辟邪入画堂 · 天然矿石暖金黄'],
      ['鬱金', 'YU JIN', '#EFA90D', '郁金香染合欢巾 · 盛唐酒肆与舞衣浓金黄'],
      ['姜黄', 'JIANG HUANG', '#E2C027', '新采秋姜晒晚晴 · 草木染温润土调黄'],
      ['枯黄', 'KU HUANG', '#D3A237', '寒叶落秋阶，余晖映古槐 · 苍劲宋画秋叶黄'],
    ],
  },
  vermilion: {
    label: '丹橘·琥珀',
    items: [
      ['杏黄', 'XING HUANG', '#FFA631', '杏子梢头春意闹 · 成熟银杏与熟杏暖黄橘'],
      ['橘红', 'JU HONG', '#F97D1C', '一年好景君须记，最是橙黄橘绿时 · 洞庭秋橘红'],
      ['琥珀', 'HU PO', '#CA6924', '兰陵美酒郁金香，玉碗盛来琥珀光 · 松脂化石深暖橙'],
      ['流金', 'LIU JIN', '#E08A1E', '大火流金砾石光 · 鎏金铜器温润暖金'],
      ['柿红', 'SHI HONG', '#F26522', '柿叶翻红霜景秋 · 霜降枝头软柿饱满红橙'],
      ['雄精', 'XIONG JING', '#E85827', '丹炉淬火炼雄精 · 鸡冠石结晶浓烈赤橙'],
      ['金驼', 'JIN TUO', '#E46828', '大漠孤烟落日圆 · 丝路驼绒暖调赭橙'],
      ['赭石', 'ZHE SHI', '#845A33', '浅绛山水敷赭石 · 传统山水画山石肌理棕褐'],
      ['檀色', 'TAN SE', '#B36D61', '轻拢慢捻抹复挑，檀板声声 · 浅绛紫檀暖木色'],
      ['驼色', 'TUO SE', '#A88462', '古道西风瘦马 · 敦煌壁画土黄暖褐'],
      ['茶褐', 'CHA HE', '#795439', '松风吹解带，山月照弹琴 · 煎茶浓汤古雅褐'],
      ['伽罗', 'JIA LUO', '#5D3F2E', '沉水袅青烟，伽罗暗香浮 · 顶级沉香深邃木褐'],
    ],
  },
  crimson: {
    label: '朱砂·胭脂',
    items: [
      ['朱砂', 'ZHU SHA', '#FF461F', '朱砂点额映明眸 · 辰州丹砂正宗矿物赤红'],
      ['银朱', 'YIN ZHU', '#DF3428', '轻研银朱批奏折 · 宫廷御批明艳赤朱'],
      ['丹雘', 'DAN HUO', '#D93A2B', '青绿山水映丹雘 · 上古岩彩赤石脂红'],
      ['妃色', 'FEI SE', '#ED5736', '杨妃微醺倚沉香 · 盛唐宫装浅绯暖红'],
      ['猩红', 'XING HONG', '#C91F37', '猩红袍袖拂丹墀 · 礼服正统高饱和浓红'],
      ['胭脂', 'YAN ZHI', '#9D2933', '泪湿罗巾梦不成，夜深前殿按歌声 · 红蓝花凝脂深红'],
      ['茜色', 'QIAN SE', '#CB3A56', '茜草染春衣，绛色映朝晖 · 千年植物染正茜红'],
      ['朱殷', 'ZHU YIN', '#8A2829', '雷声忽送千峰雨，花气浑如百和香 · 凝重暗赤红'],
      ['榴花红', 'LIU HUA HONG', '#F34718', '五月榴花照眼明 · 仲夏石榴盛放炽烈红'],
      ['鹤顶红', 'HE DING HONG', '#D42517', '丹顶乘云游碧落 · 仙鹤冠顶纯正艳红'],
      ['绛色', 'JIANG SE', '#7C1823', '绛帻鸡人报晓筹 · 秦汉宫廷深沉大赤'],
      ['绾色', 'WAN SE', '#A98175', '香丝绾作同心结 · 浅绛微灰的温柔绢帛色'],
    ],
  },
  rouge: {
    label: '海棠·桃夭',
    items: [
      ['桃夭', 'TAO YAO', '#F47983', '桃之夭夭，灼灼其华 · 《诗经》春日初绽桃花粉'],
      ['海棠红', 'HAI TANG HONG', '#DB5A6B', '只恐夜深花睡去，故烧高烛照红妆 · 雨后海棠娇红'],
      ['酡颜', 'TUO YAN', '#F9906F', '春风吹面薄于纱，酒入酡颜映晚霞 · 饮酒微醺面颊粉橘'],
      ['樱草粉', 'YING CAO FEN', '#F8ABA6', '春水初生乳燕飞 · 江南早春报春花柔粉'],
      ['玉红', 'YU HONG', '#C04851', '玉红深处见芳菲 · 温润如赤玉的古典粉红'],
      ['芙蓉', 'FU RONG', '#F28E88', '千林扫作一番黄，只有芙蓉独自芳 · 木芙蓉清透粉'],
      ['藕荷', 'OU HE', '#E4C6D0', '荷风送香气，竹露滴清响 · 莲藕断面淡紫粉'],
      ['霞光', 'XIA GUANG', '#EC6D71', '落霞与孤鹜齐飞 · 暮云层染的绯粉霞色'],
      ['退红', 'TUI HONG', '#F7CFE1', '揉蓝衫子杏黄裙，退红袖口微香 · 唐代名色浅淡粉'],
      ['苏芳', 'SU FANG', '#9E3E4F', '苏芳染就轻罗扇 · 苏木煎汁染出的古典紫红'],
      ['玫瑰紫', 'MEI GUI ZI', '#BA2F7B', '水晶帘动微风起，满架蔷薇一院香 · 重瓣玫瑰明艳紫红'],
      ['胭脂水', 'YAN ZHI SHUI', '#E77C8E', '薄施胭脂水，清透若朝露 · 宋代汝窑与仕女淡妆粉'],
    ],
  },
  violet: {
    label: '暮山·丁香',
    items: [
      ['暮山紫', 'MU SHAN ZI', '#A4ABD6', '潦水尽而寒潭清，烟光凝而暮山紫 · 王勃《滕王阁序》薄暮远山紫'],
      ['丁香', 'DING XIANG', '#CCA4E3', '青鸟不传云外信，丁香空结雨中愁 · 雨巷紫丁香柔紫'],
      ['雪青', 'XUE QING', '#B0A4E3', '雪霁远峰生浅紫 · 冷调清透如雪光映青紫'],
      ['紫薇', 'ZI WEI', '#B878B8', '独坐黄昏谁是伴，紫薇花对紫微郎 · 盛夏百日红雅紫'],
      ['茈藐', 'ZI MIAO', '#9B6E9C', '茈草染衣裳，古意入微茫 · 《尔雅》记载紫草根古紫'],
      ['青莲', 'QING LIAN', '#801DAE', '青莲居士踏歌行 · 佛国青色莲花高饱和明紫'],
      ['绛紫', 'JIANG ZI', '#8C4356', '紫气东来映华堂 · 红紫交融的沉稳宫廷紫'],
      ['乌梅紫', 'WU MEI ZI', '#622A4D', '微雨过，小荷翻，榴花开欲燃 · 烟熏乌梅深邃暗紫'],
      ['葛巾紫', 'GE JIN ZI', '#7E2065', '葛巾漉酒柳阴下 · 牡丹名品紫葛巾浓紫'],
      ['檀紫', 'TAN ZI', '#5C2223', '小山重叠金明灭 · 百年老紫檀木心幽暗红紫'],
      ['紫檀', 'ZI TAN', '#4C221B', '雕栏玉砌应犹在 · 宫廷重器紫檀深邃极暗褐紫'],
      ['暗香紫', 'AN XIANG ZI', '#5B4965', '疏影横斜水清浅，暗香浮动月黄昏 · 寒夜梅影冷灰紫'],
    ],
  },
  indigo: {
    label: '群青·靛蓝',
    items: [
      ['群青', 'QUN QING', '#4C8DAE', '青，取之于蓝而青于蓝 · 青金石研磨重彩矿物正蓝'],
      ['靛青', 'DIAN QING', '#177CB0', '日出江花红胜火，春来江水绿如蓝 · 蓼蓝发酵印染纯正靛色'],
      ['花青', 'HUA QING', '#23577A', '泼墨敷花青，烟雨入江南 · 国画写意核心植物深蓝'],
      ['藏蓝', 'ZANG LAN', '#2E4E7E', '雪域苍穹夜色深 · 庄重深邃的传统深蓝'],
      ['宝蓝', 'BAO LAN', '#4B5CC4', '点翠镶宝蓝，流光溢彩间 · 明清点翠与蓝宝石华贵明蓝'],
      ['黛蓝', 'DAI LAN', '#425066', '远山如黛水如烟 · 画眉青黛与夜色交融的灰蓝'],
      ['青冥', 'QING MING', '#32527B', '青冥浩荡不见底，日月照耀金银台 · 李白诗中九天苍穹深蓝'],
      ['绀青', 'GAN QING', '#1B315E', '绀宇澄明夜色凝 · 佛经泥金笺底深紫玄蓝'],
      ['海青', 'HAI QING', '#224B6E', '海上生明月，天涯共此时 · 沧海深流静谧蓝'],
      ['螺青', 'LUO QING', '#395260', '遥望洞庭山水翠，白银盘里一青螺 · 螺壳暗光墨蓝'],
      ['钢青', 'GANG QING', '#142334', '百炼成钢泛青光 · 刀剑寒芒极深冷蓝'],
      ['暗夜蓝', 'AN YE LAN', '#121E2D', '星垂平野阔，月涌大江流 · 旷野子夜深蓝'],
    ],
  },
  azure: {
    label: '天水·窃蓝',
    items: [
      ['天水碧', 'TIAN SHUI BI', '#5AA4AE', '天水碧，染就一江秋色 · 南唐李煜宫廷传世名色'],
      ['窃蓝', 'QIE LAN', '#88ABDA', '秋鸟有窃蓝，羽衣映晴空 · 《尔雅》九雇鸟浅雅蓝'],
      ['晴山蓝', 'QING SHAN LAN', '#6E9BC5', '水光潋滟晴方好 · 雨过天晴远山澄澈蓝'],
      ['蔚蓝', 'WEI LAN', '#70F3FF', '天似穹庐，笼盖四野 · 晴空万里高明度澄蓝'],
      ['碧落', 'BI LUO', '#5CB3CC', '上穷碧落下黄泉 · 东方诗意里的第一重天空蓝'],
      ['沧浪', 'CANG LANG', '#5192A3', '沧浪之水清兮，可以濯我缨 · 楚辞清澈江水蓝青'],
      ['秋波蓝', 'QIU BO LAN', '#8ABCD1', '水是眼波横，山是眉峰聚 · 清澈灵动的浅水蓝'],
      ['湖蓝', 'HU LAN', '#30DFF3', '欲把西湖比西子 · 晴日高原湖泊明快亮蓝'],
      ['涧石蓝', 'JIAN SHI LAN', '#4E7CA1', '明月松间照，清泉石上流 · 幽涧水色冷调蓝'],
      ['星蓝', 'XING LAN', '#93B5CF', '天阶夜色凉如水，卧看牵牛织女星 · 银河微光淡灰蓝'],
      ['霁蓝', 'JI LAN', '#1F4E7A', '霁蓝釉色凝如玉 · 景德镇高温官窑霁蓝釉'],
      ['扁青', 'BIAN QING', '#508A88', '丹青不渝，扁青敷色 · 《本草纲目》上等矿物石青色'],
    ],
  },
  celadon: {
    label: '天青·秘色',
    items: [
      ['天青', 'TIAN QING', '#83CBAC', '雨过天青云破处，这般颜色做将来 · 宋徽宗汝窑巅峰釉色'],
      ['秘色', 'MI SE', '#7E9E85', '九秋风露越窑开，夺得千峰翠色来 · 法门寺唐代越窑秘色瓷'],
      ['梅子青', 'MEI ZI QING', '#69A794', '青梅如豆柳如眉，日长蝴蝶飞 · 南宋龙泉窑莹润青釉'],
      ['粉青', 'FEN QING', '#88BFB8', '温润如春水，清雅若凝脂 · 龙泉窑粉青厚釉'],
      ['缥色', 'PIAO SE', '#7FECAD', '缥缈仙衣带微风 · 淡青带绿的轻盈丝帛色'],
      ['石青', 'SHI QING', '#1685A9', '千里江山图上石青色 · 王希孟青绿山水蓝铜矿色'],
      ['碧色', 'BI SE', '#1BD1A5', '碧玉妆成一树高，万条垂下绿丝绦 · 清透通明的新碧玉色'],
      ['翡翠', 'FEI CUI', '#3DE1AD', '翡翠巢南海，雄雌珠树林 · 翠羽与硬玉明艳青绿'],
      ['铜绿', 'TONG LV', '#549688', '青铜古器生碧锈 · 千年礼器表面温润铜绿'],
      ['影青', 'YING QING', '#C6E6E8', '青如天，明如镜，薄如纸，声如磬 · 宋代景德镇青白瓷'],
      ['鸭卵青', 'YA LUAN QING', '#D0E6E0', '极淡天青若鸭卵 · 清代文人极推崇的微青极浅色'],
      ['苍筤', 'CANG LANG', '#5B8E7D', '苍筤竹色映窗纱 · 《易经》初生青竹清雅青绿'],
    ],
  },
  jade: {
    label: '竹青·苍葭',
    items: [
      ['竹青', 'ZHU QING', '#789262', '宁可食无肉，不可居无竹 · 江南毛竹节间沉静青绿'],
      ['苍葭', 'CANG JIA', '#A8BF8F', '蒹葭苍苍，白露为霜 · 秋水芦苇微灰清绿'],
      ['豆绿', 'DOU LV', '#9ED048', '春盘春酒年年好，试拈春盘嫩豆绿 · 初春鲜蚕豆清亮绿'],
      ['石绿', 'SHI LV', '#16A951', '金碧山水敷石绿 · 孔雀石研磨的国画正统青绿'],
      ['柳绿', 'LIU LV', '#AFDD22', '草长莺飞二月天，拂堤杨柳醉春烟 · 新柳嫩芽鲜绿'],
      ['葱倩', 'CONG QIAN', '#6EB86E', '青葱欲滴映晴光 · 鲜润生机的草木翠绿'],
      ['官绿', 'GUAN LV', '#2A6E3F', '绿蚁新醅酒，红泥小火炉 · 沉稳端庄的深正绿'],
      ['艾绿', 'AI LV', '#A4CAB6', '门挂艾草迎端阳 · 艾叶背面带有白绒的清淡灰绿'],
      ['苔绿', 'TAI LV', '#638C45', '苔痕上阶绿，草色入帘青 · 幽静庭院青苔温润绿'],
      ['荷叶绿', 'HE YE LV', '#43794B', '接天莲叶无穷碧 · 盛夏西湖荷盖浓郁深绿'],
      ['苍翠', 'CANG CUI', '#519A73', '树木丛生，百草丰茂 · 远山古木深沉翠绿'],
      ['松柏绿', 'SONG BAI LV', '#2D513C', '岁寒，然后知松柏之后凋也 · 凛冬古松苍劲墨绿'],
    ],
  },
  ink: {
    label: '月白·玄墨',
    items: [
      ['月白', 'YUE BAI', '#D6ECF0', '月光如水水如天 · 月色映照绢帛的极淡微蓝白'],
      ['鱼肚白', 'YU DU BAI', '#FCEFE8', '客从东方来，衣上鱼肚白 · 破晓前东方天际微暖白'],
      ['茶白', 'CHA BAI', '#F3F9F1', '晴窗分乳茶，色比凝霜白 · 宋代点茶汤花清透白'],
      ['皦玉', 'JIAO YU', '#FAF4EB', '皦如白玉凝清辉 · 羊脂白玉温润象牙白'],
      ['素色', 'SU SE', '#E0E5DF', '素衣莫起风尘叹 · 未经漂染的天然蚕丝本白'],
      ['银鼠', 'YIN SHU', '#B5B8B1', '银鼠轻裘踏雪行 · 清冷细腻的银灰'],
      ['苍色', 'CANG SE', '#75878A', '天苍苍，野茫茫 · 含青灰调的辽阔自然灰'],
      ['墨色', 'MO SE', '#50616D', '墨分五色：焦、浓、重、淡、清 · 徽墨研开的中浓墨灰'],
      ['玄青', 'XUAN QING', '#3D3B4F', '天玄而地黄 · 带有微紫蓝调的上古礼服至深色'],
      ['缁色', 'ZI SE', '#493131', '缁衣之宜兮，敝予又改为兮 · 黑中透微赤的古朴深褐黑'],
      ['乌金', 'WU JIN', '#312C29', '乌金墨玉泛幽光 · 金属般温润光泽的深炭黑'],
      ['漆黑', 'QI HEI', '#161823', '如胶似漆夜未央 · 大漆器皿深邃含蓝光的镜面黑'],
    ],
  },
};

const FAMILY_ORDER: SpectrumFamily[] = [
  'amber',
  'vermilion',
  'crimson',
  'rouge',
  'violet',
  'indigo',
  'azure',
  'celadon',
  'jade',
  'ink',
];

export const SPECTRUM_COLORS: SpectrumColor[] = (() => {
  const result: SpectrumColor[] = [];
  // 6 rings, each ring has 20 sectors (2 per family in order)
  for (let ring = 0; ring < 6; ring++) {
    FAMILY_ORDER.forEach((family, familyIdx) => {
      const group = RAW_CLASSICAL_COLORS[family];
      for (let sub = 0; sub < 2; sub++) {
        const itemIdx = ring * 2 + sub;
        const [name, pinyin, hex, poem] = group.items[itemIdx];
        const rgb = hexToRgbTuple(hex);
        const cmyk = rgbToCmykTuple(rgb);
        const hsl = rgbToHslTuple(rgb);
        const sector = familyIdx * 2 + sub;
        const slug = pinyin.toLowerCase().replace(/\s+/g, '-');
        result.push({
          id: `${family}-${ring}-${sub}`,
          slug,
          name,
          pinyin,
          poem,
          hex,
          rgb,
          cmyk,
          hsl,
          family,
          familyLabel: group.label,
          ring,
          sector,
        });
      }
    });
  }
  return result;
})();

export function buildSpectrumDiscTerrain(colors: SpectrumColor[]): SpectrumDiscPlacement[] {
  const ringSizes = [12, 18, 22, 24, 24, 20];
  const placements: SpectrumDiscPlacement[] = [];
  let cursor = 0;

  ringSizes.forEach((count, ringIndex) => {
    const slice = colors.slice(cursor, cursor + count);
    cursor += count;
    const radius = 1.05 + ringIndex * 0.88;

    slice.forEach((color, idx) => {
      const ratio = idx / Math.max(1, slice.length);
      const angle = ratio * Math.PI * 2 + ringIndex * 0.14;
      const baseX = Math.cos(angle) * radius;
      const baseZ = Math.sin(angle) * radius * 0.78;
      const wave = Math.sin(angle * 2.5 + ringIndex * 0.65) * 0.12;
      const bowl = Math.pow(ringIndex / 5, 1.45) * 0.34 - 0.18;
      const baseY = bowl + wave;
      const discScale = Math.max(0.76, 1.06 - ringIndex * 0.052);

      placements.push({
        color,
        ringIndex,
        angle,
        radius,
        baseX,
        baseY,
        baseZ,
        discScale,
      });
    });
  });

  // Any remaining colors if filtered oddly
  if (cursor < colors.length) {
    const rest = colors.slice(cursor);
    rest.forEach((color, idx) => {
      const angle = (idx / Math.max(1, rest.length)) * Math.PI * 2;
      const radius = 6.2;
      placements.push({
        color,
        ringIndex: 6,
        angle,
        radius,
        baseX: Math.cos(angle) * radius,
        baseY: 0.2,
        baseZ: Math.sin(angle) * radius * 0.78,
        discScale: 0.74,
      });
    });
  }

  return placements;
}
