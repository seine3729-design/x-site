import type { ClientInit } from '@sveltejs/kit/hooks';

/**
 * 首屏首图（LCP 元素，首页轮播第一张，带 data-lead）先画出来再水合（docs/08 第 5 节）。
 *
 * 水合是一整个长任务，首图正好在水合期间下载完的话，要等水合跑完才能画上屏幕：实测本机就差了 400 ms，
 * 慢的手机上更久，Lighthouse 也会把整段水合算进 LCP。SvelteKit 在水合前会等这个 init，
 * 所以这里最多等 LEAD_WAIT_MS：首图下完（成功或失败）并过完一帧、或者超时，就开始水合。
 * 首图失败时 Svelte 会在水合后重放那次 error 事件，退回原图的逻辑照常执行（HeroCarousel）。
 * 页面上没有首图（不是首页，或首图不是 SSR 输出的）时不等。
 *
 * 代价：首图很慢时页面最多晚 LEAD_WAIT_MS 才能交互。页面里的主要入口都是普通链接，不依赖水合。
 */
const LEAD_WAIT_MS = 800;

export const init: ClientInit = () => {
	const lead = document.querySelector<HTMLImageElement>('img[data-lead]');
	if (!lead) {
		return;
	}
	return new Promise<void>((resolve) => {
		const timer = setTimeout(resolve, LEAD_WAIT_MS);
		// rAF 里排一个宏任务：它在首图那一帧绘制之后才执行
		const painted = () => {
			clearTimeout(timer);
			requestAnimationFrame(() => setTimeout(resolve, 0));
		};
		if (lead.complete) {
			painted();
			return;
		}
		lead.addEventListener('load', painted, { once: true });
		lead.addEventListener('error', painted, { once: true });
	});
};
