export type ParamControl =
  | {
      key: string;
      label: string;
      type: 'range';
      min: number;
      max: number;
      step: number;
      defaultValue: number;
    }
  | {
      key: string;
      label: string;
      type: 'select';
      options: { value: string; label: string }[];
      defaultValue: string;
    }
  | {
      key: string;
      label: string;
      type: 'color';
      defaultValue: string;
    };

export type SceneCategory =
  | 'all'
  | 'physics'
  | 'gpgpu'
  | 'raymarch'
  | 'cosmos'
  | 'math';

export type StudioSceneSpec = {
  id: string;
  number: string;
  title: string;
  subtitle: string;
  category: Exclude<SceneCategory, 'all'>;
  tags: string[];
  description: string;
  interactionHint: string;
  controls: ParamControl[];
  presets?: Record<string, Record<string, any>>;
};

export const SCENE_CATEGORIES: { id: SceneCategory; label: string }[] = [
  { id: 'all', label: '全部场景 (16)' },
  { id: 'gpgpu', label: 'GPGPU 与人工生命' },
  { id: 'physics', label: '物理与声学仿真' },
  { id: 'raymarch', label: 'SDF 光线步进与光学' },
  { id: 'cosmos', label: '天体与场论流形' },
  { id: 'math', label: '数学证明与字符场' },
];

