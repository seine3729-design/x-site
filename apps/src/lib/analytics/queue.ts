/**
 * 埋点上报队列（W4-1，docs/04 C.10）：攒批，第一条入队后等一会儿、攒满一批、页面隐藏时发送；失败有限次重试。
 * 这里只管「什么时候发、一次发多少」，怎么发由调用方注入（浏览器里是加密客户端 + keepalive），便于单测。
 */

/** 事件类型，与后端白名单（service/internal/domain/analytics/model.go）一一对应 */
export type EventType =
	| 'visitor'
	| 'page_view'
	| 'play_attempt'
	| 'play_start'
	| 'play_error'
	| 'play_stall'
	| 'line_switch'
	| 'watch'
	| 'ad_impression'
	| 'ad_click'
	| 'register_wall'
	| 'register'
	| 'domain_switch'
	| 'web_vitals';

/** 属性值只用标量；undefined 的键序列化时自动去掉 */
export type EventProps = Record<string, string | number | boolean | undefined>;

export type TrackEvent = {
	type: EventType;
	drama_id?: number;
	episode_id?: number;
	props?: EventProps;
};

/**
 * 发送一批：resolve(true) 表示已送达或重试也没用（如格式错误），false 表示可以重试（断网、服务端 5xx）。
 * 请求真正交给浏览器的那一刻调用 dispatched()（加密是异步的，之前页面若被卸载，这批就没发出去）。
 */
export type Send = (events: TrackEvent[], dispatched: () => void) => Promise<boolean>;

export type QueueOptions = {
	send: Send;
	/** 第一条入队后多久发送（毫秒） */
	delayMs?: number;
	/** 攒到这么多条立即发送 */
	flushAt?: number;
	/** 单批上限：后端每次最多收 50 条，多出的会被截掉 */
	maxBatch?: number;
	/** 队列上限，满了丢最旧的（断网很久时不无限占内存） */
	maxQueue?: number;
	/** 同一条事件最多发几次 */
	maxAttempts?: number;
	/** 一批请求交给浏览器之后、以及每批有了结果（送达、放回重试或放弃）之后调用，调用方据此更新本机暂存 */
	onChange?: () => void;
	setTimer?: (fn: () => void, ms: number) => unknown;
	clearTimer?: (handle: unknown) => void;
};

export type EventQueue = {
	push(event: TrackEvent): void;
	/** 立即发出队列里的全部事件；返回的 promise 在这一轮所有批次都有结果后完成 */
	flush(): Promise<void>;
	/** 还没交给浏览器的全部事件（排队的 + 正在加密的），按先后顺序；页面卸载时只有这些会丢 */
	pending(): TrackEvent[];
	/** 还在排队、没开始发送的条数 */
	size(): number;
};

/** sent：请求已经交给浏览器（keepalive 请求页面关掉也会送达） */
type Item = { event: TrackEvent; attempts: number; sent: boolean };

export function createQueue(options: QueueOptions): EventQueue {
	const {
		send,
		// 等得越短，页面被直接清掉（往返缓存里被淘汰等）时还没发出去的就越少，所以只等 2 秒
		delayMs = 2_000,
		flushAt = 20,
		maxBatch = 50,
		maxQueue = 300,
		maxAttempts = 3,
		onChange,
		setTimer = (fn, ms) => setTimeout(fn, ms),
		clearTimer = (handle) => clearTimeout(handle as ReturnType<typeof setTimeout>)
	} = options;

	let items: Item[] = [];
	/** 已交给 send、还没有结果的 */
	const inflight = new Set<Item>();
	let timer: unknown = null;

	function trim() {
		if (items.length > maxQueue) {
			items.splice(0, items.length - maxQueue);
		}
	}

	function schedule(ms: number) {
		if (timer === null) {
			timer = setTimer(() => {
				timer = null;
				void flush();
			}, ms);
		}
	}

	async function deliver(batch: Item[]) {
		for (const item of batch) {
			inflight.add(item);
		}
		const dispatched = () => {
			for (const item of batch) {
				item.sent = true;
			}
			onChange?.();
		};
		const ok = await send(
			batch.map((item) => item.event),
			dispatched
		).catch(() => false);
		for (const item of batch) {
			inflight.delete(item);
			item.sent = false;
		}
		if (!ok) {
			const retry = batch.filter((item) => ++item.attempts < maxAttempts);
			if (retry.length > 0) {
				// 放回队首（它们比队里的更早），隔一段再试；期间新入队的事件会和它们一起走
				items = [...retry, ...items];
				trim();
				schedule(delayMs * 2);
			}
		}
		onChange?.();
	}

	function flush(): Promise<void> {
		if (timer !== null) {
			clearTimer(timer);
			timer = null;
		}
		const sending: Promise<void>[] = [];
		while (items.length > 0) {
			sending.push(deliver(items.splice(0, maxBatch)));
		}
		return Promise.all(sending).then(() => undefined);
	}

	function push(event: TrackEvent) {
		items.push({ event, attempts: 0, sent: false });
		trim();
		if (items.length >= flushAt) {
			void flush();
		} else {
			schedule(delayMs);
		}
	}

	function pending(): TrackEvent[] {
		return [...inflight, ...items].filter((item) => !item.sent).map((item) => item.event);
	}

	return { push, flush, pending, size: () => items.length };
}
