import { THEME_STORAGE_KEY } from './apply'

/**
 * 在首屏渲染前定主题，避免浅色闪一下再变深色（feedback/docs/06 第 9 节）。
 *
 * 必须是同步内联脚本：任何异步方案（useEffect、动态 import）都会先画一帧错的颜色。
 * 脚本是这个文件里的字面量，不拼接任何外部输入，所以 dangerouslySetInnerHTML 在这里是安全的。
 */
/*
 * 这里写两个东西，各有用途，不能合并：
 *
 *   .dark 类          → 最终外观。CSS 变量按它切换。
 *   data-theme 属性   → 用户选的模式（system / light / dark）。
 *
 * 为什么需要第二个：跟随系统且系统是深色时，最终外观和「选了深色」完全一样，
 * 单靠 .dark 分不出来。主题切换器要高亮「跟随系统」还是「深色」就得知道这个区别，
 * 而它必须在首帧就可用 —— 否则高亮位置会在 hydration 后跳一下。
 */
const script = `
(function () {
  try {
    var stored = localStorage.getItem('${THEME_STORAGE_KEY}');
    var mode = (stored === 'light' || stored === 'dark') ? stored : 'system';
    var dark = mode === 'dark' ||
      (mode === 'system' && window.matchMedia('(prefers-color-scheme: dark)').matches);
    document.documentElement.classList.toggle('dark', dark);
    document.documentElement.dataset.theme = mode;
    document.documentElement.style.colorScheme = dark ? 'dark' : 'light';
  } catch (e) {
    /* localStorage 被禁用时按系统主题走，不影响渲染 */
    document.documentElement.dataset.theme = 'system';
  }
})();
`

/**
 * 注意：这个脚本只在**新文档**加载时执行一次。
 * 客户端路由导航时它不会重跑，而 React 会把它加的标记抹掉 ——
 * 那种情况由 `ThemeSync` 兜住（见 theme-sync.tsx 的说明）。
 */
export function ThemeScript() {
  return <script dangerouslySetInnerHTML={{ __html: script }} />
}
