/**
 * 线路质量上报（docs/04 C.2）：首帧、失败、切换事件攒一批再发 POST /api/v1/lines/report，
 * 每 10 秒或页面隐藏时发送一次；后端按设备每分钟 30 条限流，超出静默丢弃，这里不重试。
 */
import type { LineReport } from './controller.svelte.ts';

const queue: (LineReport & { kind: 'video' })[] = [];
let timer: ReturnType<typeof setTimeout> | undefined;

async function flush() {
	timer = undefined;
	if (queue.length === 0) {
		return;
	}
	const items = queue.splice(0, 100);
	try {
		const { api } = await import('#lib/api/client.ts');
		await api.post('/api/v1/lines/report', { items });
	} catch {
		// 上报失败不影响播放
	}
}

export function reportLine(event: LineReport) {
	queue.push({ ...event, kind: 'video' });
	timer ??= setTimeout(flush, 10_000);
}

if (typeof document !== 'undefined') {
	document.addEventListener('visibilitychange', () => {
		if (document.visibilityState === 'hidden') {
			clearTimeout(timer);
			void flush();
		}
	});
}
