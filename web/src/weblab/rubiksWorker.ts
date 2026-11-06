// 在 Worker 里构建解算表并求解，避免首次构建（约 1~2 秒）卡住页面。
import { solveByState, type SolverCubelet } from './rubiksSolver';

self.onmessage = (e: MessageEvent<{ id: number; cubelets: SolverCubelet[]; size: number }>) => {
  const { id, cubelets, size } = e.data;
  try {
    const result = solveByState(cubelets, size);
    self.postMessage({ id, result });
  } catch (err) {
    self.postMessage({ id, result: null, error: (err as Error).message });
  }
};
