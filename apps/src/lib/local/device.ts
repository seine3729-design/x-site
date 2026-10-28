/**
 * 本机设备标识（docs/07 第 0.2 节 X-Device-Id）：首次使用时随机生成并存在 localStorage。
 * 只是随机 UUID，不含身份信息、不关联硬件；用于广告频控、选线种子与播放令牌的设备绑定，
 * 让同一浏览器换网络后（IP 变化）播放令牌仍然有效。
 */
const KEY = 'xs_did';
const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

let cached: string | null = null;
/** 标识是这次打开页面时才生成的（本机以前没有） */
let created = false;

export function deviceId(): string | null {
	if (cached) {
		return cached;
	}
	if (typeof window === 'undefined' || typeof crypto?.randomUUID !== 'function') {
		return null;
	}
	try {
		let value = localStorage.getItem(KEY);
		if (!value || !UUID.test(value)) {
			value = crypto.randomUUID();
			localStorage.setItem(KEY, value);
			created = true;
		}
		cached = value;
	} catch {
		// 隐私模式写不进去：本次会话内用内存里的标识
		cached = crypto.randomUUID();
		created = true;
	}
	return cached;
}

/**
 * 本机是不是第一次来（标识是这次打开页面时才生成的；清过本机数据也算）。
 * 埋点区分新老访客用（docs/04 C.10），不管页面上谁先取过标识，结果都一样。
 */
export function deviceIsNew(): boolean {
	deviceId();
	return created;
}