export const STUDIO_SCENES: StudioSceneSpec[] = [
  {
    id: 'gpgpu-black-hole',
    number: '002',
    title: 'GPGPU 黑洞吸积盘',
    subtitle: '262,144 粒子双缓冲开普勒引力与三维旋度湍流',
    category: 'gpgpu',
    tags: ['GPGPU', 'Ping-Pong FBO', 'Curl Noise', 'Keplerian'],
    description:
      '在 GPU 浮点双缓冲帧缓冲中实时演化的黑洞吸积盘：包含内侧光子环、事件视界遮罩、三维 Simplex 旋度湍流与鼠标引力扰动。',
    interactionHint: '拖拽旋转 3D 视角 · 滚轮缩放 · 鼠标移动对吸积盘施加局部引力牵引',
    controls: [
      { key: 'gravity', label: '黑洞引力常数 G', type: 'range', min: 6, max: 32, step: 0.5, defaultValue: 18 },
      { key: 'turbulence', label: '旋度湍流强度', type: 'range', min: 0, max: 2.0, step: 0.05, defaultValue: 0.72 },
      { key: 'friction', label: '轨道阻尼系数', type: 'range', min: 0.94, max: 0.998, step: 0.001, defaultValue: 0.985 },
      { key: 'horizonGlow', label: '光子环辉光', type: 'range', min: 0.2, max: 2.2, step: 0.05, defaultValue: 1.05 },
      { key: 'particleSize', label: '粒子粒径', type: 'range', min: 1.0, max: 4.0, step: 0.1, defaultValue: 2.1 },
      { key: 'colorCore', label: '内盘高温色', type: 'color', defaultValue: '#ffbc66' },
      { key: 'colorOuter', label: '外盘等离子色', type: 'color', defaultValue: '#3d7dff' },
    ],
    presets: {
      'Sagittarius A*': { gravity: 18, turbulence: 0.72, horizonGlow: 1.05, colorCore: '#ffbc66', colorOuter: '#3d7dff' },
      'Quasar Blazar': { gravity: 26, turbulence: 1.35, horizonGlow: 1.75, colorCore: '#ffffff', colorOuter: '#a855f7' },
      'Cold Accretion': { gravity: 11, turbulence: 0.35, horizonGlow: 0.65, colorCore: '#38bdf8', colorOuter: '#0284c7' },
    },
  },
  {
    id: 'toroidal-flow-atlas',
    number: '019',
    title: '环面向量场流线图谱',
    subtitle: 'RK4 四阶龙格-库塔环面流线积分与锁相共振结',
    category: 'cosmos',
    tags: ['RK4 Integrator', 'Toroidal Field', 'GLSL Streamlines'],
    description:
      '在环面坐标系 (u, v, ρ) 下对参数化向量场进行四阶龙格-库塔积分，展示准周期铺满、p:q 锁相共振纽结与磁约束剪切流。',
    interactionHint: '拖拽旋转环面视角 · 滚轮缩放 · 切换预设观察闭合共振纽结与沙漏收缩',
    controls: [
      { key: 'resonanceP', label: '极向缠绕数 p', type: 'range', min: 1, max: 8, step: 1, defaultValue: 3 },
      { key: 'resonanceQ', label: '环向缠绕数 q', type: 'range', min: 1, max: 8, step: 1, defaultValue: 2 },
      { key: 'poloidalSpeed', label: '极向角速度', type: 'range', min: 0.3, max: 2.6, step: 0.05, defaultValue: 1.48 },
      { key: 'toroidalSpeed', label: '环向角速度', type: 'range', min: 0.3, max: 1.8, step: 0.05, defaultValue: 0.86 },
      { key: 'radialPinch', label: '径向沙漏箍缩', type: 'range', min: 0, max: 2.5, step: 0.05, defaultValue: 1.15 },
      { key: 'shearStrength', label: '磁面剪切强度', type: 'range', min: 0, max: 1.1, step: 0.05, defaultValue: 0.5 },
      { key: 'flowSpeed', label: '流线脉冲速度', type: 'range', min: 0.02, max: 0.3, step: 0.01, defaultValue: 0.1 },
      {
        key: 'seedMode',
        label: '种子分布模式',
        type: 'select',
        options: [
          { value: 'surface-shells', label: '嵌套磁面壳层' },
          { value: 'uniform-volume', label: '均匀体积采样' },
          { value: 'cross-section-grid', label: '庞加莱截面网格' },
          { value: 'focused-core', label: '磁轴核心聚集' },
        ],
        defaultValue: 'surface-shells',
      },
    ],
    presets: {
      '平衡流场 (3:2)': { resonanceP: 3, resonanceQ: 2, poloidalSpeed: 1.48, toroidalSpeed: 0.86, radialPinch: 1.15, shearStrength: 0.5, seedMode: 'surface-shells' },
      '闭合锁相纽结 (5:2)': { resonanceP: 5, resonanceQ: 2, poloidalSpeed: 1.88, toroidalSpeed: 0.72, radialPinch: 0.9, shearStrength: 0.42, seedMode: 'surface-shells' },
      '沙漏磁镜箍缩': { resonanceP: 3, resonanceQ: 1, poloidalSpeed: 1.36, toroidalSpeed: 0.82, radialPinch: 2.35, shearStrength: 0.58, seedMode: 'focused-core' },
    },
  },
  {
    id: 'gpu-evolution-life',
    number: '023',
    title: 'GPU 物种演化生命实验室',
    subtitle: '基因色彩遗传、突变漂移与三维实例化群落柱状地形',
    category: 'gpgpu',
    tags: ['GPU Cellular Automata', 'InstancedMesh', 'Mutation'],
    description:
      '运行在 GPU 纹理上的多规则元胞生态系统：新生细胞从父代邻居继承物种基因色相并发生概率突变，高度场反映群落年龄与残影。',
    interactionHint: '左键在棋盘上直接涂抹培育新物种 · 右键擦除 · 拖拽空白处旋转视角',
    controls: [
      {
        key: 'rule',
        label: '演化规则集',
        type: 'select',
        options: [
          { value: 'conway', label: 'Conway (B3/S23)' },
          { value: 'highlife', label: 'HighLife 复制子 (B36/S23)' },
          { value: 'seeds', label: 'Seeds 爆发态 (B2/S)' },
        ],
        defaultValue: 'conway',
      },
      { key: 'speed', label: '演化代数 / 秒', type: 'range', min: 1, max: 30, step: 1, defaultValue: 12 },
      { key: 'mutation', label: '基因突变率', type: 'range', min: 0, max: 0.2, step: 0.005, defaultValue: 0.05 },
      { key: 'cellHeight', label: '三维群落柱高', type: 'range', min: 0.04, max: 0.35, step: 0.01, defaultValue: 0.14 },
      { key: 'trailDecay', label: '代谢残影保持', type: 'range', min: 0.75, max: 0.985, step: 0.005, defaultValue: 0.92 },
      { key: 'brushSize', label: '播种画笔半径', type: 'range', min: 1, max: 8, step: 1, defaultValue: 3 },
      {
        key: 'palette',
        label: '基因色谱映射',
        type: 'select',
        options: [
          { value: '0', label: '全光谱 (Spectrum)' },
          { value: '1', label: '深海生物光 (Ocean)' },
          { value: '2', label: '生物电脉冲 (Bioelectric)' },
        ],
        defaultValue: '0',
      },
    ],
  },
  {
    id: 'gray-scott-reaction',
    number: '050',
    title: 'Gray-Scott 反应扩散图灵斑图',
    subtitle: '512×512 双化学物质 GPU 求解器与三维浮雕映射',
    category: 'gpgpu',
    tags: ['Reaction-Diffusion', 'Turing Pattern', 'GPU Solver'],
    description:
      '实时求解 ∂u/∂t = Du∇²u - uv² + F(1-u) 与 ∂v/∂t = Dv∇²v + uv² - (F+k)v，生成有丝分裂细胞、珊瑚迷宫、孤子波等图灵形态。',
    interactionHint: '按住左键在画布上划动可直接注入化学试剂 V · 切换预设体验不同图灵斑图区域',
    controls: [
      { key: 'feed', label: '进料率 F (Feed)', type: 'range', min: 0.015, max: 0.075, step: 0.0005, defaultValue: 0.0367 },
      { key: 'kill', label: '衰减率 k (Kill)', type: 'range', min: 0.04, max: 0.072, step: 0.0005, defaultValue: 0.0649 },
      { key: 'stepsPerFrame', label: '每帧迭代步数', type: 'range', min: 2, max: 16, step: 1, defaultValue: 8 },
      { key: 'brushRadius', label: '试剂注射半径', type: 'range', min: 0.01, max: 0.08, step: 0.002, defaultValue: 0.03 },
      { key: 'contrast', label: '浓度边缘对比度', type: 'range', min: 0.8, max: 2.4, step: 0.05, defaultValue: 1.5 },
    ],
    presets: {
      '有丝分裂细胞 (Mitosis)': { feed: 0.0367, kill: 0.0649, stepsPerFrame: 8 },
      '珊瑚迷宫 (Coral Maze)': { feed: 0.0545, kill: 0.062, stepsPerFrame: 10 },
      '脉冲孤子与螺旋波': { feed: 0.018, kill: 0.051, stepsPerFrame: 8 },
      '斑点指纹 (U-Skate)': { feed: 0.062, kill: 0.0609, stepsPerFrame: 10 },
    },
  },
  {
    id: 'chladni-standing-wave',
    number: '045',
    title: '克拉尼驻波共振沙盘',
    subtitle: '28,000 颗石英砂沿本征模节线 f(x,y)=0 梯度收敛',
    category: 'physics',
    tags: ['Chladni Eigenmodes', 'Acoustics', 'GPU Particles'],
    description:
      '模拟受激金属薄板的本征驻波 cos(nx)cos(my) - cos(mx)cos(ny) = 0，数万颗沙粒在振动梯度修正下自动向静止节线聚集，并可联动 WebAudio 驻波合成音。',
    interactionHint: '拖拽旋转三维金属共振台 · 调节本征模态 (n, m) 观察几何节线重构 · 可开启驻波发声',
    controls: [
      { key: 'modeN', label: '本征模态数 n', type: 'range', min: 1, max: 9, step: 1, defaultValue: 3 },
      { key: 'modeM', label: '本征模态数 m', type: 'range', min: 1, max: 9, step: 1, defaultValue: 5 },
      { key: 'settling', label: '节线收敛强度', type: 'range', min: 0, max: 1, step: 0.01, defaultValue: 0.94 },
      { key: 'vibration', label: '金属板振幅', type: 'range', min: 0, max: 0.45, step: 0.01, defaultValue: 0.18 },
      { key: 'frequency', label: '激励频率调制', type: 'range', min: 0.2, max: 1.5, step: 0.02, defaultValue: 0.65 },
      { key: 'particleSize', label: '石英砂粒径', type: 'range', min: 1.5, max: 5.0, step: 0.1, defaultValue: 3.0 },
    ],
    presets: {
      '模态 (3, 5) 经典花瓣': { modeN: 3, modeM: 5, settling: 0.94, vibration: 0.18 },
      '模态 (4, 7) 星形晶格': { modeN: 4, modeM: 7, settling: 0.96, vibration: 0.22 },
      '模态 (2, 9) 高阶对称': { modeN: 2, modeM: 9, settling: 0.95, vibration: 0.16 },
    },
  },
  {
    id: 'verlet-cloth-banner',
    number: '010',
    title: 'Verlet 物理织物与风场撕裂',
    subtitle: '弹簧约束松弛、全息丝绸菲涅尔着色与鼠标冲击波',
    category: 'physics',
    tags: ['Verlet Integration', 'Cloth Physics', 'Iridescent Shader'],
    description:
      '基于位置 Verlet 积分与 5 次迭代距离约束松弛的实时三维织物：支持湍流风场、指针速度推力以及点击触发的径向冲击波。',
    interactionHint: '划过织物推开丝绸 · 点击织物发射径向冲击波 · 拖拽旋转观察全息虹彩',
    controls: [
      { key: 'windStrength', label: '湍流风场强度', type: 'range', min: 0, max: 2.5, step: 0.05, defaultValue: 1.1 },
      { key: 'damping', label: '速度阻尼保持', type: 'range', min: 0.92, max: 0.996, step: 0.002, defaultValue: 0.982 },
      { key: 'interactionStrength', label: '指针推力强度', type: 'range', min: 0.2, max: 2.5, step: 0.05, defaultValue: 1.2 },
      { key: 'interactionRadius', label: '作用场半径', type: 'range', min: 0.4, max: 2.2, step: 0.05, defaultValue: 1.1 },
      { key: 'glowStrength', label: '全息丝绸辉光', type: 'range', min: 0.2, max: 2.0, step: 0.05, defaultValue: 1.0 },
    ],
  },
  {
    id: 'boids-flocking',
    number: '016',
    title: '三维 Boids 鸟群集群智能',
    subtitle: '分离/对齐/凝聚三法则、拍翼几何着色器与掠食者规避',
    category: 'gpgpu',
    tags: ['Flocking AI', 'Instanced Birds', 'Predator Avoidance'],
    description:
      '遵循 Craig Reynolds 集群三法则的三维发光飞鸟群：每只飞鸟根据局部邻域动态调整航向，具有独立相位的拍翼顶点动画，并主动规避鼠标指针掠食者。',
    interactionHint: '移动鼠标穿过鸟群充当掠食者驱散飞鸟 · 拖拽旋转相机环视三维鸟群',
    controls: [
      { key: 'avoidCrowding', label: '分离斥力 (Separation)', type: 'range', min: 0.5, max: 3.8, step: 0.05, defaultValue: 2.1 },
      { key: 'matchDirection', label: '航向对齐 (Alignment)', type: 'range', min: 0.1, max: 2.6, step: 0.05, defaultValue: 1.15 },
      { key: 'stayTogether', label: '群落凝聚 (Cohesion)', type: 'range', min: 0.15, max: 1.8, step: 0.05, defaultValue: 0.75 },
      { key: 'flightSpeed', label: '巡航速度上限', type: 'range', min: 1.0, max: 4.5, step: 0.1, defaultValue: 2.4 },
      { key: 'pointerAvoidance', label: '指针掠食者威慑', type: 'range', min: 0, max: 9.0, step: 0.25, defaultValue: 5.0 },
      { key: 'birdSize', label: '飞鸟翼展尺寸', type: 'range', min: 0.6, max: 1.6, step: 0.05, defaultValue: 1.05 },
    ],
  },
  {
    id: 'autonomous-snake',
    number: '022',
    title: '哈密顿环路自治寻路蛇',
    subtitle: 'BFS 最短路裁剪 + 尾部逃生可达性验证 + 动态障碍适应',
    category: 'math',
    tags: ['Hamiltonian Cycle', 'BFS Pathfinding', 'Tail Guard'],
    description:
      '永不自撞的自治寻路算法：每一步在追求食物最短路径前先在虚拟沙盒中推演吃食后的状态，验证蛇头仍能连通蛇尾（Tail-Guard）且不破坏哈密顿环序，直至 100% 填满全图。',
    interactionHint: '点击或拖拽棋盘格子可动态放置食物 / 障碍物，观察算法如何在Safe-Shortcut与Tail-Guard间实时切换',
    controls: [
      { key: 'boardSize', label: '棋盘偶数边长', type: 'range', min: 8, max: 22, step: 2, defaultValue: 12 },
      { key: 'movesPerSecond', label: '每秒决策步数', type: 'range', min: 2, max: 24, step: 1, defaultValue: 12 },
    ],
  },
  {
    id: 'sdf-ferrofluid-sculpture',
    number: '042',
    title: 'SDF 铁磁流体与平滑布尔雕塑',
    subtitle: '104 步光线步进、软阴影环境光遮蔽与音频律动尖峰',
    category: 'raymarch',
    tags: ['SDF Raymarching', 'Smooth Boolean', 'Ferrofluid'],
    description:
      '实时求解有向距离场（Signed Distance Field）：支持球体→环面→圆角立方体连续拓扑变形、伴星平滑布尔融合（Smooth Minimum）以及铁磁流体律动尖刺。',
    interactionHint: '按住左键拖拽直接旋转三维 SDF 雕塑 · 调节平滑融合半径与空间扭曲率',
    controls: [
      { key: 'shapeMorph', label: '拓扑变形 (球→环→方)', type: 'range', min: 0, max: 1, step: 0.01, defaultValue: 0.34 },
      { key: 'smoothUnion', label: '平滑布尔融合系数', type: 'range', min: 0.04, max: 0.5, step: 0.01, defaultValue: 0.24 },
      { key: 'twist', label: '空间螺旋扭曲度', type: 'range', min: 0, max: 2.5, step: 0.05, defaultValue: 0.85 },
      { key: 'spikeIntensity', label: '磁流体尖刺调制', type: 'range', min: 0, max: 0.45, step: 0.01, defaultValue: 0.08 },
      { key: 'satelliteScale', label: '轨道伴星尺寸', type: 'range', min: 0.4, max: 1.6, step: 0.05, defaultValue: 1.05 },
      { key: 'roughness', label: '微平面粗糙度', type: 'range', min: 0.06, max: 0.65, step: 0.02, defaultValue: 0.22 },
    ],
  },
  {
    id: 'molten-relief',
    number: '037',
    title: '熔岩晶格与地质侵蚀浮雕',
    subtitle: 'Voronoi 晶胞裂谷、层状岩脉与冷蓝边缘光法线合成',
    category: 'raymarch',
    tags: ['Voronoi Strata', 'Procedural Heightfield', 'Thermal PBR'],
    description:
      '单通道过程化矿物高度场：融合动态 Voronoi 熔岩晶胞、六重折叠对称晶花、层状侵蚀岩脉与冷暖双色斜向高光。',
    interactionHint: '移动鼠标实时改变斜向主光源入射角 · 切换预设查看黑曜石裂谷或六方晶簇',
    controls: [
      { key: 'reliefStrength', label: '浮雕隆起深度', type: 'range', min: 0.4, max: 1.6, step: 0.02, defaultValue: 1.05 },
      { key: 'cellularScale', label: 'Voronoi 晶胞密度', type: 'range', min: 1.2, max: 5.0, step: 0.1, defaultValue: 2.6 },
      { key: 'crystalStrength', label: '六方晶花生长', type: 'range', min: 0, max: 1.5, step: 0.05, defaultValue: 0.75 },
      { key: 'symmetryStrength', label: '六重对称折叠', type: 'range', min: 0, max: 1, step: 0.02, defaultValue: 0.35 },
      { key: 'heatIntensity', label: '地热辐射强度', type: 'range', min: 0.5, max: 1.8, step: 0.05, defaultValue: 1.15 },
      { key: 'blueRimStrength', label: '冷淬边缘反光', type: 'range', min: 0, max: 1.5, step: 0.05, defaultValue: 0.85 },
    ],
    presets: {
      '玄武岩裂谷 (Basalt Fissure)': { reliefStrength: 1.05, cellularScale: 2.6, crystalStrength: 0.4, symmetryStrength: 0.1, heatIntensity: 1.2, blueRimStrength: 0.85 },
      '六方火山晶簇 (Hex Crystal)': { reliefStrength: 1.2, cellularScale: 3.2, crystalStrength: 1.35, symmetryStrength: 0.92, heatIntensity: 1.1, blueRimStrength: 1.1 },
      '冷却黑曜石 (Cold Obsidian)': { reliefStrength: 0.85, cellularScale: 2.1, crystalStrength: 0.25, symmetryStrength: 0.0, heatIntensity: 0.72, blueRimStrength: 1.4 },
    },
  },
  {
    id: 'thin-film-interference',
    number: '044',
    title: '纳米薄膜光学干涉与双缝衍射',
    subtitle: '皂膜光程差 RGB 相长干涉与杨氏双缝概率波场',
    category: 'raymarch',
    tags: ['Wave Optics', 'Thin-Film RGB', 'Double-Slit ψ'],
    description:
      '精确物理光学着色器：可切换「纳米皂膜反射干涉（按 650/510/475nm 波长与折射率计算光程相位）」与「杨氏双缝干涉衍射包络 ψ = ψ₁ + ψ₂」。',
    interactionHint: '移动鼠标改变观察视角余弦角 θ · 切换光学实验模式观察色彩条纹移动',
    controls: [
      {
        key: 'opticsMode',
        label: '物理光学模式',
        type: 'select',
        options: [
          { value: 'thin-film', label: '纳米皂膜厚度干涉 (Thin-Film)' },
          { value: 'double-slit', label: '杨氏双缝相干衍射场 (Double-Slit)' },
        ],
        defaultValue: 'thin-film',
      },
      { key: 'filmThickness', label: '平均膜厚 (nm) / 波长', type: 'range', min: 120, max: 880, step: 10, defaultValue: 420 },
      { key: 'thicknessVariation', label: '厚度起伏 / 双缝间距', type: 'range', min: 40, max: 380, step: 5, defaultValue: 190 },
      { key: 'refractiveIndex', label: '薄膜折射率 n / 相干度', type: 'range', min: 1.1, max: 1.75, step: 0.01, defaultValue: 1.33 },
      { key: 'flowSpeed', label: '流体对流 / 波传播速度', type: 'range', min: 0.05, max: 0.9, step: 0.02, defaultValue: 0.28 },
    ],
  },
  {
    id: 'gravitational-lensing-aurora',
    number: '051',
    title: '引力透镜爱因斯坦环与极光体积云',
    subtitle: '广义相对论光线偏折方程与 48 层磁力线极光发射积分',
    category: 'cosmos',
    tags: ['Einstein Ring', 'Gravitational Lens', '48-Layer Aurora'],
    description:
      '天体物理光学观测台：支持「前景椭圆星系团引力透镜（产生爱因斯坦环与多重弧像）」与「48 层太阳风磁层极光体渲染」。',
    interactionHint: '移动鼠标改变引力透镜中心质量位置或极光视差角',
    controls: [
      {
        key: 'astroMode',
        label: '天体物理场景',
        type: 'select',
        options: [
          { value: 'lensing', label: '引力透镜爱因斯坦环 (Lensing)' },
          { value: 'aurora', label: '48 层体积极光帷幕 (Aurora)' },
        ],
        defaultValue: 'lensing',
      },
      { key: 'einsteinRadius', label: '爱因斯坦半径 θE / 太阳风', type: 'range', min: 0.12, max: 0.62, step: 0.01, defaultValue: 0.34 },
      { key: 'ellipticity', label: '透镜椭率 / 磁层曲率', type: 'range', min: 0, max: 0.35, step: 0.01, defaultValue: 0.1 },
      { key: 'sourceOffset', label: '背景星系偏轴度 / 极光高度', type: 'range', min: -0.3, max: 0.3, step: 0.01, defaultValue: 0.04 },
      { key: 'exposure', label: '光度曝光增益', type: 'range', min: 0.5, max: 1.8, step: 0.02, defaultValue: 1.15 },
    ],
  },
  {
    id: 'cosmic-web-attractor',
    number: '036',
    title: '宇宙大尺度纤维网与奇异吸引子',
    subtitle: '暗物质星系团断层扫描 + Lorenz/Rössler/Thomas 混沌轨迹',
    category: 'cosmos',
    tags: ['Cosmic Web', 'Strange Attractor', 'Tomography'],
    description:
      '三维空间流形与断层扫描：可切换「宇宙大尺度暗物质纤维网与 Z 轴层析切片」与「RK4 奇异吸引子初值敏感发散流（Lorenz / Rössler / Thomas）」。',
    interactionHint: '拖拽旋转三维空间 · 滚轮缩放 · 观察邻近初值轨迹在混沌吸引子中的指数分离',
    controls: [
      {
        key: 'fieldMode',
        label: '流形系统选择',
        type: 'select',
        options: [
          { value: 'cosmic-web', label: '宇宙大尺度纤维网 (Cosmic Web)' },
          { value: 'lorenz', label: 'Lorenz 奇异吸引子' },
          { value: 'rossler', label: 'Rössler 螺旋吸引子' },
          { value: 'thomas', label: 'Thomas 环对称吸引子' },
        ],
        defaultValue: 'cosmic-web',
      },
      { key: 'chaos', label: '动力学参数 / 纤维束宽', type: 'range', min: 0.05, max: 1.0, step: 0.02, defaultValue: 0.5 },
      { key: 'scanSpeed', label: '层析扫描 / 示踪速率', type: 'range', min: 0.1, max: 1.4, step: 0.02, defaultValue: 0.55 },
      { key: 'pointSize', label: '星团发光粒径', type: 'range', min: 1.5, max: 5.0, step: 0.1, defaultValue: 3.2 },
      { key: 'rotationSpeed', label: '空间自转角速度', type: 'range', min: 0, max: 0.6, step: 0.02, defaultValue: 0.18 },
    ],
  },
  {
    id: 'fibonacci-parastichy-sphere',
    number: '059',
    title: '黄金角叶序球与四态粒子重组',
    subtitle: '137.508° 黄金分割斜列螺旋族 + 48,000 粒子四态变形场',
    category: 'math',
    tags: ['Golden Angle', 'Parastichy Spirals', 'Morphing'],
    description:
      '展示黄金分割角 137.50776° 如何在球面上自然组织出相邻斐波那契数（13/21、21/34、34/55）双向斜列螺旋族；或切换至球体/环面/旋涡星系/波浪网格四态粒子场。',
    interactionHint: '拖拽旋转球体 · 点击画布触发四态粒子变形脉冲',
    controls: [
      {
        key: 'subMode',
        label: '几何构造模式',
        type: 'select',
        options: [
          { value: 'parastichy', label: '斐波那契双向斜列螺旋球' },
          { value: 'morph', label: '48,000 粒子四态拓扑变形场' },
        ],
        defaultValue: 'parastichy',
      },
      {
        key: 'fibonacciPair',
        label: '斐波那契螺旋族阶数',
        type: 'select',
        options: [
          { value: '13/21', label: 'F7 / F8 (13 与 21 族螺旋)' },
          { value: '21/34', label: 'F8 / F9 (21 与 34 族螺旋)' },
          { value: '34/55', label: 'F9 / F10 (34 与 55 族螺旋)' },
        ],
        defaultValue: '21/34',
      },
      { key: 'sampleCount', label: '黄金角采样点数', type: 'range', min: 400, max: 2400, step: 50, defaultValue: 1200 },
      { key: 'lineOpacity', label: '斜列螺旋线透明度', type: 'range', min: 0.05, max: 0.65, step: 0.02, defaultValue: 0.34 },
      { key: 'flowSpeed', label: '螺旋相位脉冲速率', type: 'range', min: 0.05, max: 1.2, step: 0.05, defaultValue: 0.38 },
      { key: 'pointSize', label: '节点发光粒径', type: 'range', min: 1.5, max: 6.0, step: 0.2, defaultValue: 3.8 },
    ],
  },
  {
    id: 'marble-music-proof',
    number: '038',
    title: '三维八音盒轨道机与平方和几何证明',
    subtitle: 'WebAudio 钢珠音锤闭环机械 + ∑k² 立方壳层差分分解',
    category: 'physics',
    tags: ['WebAudio Synth', 'Catmull-Rom Rail', 'Visual Proof'],
    description:
      '支持两种精密三维机构：「双轨钢珠八音盒（钢珠沿闭合样条滚落撞击 C3~D4 六块音板并由 WebAudio 实时合成钟声）」与「平方和公式 (k+1)³ - k³ = 3k² + 3k + 1 离散立方壳层爆炸图证明」。',
    interactionHint: '拖拽旋转三维舞台 · 在八音盒模式下点击底部「开启钟声」即可听到钢珠撞击音板的实时合成乐音',
    controls: [
      {
        key: 'stageMode',
        label: '三维机构模式',
        type: 'select',
        options: [
          { value: 'marble', label: '双轨钢珠八音盒 (Marble Machine)' },
          { value: 'proof', label: '平方和立方壳层几何证明 (∑k² Proof)' },
        ],
        defaultValue: 'marble',
      },
      { key: 'tempo', label: '八音盒速度 (BPM)', type: 'range', min: 60, max: 180, step: 2, defaultValue: 112 },
      { key: 'marbles', label: '轨道钢珠数量 / 阶数 n', type: 'range', min: 2, max: 8, step: 1, defaultValue: 4 },
      { key: 'explodeShell', label: '音板辉光 / 壳层爆炸分离度', type: 'range', min: 0, max: 1.2, step: 0.02, defaultValue: 0.45 },
      { key: 'proofStep', label: '证明步骤 (0~3)', type: 'range', min: 0, max: 3, step: 1, defaultValue: 1 },
    ],
  },
  {
    id: 'alife-ascii-workbench',
    number: '046',
    title: '人工生命自然选择与 3D ASCII 字符场',
    subtitle: '代谢成本基因演化生态圈 + 深度排序三维 ASCII 投影仪',
    category: 'math',
    tags: ['Artificial Life', 'Genetic Selection', '3D ASCII'],
    description:
      '高帧率 2D/2.5D 算法画布：支持「人工生命微生物自然选择（速度、感知半径与转向率基因承受显式代谢消耗，捕食繁殖并实时统计种群均值）」与「三维几何深度排序 ASCII 字符投影与蒙版涂绘」。',
    interactionHint: '按住鼠标在生态圈中投放营养源，或在 ASCII 模式下拖拽控制三维字符场旋转角度',
    controls: [
      {
        key: 'canvasMode',
        label: '算法画布模式',
        type: 'select',
        options: [
          { value: 'alife', label: '微生物基因自然选择生态圈 (ALife)' },
          { value: 'ascii-3d', label: '三维深度排序 ASCII 字符渲染器' },
        ],
        defaultValue: 'alife',
      },
      { key: 'population', label: '初始种群 / 字符采样密度', type: 'range', min: 40, max: 320, step: 10, defaultValue: 160 },
      { key: 'foodDensity', label: '食物供给率 / 字符字号', type: 'range', min: 0.2, max: 1.0, step: 0.02, defaultValue: 0.65 },
      { key: 'mutationRate', label: '基因突变率 / 自转速率', type: 'range', min: 0.02, max: 0.5, step: 0.01, defaultValue: 0.15 },
      { key: 'selectionPressure', label: '代谢自然选择压力', type: 'range', min: 0.2, max: 1.2, step: 0.02, defaultValue: 0.58 },
    ],
  },
];
