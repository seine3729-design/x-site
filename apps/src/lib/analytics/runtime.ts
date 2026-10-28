/**
 * 埋点运行时（W4-1，docs/04 C.10）：页面与组件一律 `import('#lib/analytics/runtime.ts')` 动态导入这一个入口，
 * 全部埋点代码（上报队列、来访、注册归因、播放口径、Web Vitals）合成一个懒加载文件，不进任何页面的首屏包
 * （docs/08：首屏 JS ≤ 90 KB br）。只有一个入口，打包器就不必为它在首屏里再拆出零碎的辅助文件。
 */
export { trackRegistered } from './funnel.ts';
export { replayPlayback } from './playback.ts';
export { flushEvents, track } from './track.ts';
export { navigated } from './visit.ts';
