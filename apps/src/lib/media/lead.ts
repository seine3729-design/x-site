/**
 * 首屏首图（LCP 元素）画出来之前，其余图片先不开始下载（docs/08 第 5 节）。
 *
 * 慢速网络下带宽是首图和其余十几张图平分的：实测首页在模拟 4G 下，轮播首图的下载时间里大半是在和
 * 下面那排封面、Logo、弹窗广告图抢带宽。等首图画出来（或失败、超时）再放行，用户先看到最大的那张，
 * 其余的晚几百毫秒淡入。页面上没有首图时（详情页、列表页）不等，立即放行。
 *
 * 只在浏览器里用：模块状态在服务端是所有请求共享的，SSR 不能调这里的函数。
 */

/** 首图迟迟不出来（弱网、图挂了）也不能把整页的图都拖住 */
const LEAD_TIMEOUT_MS = 2500;

let gate: Promise<void> | null = null;
let open: (() => void) | null = null;

function release(): void {
	open?.();
	open = null;
}

/**
 * 首屏有首图的组件挂载时调用，返回放行函数（首图画出来或加载失败时调用，可重复调用）。
 * 同一页面会话里只拦一次：客户端导航回首页时首图是脚本创建的，其余图片不必再等它。
 */
export function holdImagesUntilLead(timeoutMs = LEAD_TIMEOUT_MS): () => void {
	if (!gate) {
		gate = new Promise<void>((resolve) => {
			open = resolve;
		});
		setTimeout(release, timeoutMs);
	}
	return release;
}

/** 其余图片开始下载前等它 */
export function afterLead(): Promise<void> {
	return gate ?? Promise.resolve();
}

/**
 * 等首图真正画到屏幕上再放行：先等解码，再等它所在的那一帧渲染完（rAF 里排一个宏任务，
 * 它会在这一帧绘制之后才执行）。放行太早的话，其余图片的请求会和首图的绘制落在同一帧之前。
 */
export function releaseAfterPaint(img: HTMLImageElement | undefined, done: () => void): void {
	const afterFrame = () => requestAnimationFrame(() => setTimeout(done, 0));
	if (img && typeof img.decode === 'function') {
		img.decode().then(afterFrame, afterFrame);
	} else {
		afterFrame();
	}
}
