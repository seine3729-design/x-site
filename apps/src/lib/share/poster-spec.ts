/**
 * 海报的尺寸、命名与参数类型（docs/04 C.7）。
 *
 * 这些东西单独成文件，是为了让 `poster.ts` 真的能按需加载：
 * 分享面板在渲染时就要尺寸（预览占位的 aspect-ratio、canvas 的 width/height）与下载文件名，
 * 而 Canvas 绘制、封面解码、二维码编码只在点开分享后才用得上。
 * 两者放在同一个文件里时，打包器会因为那几个常量被静态导入，把整个绘制模块一起并进首屏 chunk
 * （实测 16.7KB / br 6.1KB 进了 3 个路由的初始包），`import('./poster.ts')` 就白写了。
 * 所以本文件**不要引入任何依赖**，也不要把 `poster.ts` 的东西在这里转出去。
 */

/** 海报画布尺寸（9:16，分享到社交平台的常用比例） */
export const POSTER_WIDTH = 720;
export const POSTER_HEIGHT = 1280;

export type PosterOptions = {
	/** 剧名 */
	title: string;
	/** 「第 3 集」之类的副标题，详情页分享时可以不传 */
	subtitle?: string;
	/** 站点名，画在顶部 */
	siteName: string;
	/** 二维码指向的完整地址 */
	url: string;
	/** 封面候选地址（多条图片线路），空数组时画渐变 */
	cover: readonly string[];
	/** 「扫码观看」这类提示语 */
	scanHint: string;
};

/** 下载用的文件名：去掉路径不允许的字符，附上日期便于区分 */
export function posterFileName(title: string, subtitle?: string): string {
	const stamp = new Date().toISOString().slice(0, 10);
	const name = [title, subtitle].filter(Boolean).join('-').replace(/[\\/:*?"<>|\s]+/g, '-').slice(0, 48);
	return `${name || 'poster'}-${stamp}.png`;
}
