/**
 * Web Vitals 埋点（W4-1，docs/04 C.10、docs/08 第 1 节的 LCP / INP / CLS 预算）。
 * 测量用 Google 的 web-vitals 库（动态导入，不进首屏包）；它按缓冲的性能条目计算，晚一点启动也不丢首屏数据。
 * 每个指标每次页面生命周期报一次：LCP / FCP / TTFB 在首屏后定稿，CLS / INP 在页面隐藏时定稿。
 */
import type { MetricType } from 'web-vitals';
import { track } from './track.ts';

/** CLS 是无量纲小数，保留 4 位；其余是毫秒，取整 */
export function metricValue(metric: Pick<MetricType, 'name' | 'value'>): number {
	return metric.name === 'CLS' ? Math.round(metric.value * 10_000) / 10_000 : Math.round(metric.value);
}

/**
 * 启动测量。route 是落地页的路由模板：这几项指标衡量的是这次文档加载，
 * 即便之后在站内切了页面，也归到落地页上。
 */
export async function startWebVitals(route: string | null) {
	const { onCLS, onFCP, onINP, onLCP, onTTFB } = await import('web-vitals');
	const report = (metric: MetricType) => {
		track('web_vitals', {
			props: {
				name: metric.name,
				value: metricValue(metric),
				rating: metric.rating,
				nav: metric.navigationType,
				route: route ?? undefined
			}
		});
	};
	onCLS(report);
	onFCP(report);
	onINP(report);
	onLCP(report);
	onTTFB(report);
}
